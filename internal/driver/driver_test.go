package driver

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The driver runs the Core passes that the settings leave on, dumps the module after the selected
// names under the label of its table, calls the hook after every pass, and asks for the
// verification after each pass when VerifyEach is set.
func TestDriverCore(t *testing.T) {
	off, err := pass.Settings{}.Without(corepass.PassScans)
	if err != nil {
		t.Fatal(err)
	}
	var dump strings.Builder
	var hooked []string
	d := &Driver{Opt: off, PrintAfter: []string{Elaborate, corepass.PassGuards}, PrintTo: &dump, VerifyEach: true,
		Hooks: Hooks{CoreHook: func(pass string, _ *core.Module) { hooked = append(hooked, pass) }}}
	if _, err := d.Core("typed", empty); err != nil {
		t.Fatal(err)
	}
	// The hook sees the steps that ran: elaborate, and the passes whose flags are on.
	want := []string{corepass.PassGuards}
	if !slices.Equal(hooked, append([]string{Elaborate}, want...)) {
		t.Errorf("hook saw %v, want elaborate and the passes %v", hooked, want)
	}
	if !d.pipeRun("typed", true).VerifyEach {
		t.Error("VerifyEach did not reach the manager")
	}
	got := dump.String()
	for _, label := range []string{"-- after elaborate [typed] --", "-- after guards [typed] --"} {
		if !strings.Contains(got, label) {
			t.Errorf("dump lacks %q:\n%s", label, got)
		}
	}
	if strings.Count(got, "-- after ") != 2 {
		t.Errorf("dump has other sections than the two selected:\n%s", got)
	}

	// Without a writer nothing is printed; "all" selects every pass that ran.
	dump.Reset()
	d = &Driver{PrintAfter: []string{"all"}}
	if _, err := d.Core("rules", empty); err != nil {
		t.Fatal(err)
	}
	d.PrintTo = &dump
	if _, err := d.Core("rules", empty); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(dump.String(), " [rules] --\n"); n != 3 {
		t.Errorf("all printed %d sections, want elaborate, guards and scans:\n%s", n, dump.String())
	}
}

// elaborate computes the attributes of the module that build makes: the module after it carries the
// attributes that core.Analyze computes, which the Core verifier checks and never writes.
func TestDriverElaborateComputesTheAttributes(t *testing.T) {
	build := func() (*core.Module, error) {
		m := &core.Module{Exps: []string{`"a"`}}
		m.Rules = []*core.Rule{{Name: "r", Body: &core.Lit{Text: "a"}, Policy: core.Policy{Seen: -1}}}
		core.CallGraph(m)
		return m, nil
	}
	var elaborated error
	off, err := pass.Settings{}.Without(corepass.PassGuards, corepass.PassScans)
	if err != nil {
		t.Fatal(err)
	}
	d := &Driver{Opt: off, Hooks: Hooks{CoreHook: func(step string, m *core.Module) {
		if step == Elaborate {
			if elaborated = core.CheckAttributes(m); elaborated == nil && m.Rules[0].Body.Attrs().Eff.Set&core.EffConsume == 0 {
				elaborated = errors.New("the literal has no consume effect")
			}
		}
	}}}
	if _, err := d.Core("rules", build); err != nil {
		t.Fatal(err)
	}
	if elaborated != nil {
		t.Errorf("the module after elaborate: %v", elaborated)
	}
}

// empty builds an empty Core module, as the step elaborate of a program without rules does.
func empty() (*core.Module, error) { return &core.Module{}, nil }

// A failing pass is reported as an internal error that keeps the cause; a failing build is the
// failure of the step elaborate, with the message of the build.
func TestDriverCoreError(t *testing.T) {
	cause := errors.New("broken")
	failing := pipe.New(pipe.Def[*core.Module, *core.Module]{
		Decl: pipe.Decl{Name: "test-core-fail", Test: true, Obligation: "none: it fails"},
		In:   corepass.IR, Out: corepass.IR,
		Run: func(_ *pipe.Ctx[*core.Module], m *core.Module) (*core.Module, pass.PreservedSet, error) {
			return m, pass.PreserveAll(), cause
		}})
	d := &Driver{CorePipeline: func(string) pipe.Pipeline[*core.Module, *core.Module] { return pipe.Seq(corepass.IR, failing) }}
	_, err := d.Core("typed", empty)
	if !errors.Is(err, cause) || !strings.HasPrefix(err.Error(), "internal error: ") {
		t.Fatalf("error %v", err)
	}
	bad := errors.New("no such rule")
	m, err := (&Driver{}).Core("typed", func() (*core.Module, error) { return nil, bad })
	if m != nil || !isStep(err, Elaborate) || !errors.Is(err, bad) || err.Error() != bad.Error() {
		t.Fatalf("a failing build: %v, %v", m, err)
	}
}

