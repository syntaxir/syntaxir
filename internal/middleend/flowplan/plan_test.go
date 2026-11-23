package flowplan_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/pipetest"
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
// check.BuildValues, the recognizer for check.Recognize.
func plan(t *testing.T, src string, rec check.Recognition, live func(int) bool) (*core.Module, []*check.Rule, *plandata.Plan) {
	t.Helper()
	ir, rules := tableIR(t, src, rec)
	p := flowplan.Plan(ir, planRules(rules), live)
	if _, err := lowerTable(flowplan.Unit(p, ir, nil), flowpass.Pipeline()); err != nil {
		t.Fatal(err)
	}
	return ir.M, rules, p
}

// lowerTable runs on the planned table u the steps of the pipeline of a table that the bytecode VM runs
// (the VM pipeline of the driver): flowlower, the Flow passes pl for the VM's target (flowpass.Bytecode) and
// flow-finish, with the observers obs.
func lowerTable(u plandata.Planned, pl pipe.Pipeline[flowpass.Unit, flowpass.Unit], obs ...pipe.Observer) (*plandata.Table[code.Flow], error) {
	p := pipe.Then(pipe.Of(flowlower.Lowering), pipe.Within(plandata.FlowModule, pipe.Then(
		pipe.WithConst(flowpass.ObserveFeature, false, pipe.WithConst(flowpass.TargetFeature, flowpass.Bytecode, pl)),
		pipe.Of(flowpass.Finishing))))
	return pipe.Execute(context.Background(), &pipe.Run{Observers: obs}, p, u)
}

