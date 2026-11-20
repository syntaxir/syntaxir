package flowplan

import (
	"testing"

	"zeroparse.dev/zp/internal/compile"
	"zeroparse.dev/zp/internal/driver"
	"zeroparse.dev/zp/internal/frontend/elaborate"
	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/code/flow"
	"zeroparse.dev/zp/internal/middleend/corepass"
	"zeroparse.dev/zp/internal/middleend/flowlower"
	"zeroparse.dev/zp/internal/middleend/flowpass"
	"zeroparse.dev/zp/internal/testing/compiletest"
)

// TestPlanTakesTheFallbackOfARefusedFunction checks the decision of the plan while the table is
// lowered: a flat function that the refusal rejects is replaced, in the plan and in the module, by its
// framed fallback under the same key, one without a fallback is left out of both, and an accepted one
// stays flat; the module then holds the functions of the plan in order. (No grammar of the repository
// has a flat function that Refused rejects, so the test refuses by the hook.)
func TestPlanTakesTheFallbackOfARefusedFunction(t *testing.T) {
	prog := compiletest.Program(t, "rule main = a b !.\nrule a = \"a\"+\nrule b = [b]*\n", compile.Options{Recognition: compile.Recognize})
	m, err := elaborate.CoreProjecting(prog, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if m, _, err = corepass.Run(m, &corepass.Env{}, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	var rules []*Rule
	byName := map[string]int{}
	for _, r := range append(append([]*compile.Rule(nil), prog.RuleList...), prog.Twins...) {
		rules = append(rules, &Rule{ID: r.ID, Name: r.Name, Body: r.Def.Expr, Action: r.Action, Plain: r.Plain, Lean: r.Lean, Novalue: r.Novalue, Leader: r.Leader})
		byName[r.Name] = r.ID
	}
	ir := &TableIR{M: m, Opts: flowlower.Options{Budget: true, Recovers: flowlower.Recovers(m), Node: true}}
	all := Plan(ir, rules, nil)
	for _, name := range []string{"main", "a", "b"} {
		if s, ok := all.Func(Key{Rule: byName[name]}); !ok || s.Form != flowlower.Flat || s.Fallback == nil {
			t.Fatalf("rule %s is not routed flat with a fallback: %+v", name, s)
		}
	}
	p := Plan(ir, rules, nil)
	fallbackOfA := p.byKey[Key{Rule: byName["a"]}].Fallback
	p.byKey[Key{Rule: byName["b"]}].Fallback = nil
	refused := map[string]bool{"a": true, "b": true}
	d := &driver.Driver{}
	fm, err := d.Flow("rules", p.Unit(ir, func(k Key) bool { return refused[rules[k.Rule].Name] }), false, flowpass.Bytecode)
	if err != nil {
		t.Fatal(err)
	}
	// The plan takes the decisions when the module is bound.
	if len(p.Funcs) != len(all.Funcs) || p.Funcs[1].Form != flowlower.Flat {
		t.Errorf("the plan changed before the module was bound: %d functions", len(p.Funcs))
	}
	if err := p.Bind(fm); err != nil {
		t.Fatal(err)
	}
	if s, ok := p.Func(Key{Rule: byName["main"]}); !ok || s.Form != flowlower.Flat {
		t.Errorf("main: %+v, want the accepted flat function", s)
	}
	if s, ok := p.Func(Key{Rule: byName["a"]}); !ok || s != fallbackOfA {
		t.Errorf("a: %+v, want its framed fallback", s)
	}
	if s, ok := p.Func(Key{Rule: byName["b"]}); ok {
		t.Errorf("b: %+v, want no function: it has no fallback", s)
	}
	for _, s := range p.Funcs {
		if refused[rules[s.Key.Rule].Name] && s.Form == flowlower.Flat {
			t.Errorf("%s: the refused flat function stays in the functions of the plan", s.Name)
		}
	}
	if f, ok := p.FlowFunc(Key{Rule: byName["a"]}); !ok || !flow.AttrsOf(flow.FuncOf(f)).Frame {
		t.Error("a: the module does not hold its framed fallback")
	}
	if f, ok := p.FlowFunc(Key{Rule: byName["main"]}); !ok || flow.AttrsOf(flow.FuncOf(f)).Frame {
		t.Error("main: the module does not hold its flat function")
	}
}

// TestPlanRefusesByItsPredicate checks the refusal through the plan's own predicate (Refused) while the
// table is lowered: a flat function whose Flow IR reads its entry position, as the lowering hands it
// to the decision, is replaced by its framed fallback, in the plan and in the module, and the others stay
// flat.
func TestPlanRefusesByItsPredicate(t *testing.T) {
	prog := compiletest.Program(t, "rule main = a b !.\nrule a = \"a\"+\nrule b = [b]*\n", compile.Options{Recognition: compile.Recognize})
	m, err := elaborate.CoreProjecting(prog, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if m, _, err = corepass.Run(m, &corepass.Env{}, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	var rules []*Rule
	for _, r := range append(append([]*compile.Rule(nil), prog.RuleList...), prog.Twins...) {
		rules = append(rules, &Rule{ID: r.ID, Name: r.Name, Body: r.Def.Expr, Action: r.Action, Plain: r.Plain, Lean: r.Lean, Novalue: r.Novalue, Leader: r.Leader})
	}
	ir := &TableIR{M: m, Opts: flowlower.Options{Budget: true, Recovers: flowlower.Recovers(m), Node: true}}
	p := Plan(ir, rules, nil)
	u := p.Unit(ir, nil)
	decide := u.Refuse
	reads := 0
	u.Refuse = func(i int, f *code.Func) bool {
		if a := flow.AttrsOf(f); f.Name == "a" && a.Start != "" {
			// The function as a lowering that reads the entry position would write it.
			f.Body.Stmts = append([]code.Stmt{&code.Assert{Cond: code.L(a.Start)}}, f.Body.Stmts...)
			reads++
		}
		return decide(i, f)
	}
	fm, err := (&driver.Driver{}).Flow("rules", u, false, flowpass.Bytecode)
	if err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("the flat function of a was handed to the decision %d times with an entry position", reads)
	}
	if err := p.Bind(fm); err != nil {
		t.Fatal(err)
	}
	for _, s := range p.Funcs {
		f, _ := p.FlowFunc(s.Key)
		frame := flow.AttrsOf(flow.FuncOf(f)).Frame
		if want := rules[s.Key.Rule].Name == "a"; frame != want || (s.Form == flowlower.Frame) != want {
			t.Errorf("%s: framed %v in the module, form %v in the plan; want framed %v", s.Name, frame, s.Form, want)
		}
	}
}