// testFlowPass is a Flow pass of the tests: it adds a comment to the function, which keeps it valid.
// testFlowPass is a Flow pass of the tests: it adds a comment to the function, which keeps it valid.
const testFlowPass = "test-flow-comment"

// testFlowComment is the pass testFlowPass, whose flag the tests register on in speed.
var testFlowComment = pipe.New(pipe.Def[flowpass.Unit, flowpass.Unit]{
	Decl: pipe.Decl{Name: testFlowPass, Obligation: "a comment changes nothing"},
	In:   code.FlowStage, Out: code.FlowStage,
	Run: func(_ *pipe.Ctx[flowpass.Unit], u flowpass.Unit) (flowpass.Unit, pass.PreservedSet, error) {
		b := flow.FuncOf(u.Module()).Body
		b.Stmts = append(b.Stmts, &code.Comment{Text: "tested"})
		return u, pass.PreserveNone(), nil
	},
})

// errBroken is the failure of the failing passes of the tests.
var errBroken = errors.New("broken")

// testFlowFail is a Flow pass of the tests that empties the body of its function and fails.
var testFlowFail = pipe.New(pipe.Def[flowpass.Unit, flowpass.Unit]{
	Decl: pipe.Decl{Name: "test-flow-fail", Test: true, Obligation: "none: it fails"},
	In:   code.FlowStage, Out: code.FlowStage,
	Run: func(_ *pipe.Ctx[flowpass.Unit], u flowpass.Unit) (flowpass.Unit, pass.PreservedSet, error) {
		flow.FuncOf(u.Module()).Body.Stmts = nil
		return u, pass.PreserveAll(), errBroken
	},
})

// flowPipeline returns a FlowPipeline hook that runs the Flow passes and then extra.
func flowPipeline(extra ...*pipe.Pass[flowpass.Unit, flowpass.Unit]) func(string) pipe.Pipeline[flowpass.Unit, flowpass.Unit] {
	return func(string) pipe.Pipeline[flowpass.Unit, flowpass.Unit] {
		return flowpass.OnFuncs(append(slices.Clone(flowpass.Passes), extra...)...)
	}
}

// withTestFlowPass adds a profile that turns testFlowPass (registered on in speed) off for the
// duration of the test, and returns a FlowPipeline hook that runs a pipeline with the pass.
func withTestFlowPass(t *testing.T) func(string) pipe.Pipeline[flowpass.Unit, flowpass.Unit] {
	defs := profileDefs
	profileDefs = append(slices.Clone(profileDefs), profileDef{Name: "no-comment", Doc: "x", Values: map[string]int{testFlowPass: 0}})
	t.Cleanup(func() { profileDefs = defs })
	return flowPipeline(testFlowComment)
}

// framed returns the unit of a framed function r that matches the literal "a" and builds no value.
func framed() lowerdata.Unit {
	lit := &core.Lit{Text: "a", Exp: 5}
	m := &core.Module{Exps: []string{"any character", "beginning of input", "end of input", "beginning of line", "end of line", `"a"`}}
	r := &core.Rule{Name: "r", Body: lit, Policy: core.Policy{Seen: -1}}
	m.Rules = []*core.Rule{r}
	core.Analyze(m)
	return lowerdata.Unit{Module: m, Funcs: []lowerdata.Func{{Form: lowerdata.Frame, Rule: r, Name: "r", Body: lit}}}
}

// table returns the unit of the framed functions r and s of a table, each of which matches the literal
// "a" and builds no value.
func table() lowerdata.Unit {
	u := framed()
	f := u.Funcs[0]
	f.Name = "s"
	u.Funcs = append(u.Funcs, f)
	return u
}

