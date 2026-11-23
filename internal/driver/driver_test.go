package driver

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/libsrc"
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/pipetest"
)

// A testPart is one table of a run of the tests of the parts of the pipeline of a table: its label and
// its unit, the build of its Core IR or its planned table.
type testPart[U any] struct {
	label string
	unit  U
}

// The kinds of the runs of the tests: the tables of a run, and what the run of each makes.
var (
	testCoreIR       = pipe.RegisterIR(&pipe.IR[[]testPart[common.TableSource]]{Name: "test-core-tables"})
	testModulesIR    = pipe.RegisterIR(&pipe.IR[[]*core.Module]{Name: "test-core-modules"})
	testPlannedIR    = pipe.RegisterIR(&pipe.IR[[]testPart[plandata.Planned]]{Name: "test-planned-tables"})
	testFlowTablesIR = pipe.RegisterIR(&pipe.IR[[]*plandata.Table[code.Flow]]{Name: "test-flow-tables"})
	testExecTablesIR = pipe.RegisterIR(&pipe.IR[[]*plandata.Table[code.Exec]]{Name: "test-exec-tables"})
)

// testSplit registers the split of a run of the tests into its tables, labeled as they are, which
// joins what the run of each made in their order, as a compilation splits its tables.
func testSplit[U, V any](name string, in *pipe.IR[[]testPart[U]], part *pipe.IR[U], done *pipe.IR[V], out *pipe.IR[[]V]) *pipe.Split[[]testPart[U], U, V, []V] {
	return pipe.RegisterSplit(in, part, done, out, &pipe.Split[[]testPart[U], U, V, []V]{
		Name: name, Labels: []string{"rules", "typed", "trules"},
		Parts: func(_ pipe.SplitCtx, ps []testPart[U]) ([]U, []string, error) {
			us, ls := make([]U, len(ps)), make([]string, len(ps))
			for i, p := range ps {
				us[i], ls[i] = p.unit, p.label
			}
			return us, ls, nil
		},
		Join: func(_ pipe.SplitCtx, _ []testPart[U], vs []V) ([]V, error) { return vs, nil },
	})
}

// The splits of the runs of the tests: the Core part of the tables, and the lowering of the planned
// tables to the end of the Flow stage or to the Exec stage.
var (
	testCoreTables = testSplit("test-core-split", testCoreIR, tableIR, corepass.IR, testModulesIR)
	testFlowTables = testSplit("test-flow-split", testPlannedIR, plandata.PlannedIR, plandata.FlowTable, testFlowTablesIR)
	testExecTables = testSplit("test-exec-split", testPlannedIR, plandata.PlannedIR, plandata.ExecTable, testExecTablesIR)
)

// testRun is a run of a part of the pipeline in the tests, as a compilation runs its pipeline
// (compilation.run): under settings, with the verification after every step if verifyEach, the
// observers of the debugger d (one that writes nothing if nil) and the edits.
type testRun struct {
	settings   pass.Settings
	verifyEach bool
	d          *debugger
	edits      []pipe.Edit
}

// runTables runs p on each of the tables ps, labeled as they are, with the split s, as r says, and
// returns what it made of each table, with the failure of a required step of a table as the
// *pass.StepError that names it and any other failure as an internal error (assembled.error).
func runTables[U, V any](t *testing.T, r testRun, s *pipe.Split[[]testPart[U], U, V, []V], p pipe.Pipeline[U, V], ps ...testPart[U]) ([]V, error) {
	t.Helper()
	d := r.d
	if d == nil {
		d = &debugger{}
	}
	pl, err := pipe.Apply(pipe.Each(s, p), r.edits...)
	if err != nil {
		t.Fatal(err)
	}
	out, err := execute(context.Background(), d, &pipe.Run{Settings: r.settings, VerifyEach: r.verifyEach, Observers: d.observers()}, pl, ps)
	if err != nil {
		err = assembled{steps: []string{Elaborate, flowlower.Name, execlower.Name}}.error(err)
	}
	return out, err
}

