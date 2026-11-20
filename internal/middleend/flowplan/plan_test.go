package flowplan_test

import (
	"slices"
	"strings"
	"testing"

	"zeroparse.dev/zp/internal/compile"
	"zeroparse.dev/zp/internal/driver"
	"zeroparse.dev/zp/internal/frontend/elaborate"
	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/code/flow"
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/middleend/corepass"
	"zeroparse.dev/zp/internal/middleend/flowlower"
	"zeroparse.dev/zp/internal/middleend/flowpass"
	"zeroparse.dev/zp/internal/middleend/flowplan"
	"zeroparse.dev/zp/internal/pass"
	"zeroparse.dev/zp/internal/testing/compiletest"
)

const grammar = `rule main = ws item* !.
rule ws = ([ ] / comment)*
rule comment = "--" [^\n]* / block_comment
rule block_comment = "/*" (block_comment / !"*/" .)* "*/"
rule item = (n:num => $n) / [a-z]+
rule num = [0-9]+
rule expr = #operators {
  skip [ ]*
  operand num
  level { infix left "+" }
  level { infix left "*" => $lhs }
}
`

// plan compiles src with the recognition mode rec and plans its table: the Node table for
// compile.BuildValues, the recognizer for compile.Recognize.
func plan(t *testing.T, src string, rec compile.Recognition, live func(int) bool) (*core.Module, []*compile.Rule, *flowplan.Table) {
	t.Helper()
	prog := compiletest.Program(t, src, compile.Options{Recognition: rec})
	m, err := elaborate.CoreProjecting(prog, nil, rec == compile.BuildValues)
	if err != nil {
		t.Fatal(err)
	}
	if m, _, err = corepass.Run(m, &corepass.Env{}, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	rules := append(append([]*compile.Rule(nil), prog.RuleList...), prog.Twins...)
	opts := flowlower.Options{Budget: true, Recovers: flowlower.Recovers(m), Node: true}
	ir := &flowplan.TableIR{M: m, Opts: opts}
	p := flowplan.Plan(ir, planRules(rules), live)
	if _, err := (&driver.Driver{}).Flow("rules", p.Unit(ir, nil), false, flowpass.Bytecode); err != nil {
		t.Fatal(err)
	}
	return m, rules, p
}

// planRules returns what the plan reads of the compiled rules.
func planRules(rules []*compile.Rule) []*flowplan.Rule {
	out := make([]*flowplan.Rule, len(rules))
	for i, r := range rules {
		out[i] = &flowplan.Rule{ID: r.ID, Name: r.Name, Body: r.Def.Expr, Action: r.Action, Plain: r.Plain, Lean: r.Lean, Novalue: r.Novalue, Leader: r.Leader}
	}
	return out
}

// TestPlanRoutes checks the routes of a table: a value-free lean rule is flat with its framed body as
// the fallback, a value-building rule is framed and builds, an Operators Rule has its skip and lines
// after the rule bodies, and each layout is the Core rule's scope.
func TestPlanRoutes(t *testing.T) {
	m, rules, p := plan(t, grammar, compile.BuildValues, nil)
	byName := map[string]*compile.Rule{}
	for _, r := range rules {
		if r.Twin == nil || byName[r.Name] == nil {
			byName[r.Name] = r
		}
	}
	for _, r := range rules {
		if got, want := p.Layouts[r.ID], core.SlotNames(m.Rules[r.ID].Scope); !slices.Equal(got, want) {
			t.Errorf("layout of %s = %v, want %v", r.Name, got, want)
		}
	}
	// The rules of the recognizer are value-free and may run flat.
	_, recRules, rec := plan(t, grammar, compile.Recognize, nil)
	var flat *flowplan.FuncSpec
	for _, s := range rec.Funcs {
		if s.Form == flowlower.Flat {
			flat = s
			break
		}
	}
	if flat == nil || !recRules[flat.Key.Rule].Novalue || flat.Fallback == nil || flat.Fallback.Form != flowlower.Frame || flat.Fallback.Key != flat.Key {
		t.Fatalf("flat function %+v, want a value-free rule with a framed fallback", flat)
	}
	item, ok := p.Func(flowplan.Key{Use: flowplan.RuleBody, Rule: byName["item"].ID})
	if !ok || item.Form != flowlower.Frame || !item.Build || item.Fallback != nil {
		t.Fatalf("item: %+v, want a framed body that builds", item)
	}
	if !slices.Equal(core.SlotNames(item.Slots), p.Layouts[byName["item"].ID]) {
		t.Errorf("item: slots %v, layout %v", core.SlotNames(item.Slots), p.Layouts[byName["item"].ID])
	}
	expr := byName["expr"].ID
	if _, ok := p.Func(flowplan.Key{Use: flowplan.RuleBody, Rule: expr}); ok {
		t.Error("an Operators Rule has a rule body")
	}
	var keys []flowplan.Key
	seen := map[flowplan.Key]bool{}
	for _, s := range p.Funcs {
		if seen[s.Key] {
			t.Errorf("key %+v planned twice", s.Key)
		}
		seen[s.Key] = true
		if s.Key.Rule == expr {
			keys = append(keys, s.Key)
		}
	}
	want := []flowplan.Key{{Use: flowplan.OperatorsSkip, Rule: expr}, {Use: flowplan.OperatorsLine, Rule: expr},
		{Use: flowplan.OperatorsLine, Rule: expr, Line: 1}, {Use: flowplan.OperatorsLine, Rule: expr, Line: 2}}
	if !slices.Equal(keys, want) {
		t.Errorf("Operators Rule keys %v, want %v", keys, want)
	}
	if last := p.Funcs[len(p.Funcs)-1]; last.Key != want[len(want)-1] {
		t.Errorf("last function %+v, want the last line of the Operators Rule", last.Key)
	}
	if err := p.CheckLayout(item.Key.Rule, "item", nil); err == nil && len(p.Layouts[item.Key.Rule]) > 0 {
		t.Error("CheckLayout accepts a frame without the planned slots")
	}
}

// TestPlanLive checks that a rule that is not live has no functions and keeps its layout.
func TestPlanLive(t *testing.T) {
	_, rules, all := plan(t, grammar, compile.BuildValues, nil)
	_, _, p := plan(t, grammar, compile.BuildValues, func(id int) bool { return rules[id].Name != "item" && rules[id].Name != "expr" })
	for _, s := range p.Funcs {
		if n := rules[s.Key.Rule].Name; n == "item" || n == "expr" {
			t.Errorf("rule %s is not live but has %+v", n, s.Key)
		}
	}
	if !slices.EqualFunc(all.Layouts, p.Layouts, slices.Equal) {
		t.Error("liveness changes the layouts")
	}
}

// TestRefused checks the refusal of flat functions on their Flow IR as the lowering writes it: the
// planned flat functions of the grammar are accepted, and one that reads its entry position at a
// reachable point is refused.
func TestRefused(t *testing.T) {
	m, _, p := plan(t, grammar, compile.Recognize, nil)
	opts := flowlower.Options{Budget: true, Recovers: flowlower.Recovers(m), Node: true}
	n, starts := 0, 0
	for _, s := range p.Funcs {
		if s.Form != flowlower.Flat {
			continue
		}
		n++
		fn := lowerFlow(t, m, s, opts)
		if flowplan.Refused(fn) {
			t.Errorf("%s is refused", s.Name)
		}
		a := flow.AttrsOf(fn)
		if a.Start == "" {
			continue
		}
		starts++
		fn.Body.Stmts = append([]code.Stmt{&code.Assert{Cond: code.L(a.Start)}}, fn.Body.Stmts...)
		if !flowplan.Refused(fn) {
			t.Errorf("%s reads its start and is not refused", s.Name)
		}
	}
	if n == 0 || starts == 0 {
		t.Fatalf("%d flat functions planned, %d with an entry position: the refusal is not exercised", n, starts)
	}
}

// lowerFlow lowers the function s of the Core module m with the options opts, as the lowering of a
// table does, and returns it.
func lowerFlow(t *testing.T, m *core.Module, s *flowplan.FuncSpec, opts flowlower.Options) *code.Func {
	t.Helper()
	f, err := flowlower.LowerUnit(flowlower.Unit{Module: m, Opts: opts, Funcs: []flowlower.Func{s.Func}})
	if err == nil {
		err = flowlower.Verify(f)
	}
	if err != nil {
		t.Fatal(err)
	}
	return flow.FuncOf(f)
}

// TestPlanDecidesBeforeThePasses checks that the plan decides on its flat functions while the table is
// lowered, once, before any pass: the table is lowered once, each function of the plan is dumped once
// after flowlower, in the order of the plan, and the first Flow pass already runs on the functions of
// the plan, pass by pass, as the driver's manager numbers them.
func TestPlanDecidesBeforeThePasses(t *testing.T) {
	const record = "test-plan-record"
	prog := compiletest.Program(t, grammar, compile.Options{Recognition: compile.Recognize})
	m, err := elaborate.CoreProjecting(prog, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if m, _, err = corepass.Run(m, &corepass.Env{}, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	ir := &flowplan.TableIR{M: m, Opts: flowlower.Options{Budget: true, Recovers: flowlower.Recovers(m), Node: true}}
	p := flowplan.Plan(ir, planRules(append(append([]*compile.Rule(nil), prog.RuleList...), prog.Twins...)), nil)
	var seen, planned []string
	pl := *flowpass.Pipeline
	pl.Passes = append([]pass.Pass[*code.Module, *flowpass.Env]{{Name: record, Obligation: "it changes nothing",
		Run: func(m *code.Module, _ *flowpass.Env) (*code.Module, pass.PreservedSet, error) {
			if seen == nil {
				for _, s := range p.Funcs {
					planned = append(planned, s.Name)
				}
			}
			seen = append(seen, flow.FuncOf(m).Name)
			return m, pass.PreserveAll(), nil
		}}}, pl.Passes...)
	var dump strings.Builder
	d := &driver.Driver{PrintAfter: []string{flowlower.Name}, PrintTo: &dump, TestPasses: []string{record},
		FlowPipeline: func(string) *pass.Pipeline[*code.Module, *flowpass.Env] { return &pl }}
	fm, err := d.Flow("rules", p.Unit(ir, nil), false, flowpass.Bytecode)
	if err != nil {
		t.Fatal(err)
	}
	if len(planned) == 0 || !slices.Equal(seen, planned) {
		t.Errorf("the first pass ran on %v, want the functions of the plan %v", seen, planned)
	}
	if err := p.Bind(fm); err != nil {
		t.Fatal(err)
	}
	var heads []string
	for _, s := range p.Funcs {
		heads = append(heads, "-- Flow IR of "+s.Name+" in the rules table after flowlower --")
	}
	if got := strings.Count(dump.String(), "-- Flow IR of "); got != len(heads) {
		t.Errorf("%d functions dumped after flowlower, want the %d of the plan", got, len(heads))
	}
	last := -1
	for _, h := range heads {
		i := strings.Index(dump.String(), h)
		if i <= last || strings.Count(dump.String(), h) != 1 {
			t.Errorf("%q is not dumped once, in the order of the plan", h)
		}
		last = i
	}
}