// execFunc runs Exec on the module m of one function and returns the function (execlower.View).
func (d *Driver) execFunc(table string, m *code.Module, target TargetPipeline, observe bool) (*execlower.Lowered, error) {
	x, err := d.Exec(table, m, target, observe)
	if x == nil {
		return nil, err
	}
	return execlower.View(x, 0), err
}

// comments counts the comments that testFlowPass added to the function of m.
func comments(m *code.Module) int {
	n := 0
	for _, st := range flow.FuncOf(m).Body.Stmts {
		if c, ok := st.(*code.Comment); ok && c.Text == "tested" {
			n++
		}
	}
	return n
}

// tableRuns records the function of each run of testTableExec.
var tableRuns []string

// testTableExec is an Exec pass of the tests that changes nothing and records the function it runs
// on in tableRuns.
var testTableExec = pipe.New(pipe.Def[codepass.Unit, codepass.Unit]{
	Decl: pipe.Decl{Name: "test-table-exec", Test: true, Obligation: "it changes nothing"},
	In:   code.ExecStage, Out: code.ExecStage,
	Run: func(c *pipe.Ctx[codepass.Unit], u codepass.Unit) (codepass.Unit, pass.PreservedSet, error) {
		tableRuns = append(tableRuns, c.Label().Func)
		return u, pass.PreserveAll(), nil
	},
})

// execPipeline returns an ExecPipeline hook that runs the Exec passes and then extra.
func execPipeline(extra ...*pipe.Pass[codepass.Unit, codepass.Unit]) func(string) pipe.Pipeline[codepass.Unit, codepass.Unit] {
	return func(string) pipe.Pipeline[codepass.Unit, codepass.Unit] {
		return codepass.OnFuncs(append(slices.Clone(codepass.Passes), extra...)...)
	}
}

// The driver lowers the functions of a table into one module and runs each pass on every function in
// turn, the Flow passes and then the Exec passes: the dumps show the module under the required steps
// and each function after each pass, pass by pass in the order of the functions, labeled with the
// table, and a pass runs on the functions in their order.
func TestDriverTable(t *testing.T) {
	record := testTableExec.Name()
	tableRuns = nil
	var dump strings.Builder
	d := &Driver{PrintAfter: []string{flowlower.Name, testFlowPass, flowpass.FinishName, execlower.Name, record}, PrintTo: &dump,
		Hooks: Hooks{FlowPipeline: withTestFlowPass(t), ExecPipeline: execPipeline(testTableExec)}}
	f, err := d.Flow("rules", table(), false, flowpass.Target{})
	if err != nil {
		t.Fatal(err)
	}
	x, err := d.Exec("rules", f, noTarget, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Funcs) != 2 || x.Funcs[0].Name != "r" || x.Funcs[1].Name != "s" {
		t.Fatalf("the Exec module holds %d functions", len(x.Funcs))
	}
	var heads []string
	for _, line := range strings.Split(dump.String(), "\n") {
		if strings.HasPrefix(line, "-- ") {
			heads = append(heads, line)
		}
	}
	var want []string
	// A step on the module of the table dumps it once; a pass on each function dumps each.
	for _, step := range []struct {
		after   string
		perFunc bool
	}{{flowlower.Name, false}, {testFlowPass, true}, {flowpass.FinishName, false}, {execlower.Name, false}, {record, true}} {
		if !step.perFunc {
			want = append(want, "-- after "+step.after+" [rules] --")
			continue
		}
		for _, fn := range []string{"r", "s"} {
			want = append(want, "-- after "+step.after+" [rules/"+fn+"] --")
		}
	}
	if !slices.Equal(heads, want) {
		t.Errorf("dumped\n%s\nwant\n%s", strings.Join(heads, "\n"), strings.Join(want, "\n"))
	}
	if w := []string{"r", "s"}; !slices.Equal(tableRuns, w) {
		t.Errorf("the Exec pass ran on %v, want %v", tableRuns, w)
	}
}