// runCoreTable runs the Core part of the pipeline of a table (corePart) on the table labeled label
// that build makes, as r says, and returns the module after it.
func runCoreTable(t *testing.T, r testRun, label string, build func() (*core.Module, error)) (*core.Module, error) {
	t.Helper()
	out, err := runTables(t, r, testCoreTables, corePart(), testPart[common.TableSource]{label, common.TableSource{Build: func(*cancel.Control) (*core.Module, error) { return build() }}})
	if len(out) == 0 {
		return nil, err
	}
	return out[0], err
}

// runFlowTable runs the pipeline of the planned table p labeled label to the end of its Flow stage
// (flowLowering) for the target flowTarget, as r says, and returns the module of the table.
func runFlowTable(t *testing.T, r testRun, label string, p plandata.Planned, flowTarget flowpass.Target) (*code.Module, error) {
	t.Helper()
	out, err := runTables(t, r, testFlowTables, flowLowering(flowTarget), testPart[plandata.Planned]{label, p})
	if len(out) == 0 || out[0] == nil {
		return nil, err
	}
	return out[0].Module.Module(), err
}

// runExecTable runs the pipeline of the planned table p labeled label to the Exec stage (lowering) for
// the target passes target, as r says, and returns the table.
func runExecTable(t *testing.T, r testRun, label string, p plandata.Planned, target targetPipeline) (*plandata.Table[code.Exec], error) {
	t.Helper()
	out, err := runTables(t, r, testExecTables, lowering(flowpass.Target{}, target), testPart[plandata.Planned]{label, p})
	if len(out) == 0 {
		return nil, err
	}
	return out[0], err
}

// coreSteps returns an observer that records the name of each step that made a Core module of the
// table labeled table, as it ran, and the module after it to see, if set.
func coreSteps(table string, steps *[]string, see func(step string, m *core.Module)) pipe.Observer {
	return &pipetest.Watch{Units: true, Fn: func(e pipe.Event, r *pipe.Result) {
		if m, ok := r.Unit.(*core.Module); ok && r.Err == nil && e.Label.Table == table {
			*steps = append(*steps, e.Step.Instance)
			if see != nil {
				see(e.Step.Instance, m)
			}
		}
	}}
}