// tableIR compiles src with the recognition mode rec and returns the Core IR of its table after the
// Core passes, with the options of a Node table that charges the budget, and the compiled rules,
// value-free twins included.
func tableIR(t *testing.T, src string, rec check.Recognition) (*plandata.TableIR, []*check.Rule) {
	t.Helper()
	prog := compiletest.Program(t, src, check.Options{Recognition: rec})
	m, err := elaborate.CoreProjecting(prog, false, rec == check.BuildValues)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if m, _, err = corepass.Run(m, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	rules := append(append([]*check.Rule(nil), prog.RuleList...), prog.Twins...)
	opts := lowerdata.Options{Logical: true, Recovers: flowlower.Recovers(m), Node: true}
	opts.Guarded = flowplan.Guarded(m, opts)
	return &plandata.TableIR{M: m, Opts: opts}, rules
}

// planRules returns what the plan reads of the compiled rules.
func planRules(rules []*check.Rule) []*plandata.Rule {
	out := make([]*plandata.Rule, len(rules))
	for i, r := range rules {
		out[i] = &plandata.Rule{ID: r.ID, Name: r.Name, Body: r.Def.Expr, Action: r.Action, Plain: r.Plain, Lean: r.Lean, Novalue: r.Novalue, Leader: r.Leader}
	}
	return out
}

// TestPlanRoutes checks the routes of a table: a value-free lean rule is flat with its framed body as
// the fallback, a value-building rule is framed and builds, an Operators Rule has its skip and lines
// after the rule bodies, and each layout is the Core rule's scope.
func TestPlanRoutes(t *testing.T) {
	m, rules, p := plan(t, grammar, check.BuildValues, nil)
	byName := map[string]*check.Rule{}
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
	_, recRules, rec := plan(t, grammar, check.Recognize, nil)
	var flat *plandata.FuncSpec
	for _, s := range rec.Funcs {
		if s.Form == lowerdata.Flat {
			flat = s
			break
		}
	}
	if flat == nil || !recRules[flat.Key.Rule].Novalue || flat.Fallback == nil || flat.Fallback.Form != lowerdata.Frame || flat.Fallback.Key != flat.Key {
		t.Fatalf("flat function %+v, want a value-free rule with a framed fallback", flat)
	}
	item, ok := p.Func(plandata.Key{Use: plandata.RuleBody, Rule: byName["item"].ID})
	if !ok || item.Form != lowerdata.Frame || !item.Build || item.Fallback != nil {
		t.Fatalf("item: %+v, want a framed body that builds", item)
	}
	if !slices.Equal(core.SlotNames(item.Slots), p.Layouts[byName["item"].ID]) {
		t.Errorf("item: slots %v, layout %v", core.SlotNames(item.Slots), p.Layouts[byName["item"].ID])
	}
	expr := byName["expr"].ID
	if _, ok := p.Func(plandata.Key{Use: plandata.RuleBody, Rule: expr}); ok {
		t.Error("an Operators Rule has a rule body")
	}
	var keys []plandata.Key
	seen := map[plandata.Key]bool{}
	for _, s := range p.Funcs {
		if seen[s.Key] {
			t.Errorf("key %+v planned twice", s.Key)
		}
		seen[s.Key] = true
		if s.Key.Rule == expr {
			keys = append(keys, s.Key)
		}
	}
	want := []plandata.Key{{Use: plandata.OperatorsSkip, Rule: expr}, {Use: plandata.OperatorsLine, Rule: expr},
		{Use: plandata.OperatorsLine, Rule: expr, Line: 1}, {Use: plandata.OperatorsLine, Rule: expr, Line: 2}}
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
	_, rules, all := plan(t, grammar, check.BuildValues, nil)
	_, _, p := plan(t, grammar, check.BuildValues, func(id int) bool { return rules[id].Name != "item" && rules[id].Name != "expr" })
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
	m, _, p := plan(t, grammar, check.Recognize, nil)
	opts := lowerdata.Options{Logical: true, Recovers: flowlower.Recovers(m), Node: true}
	n, starts := 0, 0
	for _, s := range p.Funcs {
		if s.Form != lowerdata.Flat {
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
	// A read in the block of a form, which a target's pass may replace and Finish splices, counts as a
	// read, and so does a variable that holds a value.
	forms, values := 0, 0
	for _, s := range p.Funcs {
		if s.Form != lowerdata.Flat {
			continue
		}
		fn := lowerFlow(t, m, s, opts)
		if a := flow.AttrsOf(fn); a.Start != "" && a.Forms != nil && len(a.Forms.Classes) > 0 {
			b := a.Forms.Classes[0].Block
			b.Stmts = append([]code.Stmt{&code.Assert{Cond: code.L(a.Start)}}, b.Stmts...)
			if !flowplan.Refused(fn) {
				t.Errorf("%s reads its start in a form and is not refused", s.Name)
			}
			forms++
		}
		fn = lowerFlow(t, m, s, opts)
		fn.Locals = append(fn.Locals, code.P("v", prim.CodeType(prim.TypeValue)))
		flow.AttrsOf(fn).Vars = append(flow.AttrsOf(fn).Vars, "v")
		if !flowplan.Refused(fn) {
			t.Errorf("%s holds a value and is not refused", s.Name)
		}
		values++
	}
	if forms == 0 || values == 0 {
		t.Fatalf("%d reads in forms, %d values: not exercised", forms, values)
	}
}

// TestRefusedBeforeTheForms checks that the refusal of every planned flat function of the test grammars,
// in the Node table and in the recognizer, is the same on the function as the lowering writes it, with
// the blocks of its forms, and after Finish splices them: deciding before the passes decides as on the
// finished function.
func TestRefusedBeforeTheForms(t *testing.T) {
	files, err := filepath.Glob("../../../testdata/grammars/*/*.sxir")
	if err != nil || len(files) == 0 {
		t.Fatalf("no test grammars: %v", err)
	}
	n := 0
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, rec := range []check.Recognition{check.Recognize, check.BuildValues} {
			m, _, p := plan(t, string(src), rec, nil)
			opts := lowerdata.Options{Logical: true, Recovers: flowlower.Recovers(m), Node: true}
			for _, s := range p.Funcs {
				if s.Form != lowerdata.Flat {
					continue
				}
				fm := lowerFlowModule(t, m, s, opts)
				fn := flow.FuncOf(fm)
				before, vars := flowplan.Refused(fn), len(flow.AttrsOf(fn).Vars)
				if _, err := pipe.Execute(context.Background(), &pipe.Run{Stop: true}, pipe.Of(flowpass.Finishing), code.At[code.Flow]{}.With(fm)); err != nil {
					t.Fatal(err)
				}
				if after := flowplan.Refused(fn); after != before {
					t.Errorf("%s %s: refused %v as lowered, %v when finished", file, s.Name, before, after)
				}
				if len(flow.AttrsOf(fn).Vars) != vars {
					t.Errorf("%s %s: Finish changed the variables, %d to %d", file, s.Name, vars, len(flow.AttrsOf(fn).Vars))
				}
				n++
			}
		}
	}
	if n == 0 {
		t.Fatal("no flat function planned")
	}
	t.Logf("%d flat functions of %d grammars", n, len(files))
}

// lowerFlow lowers the function s of the Core module m with the options opts, as the lowering of a
// table does, and returns it.
func lowerFlow(t *testing.T, m *core.Module, s *plandata.FuncSpec, opts lowerdata.Options) *code.Func {
	t.Helper()
	return flow.FuncOf(lowerFlowModule(t, m, s, opts))
}

// lowerFlowModule is lowerFlow returning the module of the function, at the Flow stage.
func lowerFlowModule(t *testing.T, m *core.Module, s *plandata.FuncSpec, opts lowerdata.Options) *code.Module {
	t.Helper()
	f, err := flowlower.LowerUnit(lowerdata.Unit{Module: m, Opts: opts, Funcs: []lowerdata.Func{s.Func}})
	if err == nil {
		err = flowlower.Verify(f)
	}
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestPlanDecidesBeforeThePasses checks that the plan decides on its flat functions while the table is
// lowered, once, before any pass: the table is lowered once, each function of the plan is dumped once
// after flowlower, in the order of the plan, and the first Flow pass already runs on the functions of
// the plan, pass by pass, as the driver's manager numbers them.
func TestPlanDecidesBeforeThePasses(t *testing.T) {
	prog := compiletest.Program(t, grammar, check.Options{Recognition: check.Recognize})
	m, err := elaborate.CoreProjecting(prog, false, false)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if m, _, err = corepass.Run(m, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	ir := &plandata.TableIR{M: m, Opts: lowerdata.Options{Logical: true, Recovers: flowlower.Recovers(m), Node: true}}
	p := flowplan.Plan(ir, planRules(append(append([]*check.Rule(nil), prog.RuleList...), prog.Twins...)), nil)
	var seen, planned []string
	pl := codegentest.RecordingFlow(true, func(_ pipe.Label, m *code.Module) {
		if seen == nil {
			for _, s := range p.Funcs {
				planned = append(planned, s.Name)
			}
		}
		seen = append(seen, flow.FuncOf(m).Name)
	})
	var dump strings.Builder
	after := &pipetest.Watch{Print: true, Fn: func(e pipe.Event, r *pipe.Result) {
		if e.Step.Name == flowlower.Name && r.Err == nil {
			fmt.Fprintf(&dump, "-- after %s --\n", e.Step.Instance)
			r.Print(&dump)
		}
	}}
	if _, err := lowerTable(flowplan.Unit(p, ir, nil), pl, after); err != nil {
		t.Fatal(err)
	}
	if len(planned) == 0 || !slices.Equal(seen, planned) {
		t.Errorf("the first pass ran on %v, want the functions of the plan %v", seen, planned)
	}
	// The module is dumped once after flowlower, its functions in the order of the plan.
	body, ok := strings.CutPrefix(dump.String(), "-- after flowlower --\n")
	if !ok || strings.Contains(body, "\n-- ") {
		t.Fatalf("the dump is not the one module after flowlower:\n%s", dump.String())
	}
	var want, got []string
	for _, s := range p.Funcs {
		want = append(want, s.Name)
	}
	for _, line := range strings.Split(body, "\n") {
		if name, ok := strings.CutPrefix(line, "func "); ok {
			got = append(got, name[:strings.IndexByte(name, '(')])
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("dumped the functions %v after flowlower, want the functions of the plan %v", got, want)
	}
}

// TestPlanTyped checks the plan of the typed table: a rule that flowlower.LocalRule accepts is a flat
// function with no fallback, a left-recursion leader is a framed body that builds its value when its
// frame has the slots of the frame the runtime shares with the Node rule and is left out otherwise, an
// Operators Rule has its skip and lines, and the lowering refuses no flat function.
func TestPlanTyped(t *testing.T) {
	const src = grammar + "rule sum = (l:sum \"+\" r:num => $l) / num\n"
	prog := compiletest.Program(t, src, check.Options{Recognition: check.BuildValues})
	m, err := elaborate.CoreFrom(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if m, _, err = corepass.Run(m, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	rules := append(append([]*check.Rule(nil), prog.RuleList...), prog.Twins...)
	ir := &plandata.TableIR{M: m, Opts: lowerdata.Options{Logical: true, Recovers: flowlower.Recovers(m)}}
	byName := map[string]*check.Rule{}
	for _, r := range rules {
		if byName[r.Name] == nil {
			byName[r.Name] = r
		}
	}
	sum := byName["sum"]
	if !sum.Leader {
		t.Fatal("sum is not a left-recursion leader")
	}
	slots := func(id int) []string { return core.SlotNames(m.Rules[id].Scope) }
	p := flowplan.PlanTyped(ir, planRules(rules), nil, slots)
	for _, r := range rules {
		s, ok := p.Func(plandata.Key{Use: plandata.RuleBody, Rule: r.ID})
		switch {
		case flowlower.LocalRule(m.Rules[r.ID]):
			if !ok || s.Form != lowerdata.Flat || s.Fallback != nil {
				t.Errorf("%s: %+v, want a flat function without a fallback", r.Name, s)
			}
		case r == sum:
			if !ok || s.Form != lowerdata.Frame || s.Build != m.Rules[r.ID].Want {
				t.Errorf("sum: %+v, want a framed body", s)
			}
		}
	}
	lines := 0
	for _, s := range p.Funcs {
		if s.Key.Use == plandata.OperatorsSkip || s.Key.Use == plandata.OperatorsLine {
			lines++
		}
	}
	if lines != 4 {
		t.Errorf("%d skips and lines of the Operators Rule, want 4", lines)
	}
	if u := flowplan.Unit(p, ir, func(plandata.Key) bool { return true }); u.Refuse != nil {
		t.Error("the typed table refuses its flat functions")
	}
	other := flowplan.PlanTyped(ir, planRules(rules), nil, func(id int) []string { return append(slots(id), "other") })
	if s, ok := other.Func(plandata.Key{Use: plandata.RuleBody, Rule: sum.ID}); ok {
		t.Errorf("sum: %+v, want none: its frame has other slots than the runtime's", s)
	}
}

// TestPlanStream checks the body of the start rule that ParseStream runs: it comes after the other
// functions of the Node table, lowers its #stream repetition as the one that delivers the elements, and
// has the capture layout of the rule's body; the recognizer has none.
func TestPlanStream(t *testing.T) {
	const src = "rule main = ws is:item* #stream $$ => $is\nrule ws = [ ]*\nrule item = n:[0-9]+ ws => $n\n"
	prog := compiletest.Program(t, src, check.Options{Recognition: check.BuildValues})
	m, err := elaborate.CoreProjecting(prog, false, true)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if m, _, err = corepass.Run(m, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	rules := planRules(append(append([]*check.Rule(nil), prog.RuleList...), prog.Twins...))
	ir := &plandata.TableIR{M: m, Opts: lowerdata.Options{Logical: true, Recovers: flowlower.Recovers(m), Node: true}}
	p := flowplan.Plan(ir, rules, nil)
	if !flowplan.Stream(p, ir, rules[0]) {
		t.Fatal("no stream body planned")
	}
	s := p.Funcs[len(p.Funcs)-1]
	body, ok := p.Func(plandata.Key{Use: plandata.RuleBody, Rule: 0})
	if s.Key != (plandata.Key{Use: plandata.StreamBody, Rule: 0}) || !s.Stream || s.Name != "main/stream" || !ok || !slices.Equal(core.SlotNames(s.Slots), core.SlotNames(body.Slots)) {
		t.Fatalf("stream body %+v", s)
	}
	lt, err := lowerTable(flowplan.Unit(p, ir, nil), flowpass.Pipeline())
	if err != nil {
		t.Fatal(err)
	}
	i, ok := lt.Index(s.Key)
	if !ok {
		t.Fatal("the lowered table has no stream body")
	}
	f := lt.Module.Module().FuncUnit(i)
	if text := code.FuncStringIn(f, flow.FuncOf(f)); !strings.Contains(text, flow.OpEmitStream+"(") {
		t.Errorf("the stream body delivers no element:\n%s", text)
	}
	rec := &plandata.TableIR{M: m, Opts: lowerdata.Options{Logical: true}}
	if flowplan.Stream(flowplan.Plan(rec, rules, nil), rec, rules[0]) {
		t.Error("a stream body is planned for a table of other than Node values")
	}
}

// TestPlanBindsFramed checks that a rule whose Core body binds a variable never runs flat: the
// binding is a predicate of the rule's own body, which makes the rule ineligible (RecEligible), so its
// framed body runs, which writes the binding in the frame that the runtime owns. A rule that binds
// nothing runs flat.
func TestPlanBindsFramed(t *testing.T) {
	const src = `var x int
rule main = set "a"
rule set = "b" {x := 1}
`
	ir, rules := tableIR(t, src, check.Recognize)
	planned := planRules(rules)
	p := flowplan.Plan(ir, planned, nil)
	var binds, free int
	for _, r := range planned {
		cr := ir.M.Rules[r.ID]
		s, ok := p.Func(plandata.Key{Use: plandata.RuleBody, Rule: r.ID})
		if !ok {
			t.Fatalf("rule %s has no body", r.Name)
		}
		if cr.Body.Attrs().Eff.Set&core.EffBind != 0 {
			binds++
			if s.Form != lowerdata.Frame || flowplan.RecEligible(r) {
				t.Errorf("rule %s binds and runs as %v (eligible %v), want a framed body", r.Name, s.Form, flowplan.RecEligible(r))
			}
		} else if r.Name == "main" {
			free++
			if s.Form != lowerdata.Flat {
				t.Errorf("rule main binds nothing and runs as %v, want flat", s.Form)
			}
		}
	}
	if binds == 0 || free == 0 {
		t.Fatalf("%d rules bind and %d checked rules do not, want some of each", binds, free)
	}
}

// TestLowerTableOutlines checks that the lowering of a table whose options outline nodes adds a
// function of each outlined call after the planned functions, nested calls included, keyed with the
// use Outlined, numbered per rule, named after a function that calls it and found by the number of the
// call (plandata.Plan.Outlined) with the call's slots and building; and that a table that outlines
// nothing adds none.
func TestLowerTableOutlines(t *testing.T) {
	for _, outline := range []bool{false, true} {
		ir, rules := tableIR(t, grammar, check.BuildValues)
		var item *check.Rule
		for _, r := range rules {
			if r.Name == "item" && item == nil {
				item = r
			}
		}
		if outline {
			body := ir.M.Rules[item.ID].Body
			under := map[core.Node]bool{}
			core.Walk(body, func(n core.Node) { under[n] = n != body && flowlower.Outlinable(n) })
			ir.Opts.Outlined = under
		}
		p := flowplan.Plan(ir, planRules(rules), nil)
		tbl, err := pipe.Execute(context.Background(), &pipe.Run{Stop: true}, pipe.Of(flowlower.Lowering), flowplan.Unit(p, ir, nil))
		if err != nil {
			t.Fatal(err)
		}
		m := tbl.Module.Module()
		var added []*plandata.FuncSpec
		byKey := map[plandata.Key]*plandata.FuncSpec{}
		for _, s := range tbl.Plan.Funcs {
			if s.Key.Use != plandata.Outlined {
				continue
			}
			added = append(added, s)
			byKey[s.Key] = s
		}
		if !outline {
			if len(added) > 0 {
				t.Errorf("a table that outlines nothing adds %d functions", len(added))
			}
			continue
		}
		if len(added) == 0 {
			t.Fatal("no outlined function added")
		}
		if first := slices.IndexFunc(tbl.Plan.Funcs, func(s *plandata.FuncSpec) bool { return s.Key.Use == plandata.Outlined }); first != len(tbl.Plan.Funcs)-len(added) {
			t.Errorf("outlined functions start at %d of %d, want after the planned functions", first, len(tbl.Plan.Funcs))
		}
		// callers holds, for the key of each outlined function, the names of the functions that call it.
		callers := map[plandata.Key]map[string]bool{}
		for _, f := range m.Funcs {
			code.Walk(f.Body, nil, func(e code.Expr) {
				if c, ok := e.(*code.Call); ok && c.Callee == flow.OpCallOutline {
					o := c.Imm.(prim.Outline)
					k, ok := tbl.Plan.Outlined(o.Call)
					if !ok {
						t.Errorf("function %s calls an outlined node that the table has no function of", f.Name)
						return
					}
					// The function found by the call runs it: under its slots, building as it does.
					if s := byKey[k]; s == nil || !slices.Equal(core.SlotNames(s.Slots), o.Slots) || s.Build != o.Build {
						t.Errorf("function %s: outlined call %+v is found as %+v", f.Name, o, s)
					}
					if callers[k] == nil {
						callers[k] = map[string]bool{}
					}
					callers[k][f.Name] = true
				}
			})
		}
		nested := false
		for i, s := range added {
			want := plandata.Key{Use: plandata.Outlined, Rule: item.ID, Line: i}
			if s.Key != want || s.Form != lowerdata.Outline {
				t.Errorf("outlined function %d: %+v %v, want %+v %v", i, s.Key, s.Form, want, lowerdata.Outline)
			}
			// Each is named after a function that calls it.
			cut := strings.LastIndex(s.Name, "/outline")
			if cut < 0 || !callers[s.Key][s.Name[:cut]] {
				t.Errorf("outlined function %s is not named after a function that calls it (%v)", s.Name, callers[s.Key])
			}
			nested = nested || cut >= 0 && strings.Contains(s.Name[:cut], "/outline")
		}
		if !nested {
			t.Error("no outlined function is called from an outlined function")
		}
	}
}