// plannedTable returns the planned table of the functions of table(), each the body of a rule.
func plannedTable() plandata.Planned {
	u := table()
	p := &plandata.Plan{}
	for i, f := range u.Funcs {
		p.Add(&plandata.FuncSpec{Key: plandata.Key{Use: plandata.RuleBody, Rule: i}, Func: f})
	}
	return plandata.Planned{Plan: p, IR: &plandata.TableIR{M: u.Module, Opts: u.Opts}}
}

// The driver runs the pipeline of a planned table in one run, from its lowering to the Exec stage, as
// the current Flow and then Exec run it on the functions of the table: the dumps are the same, the
// passes run on the same functions, and the table at the Exec stage holds the function of each use of
// the plan. The test compares the two paths of this code only; that the one run generates and dumps what
// the separate runs did before it rests on comparing the generated parsers and dumps of the two builds.
func TestDriverLowerTable(t *testing.T) {
	steps := []string{flowlower.Name, testFlowPass, flowpass.FinishName, execlower.Name, testTableExec.Name()}
	run := func(lower func(d *Driver)) (string, []string) {
		tableRuns = nil
		var dump strings.Builder
		d := &Driver{PrintAfter: steps, PrintTo: &dump,
			Hooks: Hooks{FlowPipeline: withTestFlowPass(t), ExecPipeline: execPipeline(testTableExec)}}
		lower(d)
		return dump.String(), tableRuns
	}
	stages, stageRuns := run(func(d *Driver) {
		f, err := d.Flow("rules", table(), false, flowpass.Target{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec("rules", f, noTarget, false); err != nil {
			t.Fatal(err)
		}
	})
	var x *plandata.Table[code.Exec]
	one, oneRuns := run(func(d *Driver) {
		var err error
		if x, err = d.LowerTable("rules", plannedTable(), false, flowpass.Target{}, noTarget); err != nil {
			t.Fatal(err)
		}
	})
	if one != stages {
		t.Errorf("one run dumped\n%s\nwhere Flow and Exec dump\n%s", one, stages)
	}
	if !slices.Equal(oneRuns, stageRuns) || len(oneRuns) != 2 {
		t.Errorf("the Exec pass ran on %v in one run, on %v through Flow and Exec", oneRuns, stageRuns)
	}
	for i, name := range []string{"r", "s"} {
		j, ok := x.Index(plandata.Key{Use: plandata.RuleBody, Rule: i})
		if !ok || x.Module.Module().Funcs[j].Name != name {
			t.Errorf("the table has function %d, ok %v, for the rule %s", j, ok, name)
		}
	}
	if st := x.Module.Module().Stage; st != execlower.Stage {
		t.Errorf("the module of the table is at the %q stage", st)
	}
}

// The driver lowers a function, runs a Flow pass when its flag is on, and a profile or a flag turns
// it off; the dumps show the function as lowered under "flowlower" and after each selected pass, labeled
// with its table.
func TestDriverFlow(t *testing.T) {
	run := withTestFlowPass(t)
	var dump strings.Builder
	d := &Driver{PrintAfter: []string{flowlower.Name, testFlowPass}, PrintTo: &dump, VerifyEach: true, Hooks: Hooks{FlowPipeline: run}}
	f, err := d.Flow("rules", framed(), false, flowpass.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if comments(f) != 1 {
		t.Errorf("the pass did not run:\n%s", code.FuncStringIn(f, flow.FuncOf(f)))
	}
	got := dump.String()
	for _, label := range []string{"-- after flowlower [rules] --\nfunc r(", "-- after " + testFlowPass + " [rules/r] --\nfunc r("} {
		if !strings.Contains(got, label) {
			t.Errorf("dump lacks %q:\n%s", label, got)
		}
	}
	byProfile, err := Profile("no-comment")
	if err != nil {
		t.Fatal(err)
	}
	byFlag, err := pass.Settings{}.Without(testFlowPass)
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]pass.Settings{"profile": byProfile, "flag": byFlag} {
		f, err := (&Driver{Opt: s, Hooks: Hooks{FlowPipeline: run}}).Flow("rules", framed(), false, flowpass.Target{})
		if err != nil {
			t.Fatal(err)
		}
		if comments(f) != 0 {
			t.Errorf("the pass ran although the %s turned it off", name)
		}
	}
	// The real pipeline prints the function after each pass that applies: for a framed function
	// of a target that has every form and no observability, every pass but terminals (local
	// functions only) and class-tables (a number, off at its default 0), and flow-finish.
	all := flowpass.Target{Terminals: true, ClassRuns: true, ClassTables: true, GuardedClasses: true, InlineLiterals: true}
	dump.Reset()
	if _, err := (&Driver{PrintAfter: []string{PrintAll}, PrintTo: &dump}).Flow("typed", framed(), false, all); err != nil {
		t.Fatal(err)
	}
	var printed []string
	for _, line := range strings.Split(dump.String(), "\n") {
		if name, ok := strings.CutPrefix(line, "-- after "); ok {
			printed = append(printed, strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name, " --"), " [typed]"), " [typed/r]"))
		}
	}
	if want := []string{flowlower.Name, flowpass.PassGuardedClasses, flowpass.PassClassRuns, flowpass.PassInlineLiterals, flowpass.FinishName}; !slices.Equal(printed, want) {
		t.Errorf("all printed %v, want %v:\n%s", printed, want, dump.String())
	}
}

