package flowplan_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// TestPlanTakesTheFallbackOfARefusedFunction checks the decision of the plan while the table is
// lowered: a flat function that the refusal rejects is replaced, in the plan of the lowered table and in
// its module, by its framed fallback under the same key, one without a fallback is left out of both, and
// an accepted one stays flat; the module then holds the functions of the plan in order, and the plan
// that was lowered is unchanged. (No grammar of the repository
// has a flat function that Refused rejects, so the test refuses by the hook.)
func TestPlanTakesTheFallbackOfARefusedFunction(t *testing.T) {
	prog := compiletest.Program(t, "rule main = a b !.\nrule a = \"a\"+\nrule b = [b]*\n", check.Options{Recognition: check.Recognize})
	m, err := elaborate.CoreProjecting(prog, false, false)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if m, _, err = corepass.Run(m, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	var rules []*flowplan.Rule
	byName := map[string]int{}
	for _, r := range append(append([]*check.Rule(nil), prog.RuleList...), prog.Twins...) {
		rules = append(rules, &flowplan.Rule{ID: r.ID, Name: r.Name, Body: r.Def.Expr, Action: r.Action, Plain: r.Plain, Lean: r.Lean, Novalue: r.Novalue, Leader: r.Leader})
		byName[r.Name] = r.ID
	}
	ir := &plandata.TableIR{M: m, Opts: lowerdata.Options{Budget: true, Recovers: flowlower.Recovers(m), Node: true}}
	all := flowplan.Plan(ir, rules, nil)
	for _, name := range []string{"main", "a", "b"} {
		if s, ok := all.Func(plandata.Key{Rule: byName[name]}); !ok || s.Form != lowerdata.Flat || s.Fallback == nil {
			t.Fatalf("rule %s is not routed flat with a fallback: %+v", name, s)
		}
	}
	planned := flowplan.Plan(ir, rules, nil)
	a, _ := planned.Func(plandata.Key{Rule: byName["a"]})
	fallbackOfA := a.Fallback
	b, _ := planned.Func(plandata.Key{Rule: byName["b"]})
	b.Fallback = nil
	refused := map[string]bool{"a": true, "b": true}
	d := &driver.Driver{}
	lt, err := d.LowerFlowTable("rules", flowplan.Unit(planned, ir, func(k plandata.Key) bool { return refused[rules[k.Rule].Name] }), false, flowpass.Bytecode)
	if err != nil {
		t.Fatal(err)
	}
	// The lowered table takes the decisions on a plan of its own.
	if len(planned.Funcs) != len(all.Funcs) || planned.Funcs[1].Form != lowerdata.Flat {
		t.Errorf("the lowering changed the plan it lowered: %d functions", len(planned.Funcs))
	}
	p := lt.Plan
	if s, ok := p.Func(plandata.Key{Rule: byName["main"]}); !ok || s.Form != lowerdata.Flat {
		t.Errorf("main: %+v, want the accepted flat function", s)
	}
	if s, ok := p.Func(plandata.Key{Rule: byName["a"]}); !ok || s != fallbackOfA {
		t.Errorf("a: %+v, want its framed fallback", s)
	}
	if s, ok := p.Func(plandata.Key{Rule: byName["b"]}); ok {
		t.Errorf("b: %+v, want no function: it has no fallback", s)
	}
	for _, s := range p.Funcs {
		if refused[rules[s.Key.Rule].Name] && s.Form == lowerdata.Flat {
			t.Errorf("%s: the refused flat function stays in the functions of the plan", s.Name)
		}
	}
	if f, ok := flowFunc(lt, plandata.Key{Rule: byName["a"]}); !ok || !flow.AttrsOf(f).Frame {
		t.Error("a: the module does not hold its framed fallback")
	}
	if f, ok := flowFunc(lt, plandata.Key{Rule: byName["main"]}); !ok || flow.AttrsOf(f).Frame {
		t.Error("main: the module does not hold its flat function")
	}
}

// TestPlanRefusesByItsPredicate checks the refusal through the plan's own predicate (Refused) while the
// table is lowered: a flat function whose Flow IR reads its entry position, as the lowering hands it
// to the decision, is replaced by its framed fallback, in the plan and in the module of the lowered
// table, and the others stay flat.
func TestPlanRefusesByItsPredicate(t *testing.T) {
	prog := compiletest.Program(t, "rule main = a b !.\nrule a = \"a\"+\nrule b = [b]*\n", check.Options{Recognition: check.Recognize})
	m, err := elaborate.CoreProjecting(prog, false, false)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if m, _, err = corepass.Run(m, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	var rules []*flowplan.Rule
	for _, r := range append(append([]*check.Rule(nil), prog.RuleList...), prog.Twins...) {
		rules = append(rules, &flowplan.Rule{ID: r.ID, Name: r.Name, Body: r.Def.Expr, Action: r.Action, Plain: r.Plain, Lean: r.Lean, Novalue: r.Novalue, Leader: r.Leader})
	}
	ir := &plandata.TableIR{M: m, Opts: lowerdata.Options{Budget: true, Recovers: flowlower.Recovers(m), Node: true}}
	u := flowplan.Unit(flowplan.Plan(ir, rules, nil), ir, nil)
	decide := u.Refuse
	reads := 0
	u.Refuse = func(k plandata.Key, f *code.Func) bool {
		if a := flow.AttrsOf(f); f.Name == "a" && a.Start != "" {
			// The function as a lowering that reads the entry position would write it.
			f.Body.Stmts = append([]code.Stmt{&code.Assert{Cond: code.L(a.Start)}}, f.Body.Stmts...)
			reads++
		}
		return decide(k, f)
	}
	lt, err := (&driver.Driver{}).LowerFlowTable("rules", u, false, flowpass.Bytecode)
	if err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("the flat function of a was handed to the decision %d times with an entry position", reads)
	}
	for _, s := range lt.Plan.Funcs {
		f, _ := flowFunc(lt, s.Key)
		frame := flow.AttrsOf(f).Frame
		if want := rules[s.Key.Rule].Name == "a"; frame != want || (s.Form == lowerdata.Frame) != want {
			t.Errorf("%s: framed %v in the module, form %v in the plan; want framed %v", s.Name, frame, s.Form, want)
		}
	}
}

// flowFunc returns the function of the use k in the module of the table t, or false if the table has
// none.
func flowFunc(t *plandata.Table[code.Flow], k plandata.Key) (*code.Func, bool) {
	i, ok := t.Index(k)
	if !ok {
		return nil, false
	}
	return t.Module.Module().Funcs[i], true
}
