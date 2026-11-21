package flowplan_test

import (
	"os"
	"path/filepath"
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
	"zeroparse.dev/zp/internal/pass/pipe"
	"zeroparse.dev/zp/internal/testing/codegentest"
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
	m, err := elaborate.CoreProjecting(prog, false, rec == compile.BuildValues)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if m, _, err = corepass.Run(m, corepass.RunOptions{}); err != nil {
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
	// A read in the block of a form, which a target's pass may replace and Finish splices, counts as a
	// read, and so does a variable that holds a value.
	forms, values := 0, 0
	for _, s := range p.Funcs {
		if s.Form != flowlower.Flat {
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
		fn.Locals = append(fn.Locals, code.P("v", flow.CodeType(flow.TypeValue)))
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
	files, err := filepath.Glob("../../../testdata/grammars/*/*.zp")
	if err != nil || len(files) == 0 {
		t.Fatalf("no test grammars: %v", err)
	}
	n := 0
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, rec := range []compile.Recognition{compile.Recognize, compile.BuildValues} {
			m, _, p := plan(t, string(src), rec, nil)
			opts := flowlower.Options{Budget: true, Recovers: flowlower.Recovers(m), Node: true}
			for _, s := range p.Funcs {
				if s.Form != flowlower.Flat {
					continue
				}
				fn := lowerFlow(t, m, s, opts)
				before, vars := flowplan.Refused(fn), len(flow.AttrsOf(fn).Vars)
				fm := &code.Module{Funcs: []*code.Func{fn}}
				flowpass.Finish(fm)
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
	prog := compiletest.Program(t, grammar, compile.Options{Recognition: compile.Recognize})
	m, err := elaborate.CoreProjecting(prog, false, false)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if m, _, err = corepass.Run(m, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	ir := &flowplan.TableIR{M: m, Opts: flowlower.Options{Budget: true, Recovers: flowlower.Recovers(m), Node: true}}
	p := flowplan.Plan(ir, planRules(append(append([]*compile.Rule(nil), prog.RuleList...), prog.Twins...)), nil)
	var seen, planned []string
	pl := codegentest.RecordingFlow(true, func(m *code.Module) {
		if seen == nil {
			for _, s := range p.Funcs {
				planned = append(planned, s.Name)
			}
		}
		seen = append(seen, flow.FuncOf(m).Name)
	})
	var dump strings.Builder
	d := &driver.Driver{PrintAfter: []string{flowlower.Name}, PrintTo: &dump,
		Hooks: driver.Hooks{FlowPipeline: func(string) pipe.Pipeline[flowpass.Unit, flowpass.Unit] { return pl }}}
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
	// The module is dumped once after flowlower, its functions in the order of the plan.
	body, ok := strings.CutPrefix(dump.String(), "-- after flowlower [rules] --\n")
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
	prog := compiletest.Program(t, src, compile.Options{Recognition: compile.BuildValues})
	m, err := elaborate.CoreFrom(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if m, _, err = corepass.Run(m, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	rules := append(append([]*compile.Rule(nil), prog.RuleList...), prog.Twins...)
	ir := &flowplan.TableIR{M: m, Opts: flowlower.Options{Budget: true, Recovers: flowlower.Recovers(m)}}
	byName := map[string]*compile.Rule{}
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
		s, ok := p.Func(flowplan.Key{Use: flowplan.RuleBody, Rule: r.ID})
		switch {
		case flowlower.LocalRule(m.Rules[r.ID]):
			if !ok || s.Form != flowlower.Flat || s.Fallback != nil {
				t.Errorf("%s: %+v, want a flat function without a fallback", r.Name, s)
			}
		case r == sum:
			if !ok || s.Form != flowlower.Frame || s.Build != m.Rules[r.ID].Want {
				t.Errorf("sum: %+v, want a framed body", s)
			}
		}
	}
	lines := 0
	for _, s := range p.Funcs {
		if s.Key.Use == flowplan.OperatorsSkip || s.Key.Use == flowplan.OperatorsLine {
			lines++
		}
	}
	if lines != 4 {
		t.Errorf("%d skips and lines of the Operators Rule, want 4", lines)
	}
	if u := p.Unit(ir, func(flowplan.Key) bool { return true }); u.Refuse != nil {
		t.Error("the typed table refuses its flat functions")
	}
	other := flowplan.PlanTyped(ir, planRules(rules), nil, func(id int) []string { return append(slots(id), "other") })
	if s, ok := other.Func(flowplan.Key{Use: flowplan.RuleBody, Rule: sum.ID}); ok {
		t.Errorf("sum: %+v, want none: its frame has other slots than the runtime's", s)
	}
}

// TestPlanStream checks the body of the start rule that ParseStream runs: it comes after the other
// functions of the Node table, lowers its #stream repetition as the one that delivers the elements, and
// has the capture layout of the rule's body; the recognizer has none.
func TestPlanStream(t *testing.T) {
	const src = "rule main = ws is:item* #stream $$ => $is\nrule ws = [ ]*\nrule item = n:[0-9]+ ws => $n\n"
	prog := compiletest.Program(t, src, compile.Options{Recognition: compile.BuildValues})
	m, err := elaborate.CoreProjecting(prog, false, true)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if m, _, err = corepass.Run(m, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	rules := planRules(append(append([]*compile.Rule(nil), prog.RuleList...), prog.Twins...))
	ir := &flowplan.TableIR{M: m, Opts: flowlower.Options{Budget: true, Recovers: flowlower.Recovers(m), Node: true}}
	p := flowplan.Plan(ir, rules, nil)
	if !p.Stream(ir, rules[0]) {
		t.Fatal("no stream body planned")
	}
	s := p.Funcs[len(p.Funcs)-1]
	body, ok := p.Func(flowplan.Key{Use: flowplan.RuleBody, Rule: 0})
	if s.Key != (flowplan.Key{Use: flowplan.StreamBody, Rule: 0}) || !s.Stream || s.Name != "main/stream" || !ok || !slices.Equal(core.SlotNames(s.Slots), core.SlotNames(body.Slots)) {
		t.Fatalf("stream body %+v", s)
	}
	fm, err := (&driver.Driver{}).Flow("rules", p.Unit(ir, nil), false, flowpass.Bytecode)
	if err == nil {
		err = p.Bind(fm)
	}
	if err != nil {
		t.Fatal(err)
	}
	f, _ := p.FlowFunc(s.Key)
	if text := code.FuncStringIn(f, flow.FuncOf(f)); !strings.Contains(text, flow.OpEmitStream+"(") {
		t.Errorf("the stream body delivers no element:\n%s", text)
	}
	rec := &flowplan.TableIR{M: m, Opts: flowlower.Options{Budget: true}}
	if flowplan.Plan(rec, rules, nil).Stream(rec, rules[0]) {
		t.Error("a stream body is planned for a table of other than Node values")
	}
}