// seenFuncs records the function that each run of testFlowSee got.
var seenFuncs []*code.Func

// testFlowSee is a Flow pass of the tests that changes its function and records it in seenFuncs.
var testFlowSee = pipe.New(pipe.Def[flowpass.Unit, flowpass.Unit]{
	Decl: pipe.Decl{Name: "test-flow-see", Test: true, Obligation: "a comment changes nothing"},
	In:   code.FlowStage, Out: code.FlowStage,
	Run: func(_ *pipe.Ctx[flowpass.Unit], u flowpass.Unit) (flowpass.Unit, pass.PreservedSet, error) {
		fn := flow.FuncOf(u.Module())
		seenFuncs = append(seenFuncs, fn)
		fn.Body.Stmts = append(fn.Body.Stmts, &code.Comment{Text: "seen"})
		return u, pass.PreserveNone(), nil
	},
})

// The Flow passes of a function change one copy of the module that flowlower made, which flow-finish
// and the printer go on without if a pass fails, and flow-finish finishes it in place; under
// VerifyEach every step that runs takes a copy of its own. The passes that change the function in
// place show the copies: each gets the function of the copy it changes.
func TestDriverFlowCopies(t *testing.T) {
	all := flowpass.Target{Terminals: true, ClassRuns: true, ClassTables: true, GuardedClasses: true, InlineLiterals: true}
	const runs = 3
	see := make([]*pipe.Pass[flowpass.Unit, flowpass.Unit], runs)
	for i := range see {
		see[i] = testFlowSee
	}
	for _, verifyEach := range []bool{false, true} {
		seenFuncs = nil
		d := &Driver{VerifyEach: verifyEach, Hooks: Hooks{FlowPipeline: flowPipeline(see...)}}
		f, err := d.Flow("typed", framed(), false, all)
		if err != nil {
			t.Fatal(err)
		}
		distinct := map[*code.Func]bool{}
		for _, fn := range seenFuncs {
			distinct[fn] = true
		}
		want := 1
		if verifyEach {
			want = runs
		}
		if len(seenFuncs) != runs || len(distinct) != want {
			t.Errorf("VerifyEach %v: %d runs on %d copies, want %d runs on %d", verifyEach, len(seenFuncs), len(distinct), runs, want)
		}
		// flow-finish finishes the copy in place, except under VerifyEach, which copies before it too.
		if fn := flow.FuncOf(f); !verifyEach && fn != seenFuncs[len(seenFuncs)-1] {
			t.Error("Flow returned another function than the one the last pass changed")
		}
	}
}

