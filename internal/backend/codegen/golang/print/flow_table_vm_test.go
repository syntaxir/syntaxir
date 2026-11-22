package print_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/pipetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestFlowTableFollowsGoNodeTable checks that the Node table the bytecode VM runs from Flow IR
// (checked.Source.FlowTable) is the one a generated Go Document runs when it counts the reference
// variant of logical compute units, whose guards skip no call (lowerdata.GuardsEvent), as the VM's do:
// a Go Document counts compute units in the functions of its Node table, and the guards that its
// default variant keeps over calls (lowerdata.GuardsExact) are the one difference between the variants.
// It pairs the functions of the two tables by name, which the plan of a table gives each
// function from its key (a rule's body, its stream body, a skip or line of an Operators Rule, or a
// node the lowering outlines), and compares each pair in full: the text of the function after the
// Flow passes, which holds its operations, locals, blocks and immediates, and its route (flat or
// framed). Every function that the Go generator lowers in its Node table must be in the VM's table,
// and every rule that the Go parser prints from Flow IR must take the same route in both. A function
// of the VM's table may be missing from the Go table for one of two reasons, which the test checks:
// the Go generator prunes the rules that no entry point of the parser reaches, so it lowers no
// function of such a rule, and it plans a stream body only for the start rule of ParseStream, where
// the VM, whose one module serves every start rule, plans one for each rule with a #stream
// repetition. The parser is generated without inline-literals, the one Flow pass of the Go target
// that applies to the Node table of a Document: the VM's target (flowpass.Bytecode) prints
// none of the forms.
func TestFlowTableFollowsGoNodeTable(t *testing.T) {
	paths, err := filepath.Glob("../../../../../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no grammars: %v", err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			g, err := parseTest(string(src))
			if err != nil {
				t.Fatal(err)
			}
			printed, gen, prog, lowered := goNodeTable(t, g)
			if gen == nil || len(printed) == 0 || len(lowered) == 0 {
				t.Fatal("the Node table prints no rule from Flow IR")
			}
			ft, err := checked.Of(prog, pass.Settings{}).FlowTable()
			if err != nil {
				t.Fatal(err)
			}
			goRules := map[int]bool{} // the rules of which the Go generator lowered a function
			for _, f := range lowered {
				goRules[f.rule] = true
			}
			vm := map[string]bool{}
			compared, absent := 0, 0
			for k, m := range ft.Funcs {
				fn := flow.FuncOf(m)
				vm[fn.Name] = true
				got := loweredFunc{rule: k.Rule, text: code.FuncStringIn(m, fn), flat: !flow.AttrsOf(fn).Frame}
				want, ok := lowered[fn.Name]
				switch {
				case ok && want != got:
					t.Errorf("%s: the VM's function (flat %v) differs from the one Go lowered (flat %v):\nVM:\n%s\nGo:\n%s", fn.Name, got.flat, want.flat, got.text, want.text)
				case ok:
					compared++
				case !goRules[k.Rule] || k.Use == plandata.StreamBody:
					absent++ // a pruned rule, or a stream body of a rule that is not the start of ParseStream
				default:
					t.Errorf("%s (%+v): in the VM's table, not in the Go table, whose rule %d Go lowers", fn.Name, k, k.Rule)
				}
			}
			for name := range lowered {
				if !vm[name] {
					t.Errorf("%s: the Go table runs it, the VM's table lacks it", name)
				}
			}
			for id, printedFlat := range printed {
				m, ok := ft.Funcs[plandata.Key{Use: plandata.RuleBody, Rule: id}]
				if !ok {
					t.Errorf("rule %d: printed from Flow IR in Go, no body in the VM's table", id)
					continue
				}
				if fn := flow.FuncOf(m); flow.AttrsOf(fn).Frame == printedFlat {
					t.Errorf("rule %s: the VM runs the flat function %v, Go prints the flat function %v", fn.Name, !printedFlat, printedFlat)
				}
			}
			t.Logf("%d functions agree; %d of the VM's table are not in the Go table", compared, absent)
		})
	}
}

// loweredFunc is a Flow function as text, the rule it belongs to, and whether it is a flat function
// (not flow.Attrs.Frame).
type loweredFunc struct {
	rule int
	text string
	flat bool
}

// goNodeTable generates the Go parser of g with budget controls and Documents, and returns the
// route of each rule the Node table prints from Flow IR (true for the flat route), the generator of
// the Node table, and the Flow functions that the Node table lowered, by name. A name lowered twice
// with different functions fails the test.
func goNodeTable(t *testing.T, g *ast.Grammar) (routes map[int]bool, gen *print.Generator, prog *frontend.Result, funcs map[string]loweredFunc) {
	t.Helper()
	var mu sync.Mutex
	var conflict []string
	funcs = map[string]loweredFunc{} // the Flow functions that the Node table lowered
	routes = map[int]bool{}          // the rules the Node table prints, true for the flat route
	hooks := &print.Hooks{
		Generator: func(x *print.Generator) { gen = x },
		LeanRoute: func(table string, r *common.Rule) {
			if table == "rules" {
				routes[r.ID] = true
			}
		},
		FramedRoute: func(table string, r *common.Rule) {
			if table == "rules" {
				routes[r.ID] = false
			}
		},
	}
	opts := common.GenOptions{Package: "p", Module: "example.com/p", Document: true, LogicalVariant: "event", NoOpt: []string{flowpass.PassInlineLiterals}}
	record := finished("rules", func(m *code.Module, fn *code.Func) {
		a := flow.AttrsOf(fn)
		f := loweredFunc{rule: -1, text: code.FuncStringIn(m, fn), flat: !a.Frame}
		if a.Rule != nil {
			f.rule = a.Rule.ID
		}
		mu.Lock()
		defer mu.Unlock()
		if old, ok := funcs[fn.Name]; ok && old != f {
			conflict = append(conflict, fn.Name)
		}
		funcs[fn.Name] = f
	})
	_, prog, err := generateGoChecked(g, opts, driver.Options{Observers: []pipe.Observer{record}}, hooks)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflict) > 0 {
		t.Fatalf("the Go generator lowered different functions under the names %v", conflict)
	}
	return routes, gen, prog, funcs
}

// finished returns an observer of a compilation that calls record with the module of the table
// labeled table and each of its functions after flow-finish ended them, as the compilation leaves the
// Flow stage.
func finished(table string, record func(m *code.Module, f *code.Func)) pipe.Observer {
	return &pipetest.Watch{Units: true, Fn: func(e pipe.Event, r *pipe.Result) {
		u, ok := r.Unit.(code.At[code.Flow])
		if !ok || r.Err != nil || e.Step.Name != flowpass.FinishName || e.Label.Table != table {
			return
		}
		m := u.Module()
		for _, f := range m.Funcs {
			record(m, f)
		}
	}}
}