// The Core part of the pipeline runs the Core passes that the settings leave on, the debugger dumps the
// module after the selected names under the label of its table, and an observer sees the module after
// every step that ran.
func TestDriverCore(t *testing.T) {
	off, err := pass.Settings{}.Without(corepass.PassScans)
	if err != nil {
		t.Fatal(err)
	}
	var dump strings.Builder
	var hooked []string
	d := &debugger{PrintAfter: []string{Elaborate, corepass.PassGuards}, Output: &dump, call: []pipe.Observer{coreSteps("typed", &hooked, nil)}}
	if _, err := runCoreTable(t, testRun{settings: off, verifyEach: true, d: d}, "typed", empty); err != nil {
		t.Fatal(err)
	}
	// The observer sees the steps that ran: elaborate, and the passes whose flags are on.
	want := []string{corepass.PassGuards}
	if !slices.Equal(hooked, append([]string{Elaborate}, want...)) {
		t.Errorf("the observer saw %v, want elaborate and the passes %v", hooked, want)
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
	if _, err := runCoreTable(t, testRun{d: &debugger{PrintAfter: []string{"all"}}}, "rules", empty); err != nil {
		t.Fatal(err)
	}
	if _, err := runCoreTable(t, testRun{d: &debugger{PrintAfter: []string{"all"}, Output: &dump}}, "rules", empty); err != nil {
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
	var steps []string
	d := &debugger{call: []pipe.Observer{coreSteps("rules", &steps, func(step string, m *core.Module) {
		if step == Elaborate {
			if elaborated = core.CheckAttributes(m); elaborated == nil && m.Rules[0].Body.Attrs().Eff.Set&core.EffConsume == 0 {
				elaborated = errors.New("the literal has no consume effect")
			}
		}
	})}}
	if _, err := runCoreTable(t, testRun{settings: off, d: d}, "rules", build); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(steps, []string{Elaborate}) {
		t.Errorf("the steps %v ran, want elaborate alone", steps)
	}
	if elaborated != nil {
		t.Errorf("the module after elaborate: %v", elaborated)
	}
}

// The postcondition of elaborate is the structural Core verifier: a module that breaks it is the
// failure of the step elaborate. Its attributes are core.Analyze's by construction, so they are
// compared where the Core passes end: a pass that leaves them stale is caught there.
func TestDriverElaborateVerifiesStructureAndPassesTheAttributes(t *testing.T) {
	lit := func(finish core.FinishKind) func() (*core.Module, error) {
		return func() (*core.Module, error) {
			m := &core.Module{Exps: []string{`"a"`}}
			m.Rules = []*core.Rule{{Name: "r", Body: &core.Lit{Text: "a"}, Policy: core.Policy{Seen: -1}, Finish: core.Finish{Kind: finish}}}
			core.CallGraph(m)
			return m, nil
		}
	}
	if _, err := runCoreTable(t, testRun{}, "rules", lit(core.FinishAction)); !isStep(err, Elaborate) || !strings.Contains(err.Error(), "action finish without action") {
		t.Errorf("a module that breaks the structure of Core IR: %v, want the failure of elaborate", err)
	}
	stale := pipe.New(pipe.Def[*core.Module, *core.Module]{
		Decl: pipe.Decl{Name: "test-core-stale", Test: true, Obligation: "none: it leaves an attribute stale"},
		In:   corepass.IR, Out: corepass.IR,
		Run: func(_ *pipe.Ctx[*core.Module], m *core.Module) (*core.Module, pass.PreservedSet, error) {
			m.Rules[0].Body.Attrs().Eff.Set &^= core.EffConsume
			return m, pass.PreserveNone(), nil
		}})
	_, err := runCoreTable(t, testRun{edits: []pipe.Edit{pipe.Fill(CoreSlot, "", pipe.Seq(corepass.IR, stale))}}, "rules", lit(core.FinishNone))
	if err == nil || !strings.Contains(err.Error(), "verify core") || strings.Contains(err.Error(), "test-core-stale") {
		t.Errorf("a pass that leaves an attribute stale: %v, want the verification where the Core passes end", err)
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
	_, err := runCoreTable(t, testRun{edits: []pipe.Edit{pipe.Fill(CoreSlot, "", pipe.Seq(corepass.IR, failing))}}, "typed", empty)
	if !errors.Is(err, cause) || !strings.HasPrefix(err.Error(), "internal error: ") {
		t.Fatalf("error %v", err)
	}
	bad := errors.New("no such rule")
	m, err := runCoreTable(t, testRun{}, "typed", func() (*core.Module, error) { return nil, bad })
	if m != nil || !isStep(err, Elaborate) || !errors.Is(err, bad) || err.Error() != bad.Error() {
		t.Fatalf("a failing build: %v, %v", m, err)
	}
}

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

// flowPasses returns the edit that runs the Flow passes and then extra on each function of every table.
func flowPasses(extra ...*pipe.Pass[flowpass.Unit, flowpass.Unit]) pipe.Edit {
	return pipe.Fill(FlowSlot, "", pipe.Seq(code.FlowStage, append(slices.Clone(flowpass.Passes), extra...)...))
}

// withTestFlowPass adds a profile that turns testFlowPass (registered on in speed) off for the
// duration of the test, and returns the edit that runs the Flow passes with the pass.
func withTestFlowPass(t *testing.T) pipe.Edit {
	defs := profileDefs
	profileDefs = append(slices.Clone(profileDefs), profileDef{Name: "no-comment", Doc: "x", Values: map[string]int{testFlowPass: 0}})
	t.Cleanup(func() { profileDefs = defs })
	return flowPasses(testFlowComment)
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

// planned returns the planned table of the functions of u, each the body of a rule by its index.
func planned(u lowerdata.Unit) plandata.Planned {
	p := &plandata.Plan{}
	for i, f := range u.Funcs {
		p.Add(&plandata.FuncSpec{Key: plandata.Key{Use: plandata.RuleBody, Rule: i}, Func: f})
	}
	return plandata.Planned{Plan: p, IR: &plandata.TableIR{M: u.Module, Opts: u.Opts}}
}

// execFunc runs the pipeline of the planned table of framed(), labeled label, to the Exec stage as r
// says, with the target passes target and the parser's observability observe, and returns its
// function (execdata.View).
func execFunc(t *testing.T, r testRun, label string, target targetPipeline, observe bool) (*execdata.Lowered, error) {
	t.Helper()
	p := planned(framed())
	p.IR.Opts.Observe = observe
	x, err := runExecTable(t, r, label, p, target)
	if x == nil {
		return nil, err
	}
	return execdata.View(x.Module.Module(), 0), err
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

// execPasses returns the edit that runs the Exec passes and then extra on each function of every table.
func execPasses(extra ...*pipe.Pass[codepass.Unit, codepass.Unit]) pipe.Edit {
	return pipe.Fill(ExecSlot, "", pipe.Seq(code.ExecStage, append(slices.Clone(codepass.Passes), extra...)...))
}

// The driver lowers the functions of a table into one module and runs each pass on every function in
// turn, the Flow passes and then the Exec passes: the dumps show the module under the required steps
// and each function after each pass, pass by pass in the order of the functions, labeled with the
// table, and a pass runs on the functions in their order.
func TestDriverTable(t *testing.T) {
	record := testTableExec.Name()
	tableRuns = nil
	var dump strings.Builder
	d := &debugger{PrintAfter: []string{flowlower.Name, testFlowPass, flowpass.FinishName, execlower.Name, record}, Output: &dump}
	xt, err := runExecTable(t, testRun{d: d, edits: []pipe.Edit{withTestFlowPass(t), execPasses(testTableExec)}}, "rules", planned(table()), noTarget)
	if err != nil {
		t.Fatal(err)
	}
	x := xt.Module.Module()
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

// The pipeline of a planned table runs in one run, from its lowering to the Exec stage: the passes run
// on the functions of the table, and the table at the Exec stage holds the function of each use of the
// plan.
func TestDriverLowerTable(t *testing.T) {
	tableRuns = nil
	x, err := runExecTable(t, testRun{edits: []pipe.Edit{withTestFlowPass(t), execPasses(testTableExec)}}, "rules", planned(table()), noTarget)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tableRuns, []string{"r", "s"}) {
		t.Errorf("the Exec pass ran on %v, want r and s", tableRuns)
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
	d := &debugger{PrintAfter: []string{flowlower.Name, testFlowPass}, Output: &dump}
	f, err := runFlowTable(t, testRun{verifyEach: true, d: d, edits: []pipe.Edit{run}}, "rules", planned(framed()), flowpass.Target{})
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
		f, err := runFlowTable(t, testRun{settings: s, edits: []pipe.Edit{run}}, "rules", planned(framed()), flowpass.Target{})
		if err != nil {
			t.Fatal(err)
		}
		if comments(f) != 0 {
			t.Errorf("the pass ran although the %s turned it off", name)
		}
	}
	// The real pipeline prints the function after each pass that applies: for a framed function
	// of a target that has every form and no observability, every pass but terminals (local
	// functions only) and class-tables (a number, off at its default 0), then canonicalize, which
	// applies to every function, and flow-finish.
	all := flowpass.Target{Terminals: true, ClassRuns: true, ClassTables: true, GuardedClasses: true, InlineLiterals: true}
	dump.Reset()
	if _, err := runFlowTable(t, testRun{d: &debugger{PrintAfter: []string{PrintAll}, Output: &dump}}, "typed", planned(framed()), all); err != nil {
		t.Fatal(err)
	}
	var printed []string
	for _, line := range strings.Split(dump.String(), "\n") {
		if name, ok := strings.CutPrefix(line, "-- after "); ok {
			printed = append(printed, strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name, " --"), " [typed]"), " [typed/r]"))
		}
	}
	if want := []string{flowlower.Name, flowpass.PassGuardedClasses, flowpass.PassClassRuns, flowpass.PassInlineLiterals, codepass.PassCanonicalize, flowpass.FinishName}; !slices.Equal(printed, want) {
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
		f, err := runFlowTable(t, testRun{verifyEach: verifyEach, edits: []pipe.Edit{flowPasses(see...)}}, "typed", planned(framed()), all)
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
			t.Error("the run returned another function than the one the last pass changed")
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
		r    testRun
		err  error
	}{
		{"passes that succeed", testRun{}, nil},
		{"a failing pass", testRun{edits: []pipe.Edit{flowPasses(testFlowFail)}}, errBroken},
	} {
		u := framed()
		before := core.Print(u.Module, true)
		for range 2 {
			f, err := runFlowTable(t, c.r, "typed", planned(u), all)
			if !errors.Is(err, c.err) {
				t.Fatalf("%s: %v", c.what, err)
			}
			if len(flow.FuncOf(f).Body.Stmts) == 0 {
				t.Errorf("%s: the module that the run returned lost its statements", c.what)
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
	if f, err := runFlowTable(t, testRun{}, "rules", planned(u), flowpass.Target{}); f != nil || !isStep(err, flowlower.Name) {
		t.Errorf("a refused unit: %v, %v", f, err)
	}
	f, err := runFlowTable(t, testRun{edits: []pipe.Edit{flowPasses(testFlowFail)}}, "rules", planned(framed()), flowpass.Target{})
	if f == nil || !errors.Is(err, errBroken) || !strings.HasPrefix(err.Error(), "internal error: test-flow-fail [rules/r]: ") {
		t.Errorf("a failing pass: %v, %v", f, err)
	}
}

// A Flow pass that fails in the run of a table ends that run's optional passes: the Exec pass of the
// table runs on no function, while the required steps go on, so the table comes back at the Exec stage,
// with the internal error that names the failed pass.
func TestDriverLowerTableFlowFailure(t *testing.T) {
	tableRuns = nil
	x, err := runExecTable(t, testRun{edits: []pipe.Edit{flowPasses(testFlowFail), execPasses(testTableExec)}}, "rules", planned(table()), noTarget)
	if x == nil || !errors.Is(err, errBroken) || !strings.HasPrefix(err.Error(), "internal error: test-flow-fail [rules/r]: ") {
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

// noTarget is the pipeline of a target without Exec passes of its own.
var noTarget = pipe.Seq(execdata.LoweredIR)

// An Exec pass runs when its flag is on and a flag turns it off; the dumps show each function of the
// module as lowered under "execlower" and after each selected pass, and a pass that breaks the module is
// named with VerifyEach.
func TestDriverExec(t *testing.T) {
	run := execPasses(testExecDoc, testExecBreaking)
	var dump strings.Builder
	l, err := execFunc(t, testRun{d: &debugger{PrintAfter: []string{execlower.Name, testExecPass}, Output: &dump}, edits: []pipe.Edit{run}}, "rules", noTarget, false)
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
	if l, err := execFunc(t, testRun{settings: off, edits: []pipe.Edit{run}}, "rules", noTarget, false); err != nil || l.Func.Doc != "" {
		t.Errorf("the pass ran although the flag turned it off (%v)", err)
	}
	on, err := pass.Settings{}.With(testExecBreak, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = execFunc(t, testRun{settings: on, verifyEach: true, edits: []pipe.Edit{run}}, "rules", noTarget, false)
	if err == nil || !strings.Contains(err.Error(), testExecBreak) {
		t.Errorf("a broken module: %v", err)
	}
	// The real pipeline leaves the module alone and prints only the lowered functions: after
	// execlower, the module once and each function after each Exec pass.
	dump.Reset()
	if _, err := execFunc(t, testRun{d: &debugger{PrintAfter: []string{PrintAll}, Output: &dump}}, "typed", noTarget, true); err != nil {
		t.Fatal(err)
	}
	_, exec, _ := strings.Cut(dump.String(), "-- after "+execlower.Name+" ")
	if n := strings.Count(exec, "-- after "); exec == "" || n != len(codepass.Passes) {
		t.Errorf("all printed %d sections after execlower:\n%s", n, dump.String())
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
	testTargetFail = pipe.New(pipe.Def[*execdata.Lowered, *execdata.Lowered]{
		Decl: pipe.Decl{Name: "test-target-fail", Test: true, Obligation: "none: it fails"},
		In:   execdata.LoweredIR, Out: execdata.LoweredIR,
		Run: func(_ *pipe.Ctx[*execdata.Lowered], l *execdata.Lowered) (*execdata.Lowered, pass.PreservedSet, error) {
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
	printed := func(l *execdata.Lowered) string { return code.FuncStringIn(l.Module, l.Func) }
	whole, err := execFunc(t, testRun{}, "typed", noTarget, false)
	if err != nil {
		t.Fatal(err)
	}
	want := printed(whole)
	if len(whole.Func.Body.Stmts) == 0 {
		t.Fatal("the lowered function has no statements to lose")
	}
	for _, c := range []struct {
		what   string
		r      testRun
		target targetPipeline
		failed string
	}{
		{"a pass that every target runs", testRun{edits: []pipe.Edit{execPasses(testExecFail)}}, noTarget, testExecFail.Name()},
		{"a pass of the target", testRun{}, pipe.Seq(execdata.LoweredIR, testTargetFail), testTargetFail.Name()},
	} {
		l, err := execFunc(t, c.r, "typed", c.target, false)
		if !errors.Is(err, errBroken) || !strings.HasPrefix(err.Error(), "internal error: "+c.failed+" [typed/r]: ") {
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

// One compilation numbers the decided runs of the optional passes once, in the order they are decided: the
// Core, Flow and Exec stages of every table of a Go target, run in one run, give one strictly
// increasing numbering, which no stage restarts.
func TestDriverNumbersTheCompilationOnce(t *testing.T) {
	p, err := checkSource(t, "@start\nrule A = [a-z]+ B / \"x\"\nrule B = \"b\"*\n", common.CompileOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	speed, err := GenSettings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int
	kinds := map[string]bool{}
	rec := &pipetest.Watch{Start: func(e pipe.Event) {
		if e.Seq != 0 {
			seqs = append(seqs, e.Seq)
			kinds[e.Step.In] = true
		}
	}}
	gen := common.GenOptions{Module: "example.com/p", Lib: common.LibImport, LibGo: libsrc.FS, Recognize: true}
	if _, err := Compile(context.Background(), p, Options{Settings: speed, Observers: []pipe.Observer{rec}}, Target{Backend: result.Go, Gen: gen}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{corepass.IR.Name, code.FlowStage.Name, code.ExecStage.Name} {
		if !kinds[k] {
			t.Errorf("no run of an optional pass on %s is numbered: %v", k, kinds)
		}
	}
	for i := range seqs {
		if seqs[i] != i+1 {
			t.Fatalf("the runs are numbered %v, want 1 to %d in order", seqs, len(seqs))
		}
	}
}