// The table's Core module, which the driver lowers each function of the table from, is the caller's:
// neither the lowering nor the Flow passes nor flow-finish change it, whether the passes succeed or
// one fails, so the next function of the table lowers from the same module.
func TestDriverFlowKeepsTheCoreModule(t *testing.T) {
	all := flowpass.Target{Terminals: true, ClassRuns: true, ClassTables: true, GuardedClasses: true, InlineLiterals: true}
	for _, c := range []struct {
		what string
		d    *Driver
		err  error
	}{
		{"passes that succeed", &Driver{}, nil},
		{"a failing pass", &Driver{Hooks: Hooks{FlowPipeline: flowPipeline(testFlowFail)}}, errBroken},
	} {
		u := framed()
		before := core.Print(u.Module, true)
		for range 2 {
			f, err := c.d.Flow("typed", u, false, all)
			if !errors.Is(err, c.err) {
				t.Fatalf("%s: %v", c.what, err)
			}
			if len(flow.FuncOf(f).Body.Stmts) == 0 {
				t.Errorf("%s: the module that Flow returned lost its statements", c.what)
			}
			if after := core.Print(u.Module, true); after != before {
				t.Errorf("%s: the Core module changed:\n%s\nwas\n%s", c.what, after, before)
			}
		}
	}
}

// A failure of flowlower is returned as the failure of the step, with no module; a failure of a Flow
// pass is an internal error that names the pass and the function, returned with the module that
// flow-finish ended.
func TestDriverFlowErrors(t *testing.T) {
	u := framed()
	u.Funcs[0].Form = lowerdata.Form(9)
	if f, err := (&Driver{}).Flow("rules", u, false, flowpass.Target{}); f != nil || !isStep(err, flowlower.Name) {
		t.Errorf("a refused unit: %v, %v", f, err)
	}
	d := &Driver{Hooks: Hooks{FlowPipeline: flowPipeline(testFlowFail)}}
	f, err := d.Flow("rules", framed(), false, flowpass.Target{})
	if f == nil || !errors.Is(err, errBroken) || !strings.HasPrefix(err.Error(), "internal error: test-flow-fail [r]: ") {
		t.Errorf("a failing pass: %v, %v", f, err)
	}
}

// A Flow pass that fails in the run of a table ends that run's optional passes: the Exec pass of the
// table runs on no function, while the required steps go on, so the table comes back at the Exec stage,
// with the internal error that names the failed pass.
func TestDriverLowerTableFlowFailure(t *testing.T) {
	tableRuns = nil
	d := &Driver{Hooks: Hooks{FlowPipeline: flowPipeline(testFlowFail), ExecPipeline: execPipeline(testTableExec)}}
	x, err := d.LowerTable("rules", plannedTable(), false, flowpass.Target{}, noTarget)
	if x == nil || !errors.Is(err, errBroken) || !strings.HasPrefix(err.Error(), "internal error: test-flow-fail [r]: ") {
		t.Fatalf("a failing Flow pass: %v, %v", x, err)
	}
	if len(tableRuns) != 0 {
		t.Errorf("the optional Exec pass ran on %v after the Flow pass failed", tableRuns)
	}
	if m := x.Module.Module(); m.Stage != execlower.Stage || len(m.Funcs) != 2 {
		t.Errorf("the table holds %d functions at the %q stage, want the 2 of its plan at the Exec stage", len(m.Funcs), m.Stage)
	}
}

// isStep reports whether err is the failure of the required step name.
func isStep(err error, name string) bool {
	var se *pass.StepError
	return errors.As(err, &se) && se.Step.Name == name
}

// testExecPass is an Exec pass of the tests: it sets the documentation of every function, which keeps
// the module valid.
const testExecPass = "test-exec-doc"

// testExecBreak is an Exec pass of the tests, off in speed, that breaks the module.
const testExecBreak = "test-exec-break"

// testExecDoc and testExecBreaking are the passes testExecPass and testExecBreak.
var (
	testExecDoc = pipe.New(pipe.Def[codepass.Unit, codepass.Unit]{
		Decl: pipe.Decl{Name: testExecPass, Obligation: "a comment changes nothing"},
		In:   code.ExecStage, Out: code.ExecStage,
		Run: func(_ *pipe.Ctx[codepass.Unit], u codepass.Unit) (codepass.Unit, pass.PreservedSet, error) {
			for _, f := range u.Module().Funcs {
				f.Doc = "tested"
			}
			return u, pass.PreserveNone(), nil
		},
	})
	testExecBreaking = pipe.New(pipe.Def[codepass.Unit, codepass.Unit]{
		Decl: pipe.Decl{Name: testExecBreak, Obligation: "none: it breaks the module"},
		In:   code.ExecStage, Out: code.ExecStage,
		Run: func(_ *pipe.Ctx[codepass.Unit], u codepass.Unit) (codepass.Unit, pass.PreservedSet, error) {
			m := u.Module()
			m.Funcs = append(m.Funcs, m.Funcs[0])
			return u, pass.PreserveNone(), nil
		},
	})
)

