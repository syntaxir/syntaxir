package vmlower_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/bytecode"
)

func grammarProgram(t *testing.T, path string) *frontend.Result {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return compiletest.Program(t, string(src), check.Options{})
}

// flowTable returns the planned Node table of prog (checked.Source.FlowTable) and a module compiled
// from prog to select its bodies into.
func flowTable(t *testing.T, prog *frontend.Result) (*checked.FlowTable, *bytecode.Module) {
	t.Helper()
	ft, err := checked.Of(prog).FlowTable()
	if err != nil {
		t.Fatal(err)
	}
	bc, err := vmlower.Compile(vmlower.Input{Run: prog.Runtime(), Elab: prog})
	if err != nil {
		t.Fatal(err)
	}
	return ft, bc
}

// ruleID returns the ID of the rule name in the table's Core IR.
func ruleID(t *testing.T, ft *checked.FlowTable, name string) int {
	t.Helper()
	for i, r := range ft.IR.M.Rules {
		if r.Name == name {
			return i
		}
	}
	t.Fatalf("no rule %s", name)
	return -1
}

// ruleIDs returns the IDs of the rules whose bodies the table holds, in increasing order.
func ruleIDs(ft *checked.FlowTable) []int {
	var ids []int
	for k := range ft.Funcs {
		if k.Use == plandata.RuleBody {
			ids = append(ids, k.Rule)
		}
	}
	slices.Sort(ids)
	return ids
}

// selectRule selects the body of the rule id of the table into bc.
func selectRule(prog *frontend.Result, ft *checked.FlowTable, bc *bytecode.Module, id int) error {
	k := plandata.Key{Use: plandata.RuleBody, Rule: id}
	_, err := vmlower.SelectFlows(bc, vmlower.Input{Run: prog.Runtime(), Elab: prog}, ft.IR.M, ft.Plan, map[plandata.Key]*code.Module{k: ft.Funcs[k]})
	return err
}

// TestSelectJSONValueFlow selects the body of the JSON value rule, a framed body of the plan.
func TestSelectJSONValueFlow(t *testing.T) {
	prog := grammarProgram(t, "../../../testdata/grammars/json/json.sxir")
	ft, bc := flowTable(t, prog)
	id := ruleID(t, ft, "value")
	if err := selectRule(prog, ft, bc, id); err != nil {
		t.Fatal(err)
	}
	f := bc.Flows[id]
	if f == nil {
		t.Fatal("value has no Flow body")
	}
	t.Logf("value: %d registers, %d instructions", f.Regs, len(f.Code))
}

// TestSelectJSONRulesCount reports how many rule bodies of the JSON grammar's table have a bytecode
// Flow form with the operations selected so far; a body that has none is an error that names its
// operation.
func TestSelectJSONRulesCount(t *testing.T) {
	prog := grammarProgram(t, "../../../testdata/grammars/json/json.sxir")
	ft, _ := flowTable(t, prog)
	ok := 0
	for _, id := range ruleIDs(ft) {
		_, bc := flowTable(t, prog)
		if err := selectRule(prog, ft, bc, id); err != nil {
			t.Logf("%s: %v", ft.IR.M.Rules[id].Name, err)
			continue
		}
		ok++
	}
	t.Logf("selected %d of %d rule bodies", ok, len(ruleIDs(ft)))
}

// TestSelectFlowsChecksTheLayout checks that a body is selected only into a rule frame whose slots
// are the capture layout that the plan gave the rule.
func TestSelectFlowsChecksTheLayout(t *testing.T) {
	prog := grammarProgram(t, "../../../testdata/grammars/json/json.sxir")
	ft, bc := flowTable(t, prog)
	id := ruleID(t, ft, "main") // its frame has the slot of a capture
	if len(bc.Rules[id].Scope) == 0 {
		t.Fatal("main has no capture slot")
	}
	bc.Rules[id].Scope = nil
	if err := selectRule(prog, ft, bc, id); err == nil {
		t.Fatal("a body selected into a frame without its planned slots")
	}
}

// TestSelectedFlowsSurviveTheFileForm selects the body of every rule of the testdata grammars that
// has one with the operations selected so far, and checks that the module validates and that its
// bodies and scopes read back from the module file as they were written.
func TestSelectedFlowsSurviveTheFileForm(t *testing.T) {
	paths, err := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no grammars: %v", err)
	}
	total := 0
	for _, path := range paths {
		prog := grammarProgram(t, path)
		ft, bc := flowTable(t, prog)
		for k, m := range ft.Funcs {
			// A function it fails for keeps no body.
			_, _ = vmlower.SelectFlows(bc, vmlower.Input{Run: prog.Runtime(), Elab: prog}, ft.IR.M, ft.Plan, map[plandata.Key]*code.Module{k: m})
		}
		total += len(bc.Flows)
		if err := program.ValidateModule(bc); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		w := &bytecode.Encoder{Strs: map[string]int{}}
		w.WriteModule(bc)
		r := &bytecode.Decoder{Data: w.Body.Bytes(), Strs: bc.Strings}
		got := r.ReadModule()
		if r.Err != nil {
			t.Fatalf("%s: %v", path, r.Err)
		}
		if !reflect.DeepEqual(got.Flows, bc.Flows) || !reflect.DeepEqual(got.Scopes, bc.Scopes) {
			t.Errorf("%s: the Flow bodies read back differ from those written", path)
		}
	}
	if total == 0 {
		t.Fatal("no rule of the testdata grammars has a Flow body")
	}
	t.Logf("%d Flow bodies", total)
}