// execModule returns the module of a function at the Flow stage, ready for execlower.
func execModule(t *testing.T) *code.Module {
	m, err := (&Driver{}).Flow("rules", framed(), false, flowpass.Target{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// noTarget is the pipeline of a target without Exec passes of its own.
var noTarget = pipe.Seq(execlower.LoweredIR)

// An Exec pass runs when its flag is on and a flag turns it off; the dumps show each function of the
// module as lowered under "execlower" and after each selected pass, and a pass that breaks the module is
// named with VerifyEach.
func TestDriverExec(t *testing.T) {
	run := execPipeline(testExecDoc, testExecBreaking)
	var dump strings.Builder
	l, err := (&Driver{PrintAfter: []string{execlower.Name, testExecPass}, PrintTo: &dump, Hooks: Hooks{ExecPipeline: run}}).execFunc("rules", execModule(t), noTarget, false)
	if err != nil {
		t.Fatal(err)
	}
	if l.Func.Doc != "tested" {
		t.Error("the pass did not run")
	}
	for _, label := range []string{"-- after execlower [rules] --\nfunc r(", "-- after " + testExecPass + " [rules/r] --\n// tested\nfunc r("} {
		if !strings.Contains(dump.String(), label) {
			t.Errorf("dump lacks %q:\n%s", label, dump.String())
		}
	}
	off, err := pass.Settings{}.Without(testExecPass)
	if err != nil {
		t.Fatal(err)
	}
	if l, err := (&Driver{Opt: off, Hooks: Hooks{ExecPipeline: run}}).execFunc("rules", execModule(t), noTarget, false); err != nil || l.Func.Doc != "" {
		t.Errorf("the pass ran although the flag turned it off (%v)", err)
	}
	on, err := pass.Settings{}.With(testExecBreak, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&Driver{Opt: on, VerifyEach: true, Hooks: Hooks{ExecPipeline: run}}).execFunc("rules", execModule(t), noTarget, false)
	if err == nil || !strings.Contains(err.Error(), testExecBreak) {
		t.Errorf("a broken module: %v", err)
	}
	// The real pipeline leaves the module alone and prints only the lowered functions.
	dump.Reset()
	if _, err := (&Driver{PrintAfter: []string{PrintAll}, PrintTo: &dump}).execFunc("typed", execModule(t), noTarget, true); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(dump.String(), "-- after "); n != 1+len(codepass.Passes) {
		t.Errorf("all printed %d sections:\n%s", n, dump.String())
	}
}

// wreck changes a lowered function in place as a failing pass could: it empties the body and removes
// the locals.
func wreck(f *code.Func) { f.Body.Stmts, f.Locals = nil, nil }

// testExecFail and testTargetFail are an Exec pass of every target and a pass of a target that wreck
// their function and fail.
var (
	testExecFail = pipe.New(pipe.Def[codepass.Unit, codepass.Unit]{
		Decl: pipe.Decl{Name: "test-exec-fail", Test: true, Obligation: "none: it fails"},
		In:   code.ExecStage, Out: code.ExecStage,
		Run: func(_ *pipe.Ctx[codepass.Unit], u codepass.Unit) (codepass.Unit, pass.PreservedSet, error) {
			wreck(u.Module().Funcs[0])
			return u, pass.PreserveAll(), errBroken
		},
	})
	testTargetFail = pipe.New(pipe.Def[*execlower.Lowered, *execlower.Lowered]{
		Decl: pipe.Decl{Name: "test-target-fail", Test: true, Obligation: "none: it fails"},
		In:   execlower.LoweredIR, Out: execlower.LoweredIR,
		Run: func(_ *pipe.Ctx[*execlower.Lowered], l *execlower.Lowered) (*execlower.Lowered, pass.PreservedSet, error) {
			wreck(l.Func)
			return l, pass.PreserveAll(), errBroken
		},
	})
)

// A failed Exec pass, of the passes that every target runs or of the target's own, is an internal error
// that names the pass and the function, and the run returns the lowered function as execlower made it:
// the pass changed a copy, so the function that the generation goes on with is whole, not as the failed
// pass left it, and it keeps what the printers print (Lowered.Keeps). This holds without VerifyEach,
// which copies before every pass anyway.
func TestDriverExecFailureKeepsTheLoweredFunction(t *testing.T) {
	printed := func(l *execlower.Lowered) string { return code.FuncStringIn(l.Module, l.Func) }
	whole, err := (&Driver{}).execFunc("typed", execModule(t), noTarget, false)
	if err != nil {
		t.Fatal(err)
	}
	want := printed(whole)
	if len(whole.Func.Body.Stmts) == 0 {
		t.Fatal("the lowered function has no statements to lose")
	}
	for _, c := range []struct {
		what   string
		d      *Driver
		target TargetPipeline
		failed string
	}{
		{"a pass that every target runs", &Driver{Hooks: Hooks{ExecPipeline: execPipeline(testExecFail)}}, noTarget, testExecFail.Name()},
		{"a pass of the target", &Driver{}, pipe.Seq(execlower.LoweredIR, testTargetFail), testTargetFail.Name()},
	} {
		l, err := c.d.execFunc("typed", execModule(t), c.target, false)
		if !errors.Is(err, errBroken) || !strings.HasPrefix(err.Error(), "internal error: "+c.failed+" [r]: ") {
			t.Errorf("%s: the error %v does not name the function and the pass", c.what, err)
		}
		if l == nil {
			t.Fatalf("%s: no lowered function", c.what)
		}
		if got := printed(l); got != want {
			t.Errorf("%s: the returned function is not the original:\n%s\nwas\n%s", c.what, got, want)
		}
		if err := l.Keeps(); err != nil {
			t.Errorf("%s: %v", c.what, err)
		}
	}
}

// seqRecorder records the numbers of the decided runs of optional passes (pipe.Event.Seq).
type seqRecorder struct{ seqs []int }

func (*seqRecorder) Needs() pipe.Needs                       { return pipe.Needs{} }
func (*seqRecorder) Before(pipe.Event, func(io.Writer)) bool { return true }
func (r *seqRecorder) After(e pipe.Event, _ *pipe.Result) {
	if e.Seq != 0 {
		r.seqs = append(r.seqs, e.Seq)
	}
}

// One compilation numbers the decided runs of the optional passes once, in the order they run: the
// Core, Flow and Exec stages of two tables, run through one driver, give one strictly increasing
// numbering, which no stage restarts. The Flow and Exec stages run a test pass each, since no pass of
// theirs applies to the functions of table.
func TestDriverNumbersTheCompilationOnce(t *testing.T) {
	rec := &seqRecorder{}
	d := &Driver{Hooks: Hooks{Observers: []pipe.Observer{rec}, FlowPipeline: withTestFlowPass(t),
		ExecPipeline: execPipeline(testTableExec)}}
	build := func() (*core.Module, error) {
		m := framed().Module
		core.CallGraph(m)
		return m, nil
	}
	var ends []int
	for _, name := range []string{"rules", "trules"} {
		if _, err := d.Core(name, build); err != nil {
			t.Fatal(err)
		}
		ends = append(ends, len(rec.seqs))
		f, err := d.Flow(name, table(), false, flowpass.Target{})
		if err != nil {
			t.Fatal(err)
		}
		ends = append(ends, len(rec.seqs))
		if _, err := d.Exec(name, f, noTarget, false); err != nil {
			t.Fatal(err)
		}
		ends = append(ends, len(rec.seqs))
	}
	start := 0
	for i, end := range ends {
		if end == start {
			t.Fatalf("stage %d of the compilation numbered no run: %v", i, rec.seqs)
		}
		start = end
	}
	for i := range rec.seqs {
		if rec.seqs[i] != i+1 {
			t.Fatalf("the runs are numbered %v, want 1 to %d in order", rec.seqs, len(rec.seqs))
		}
	}
}
