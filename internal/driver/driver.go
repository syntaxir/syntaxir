package driver

import (
	"fmt"
	"io"
	"slices"

	gopass "zeroparse.dev/zp/internal/backend/codegen/golang/pass"
	tspass "zeroparse.dev/zp/internal/backend/codegen/typescript/pass"
	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/code/flow"
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/middleend/codepass"
	"zeroparse.dev/zp/internal/middleend/corepass"
	"zeroparse.dev/zp/internal/middleend/execlower"
	"zeroparse.dev/zp/internal/middleend/flowlower"
	"zeroparse.dev/zp/internal/middleend/flowpass"
	"zeroparse.dev/zp/internal/pass"
)

// A Driver runs the pipeline of a compilation under one Settings value: it turns the passes on and
// off by their flags, verifies the unit after each pass when asked, and writes the dumps that
// PrintAfter selects. Every backend that lowers from the IR runs its pipeline through a Driver, so a
// profile or a flag means the same passes whatever the backend. The zero value runs the speed
// profile, verifies once at the end of each stage and dumps nothing.
//
// One Driver value is one compilation: its stages run under one manager (pass.Run), created when the
// first stage runs, which numbers the runs of optional passes across the tables and functions of the
// compilation and tells the observers (the dumps, CoreHook) about every step. A copy of a Driver
// that has not run yet is a compilation of its own.
//
// Each stage consumes the unit it is given (pass.Stage.Run): Core takes the module that build makes,
// Flow the module that flowlower makes of the functions of a table and Exec the module of Flow. The
// Core passes change their module in place, since a failure drops it. The Flow and Exec passes of a
// function keep their unit (pass.Transform.Keep), since after one of them fails the required steps and
// the printer finish the function from the unit as it was before them: they change one copy of it.
// What the caller keeps is the table's Core module, which Flow lowers the functions of the table from.
type Driver struct {
	// Opt holds the optimizations that are on.
	Opt pass.Settings
	// PrintAfter lists the names of the steps and passes (PrintNames) after which a unit is written
	// to PrintTo; "all" selects every step and pass.
	PrintAfter []string
	PrintTo    io.Writer
	// VerifyEach verifies the unit after every pass instead of once at the end, so that the error
	// names the first pass that broke an invariant.
	VerifyEach bool
	Hooks

	// run is the manager of the compilation, nil until the first stage runs.
	run *pass.Run
}

// Hooks are the test hooks of the pipelines, which replace or observe a stage (generate.Hooks).
type Hooks struct {
	// CoreHook, if set, is called with the name of the step and the Core module after elaborate
	// built it and after each Core pass that ran, before it is verified.
	CoreHook func(pass string, m *core.Module)
	// CorePipeline, FlowPipeline and ExecPipeline, if set, return the pipeline of the Core, Flow
	// or Exec passes that runs on the units of the named table in place of corepass.Pipeline,
	// flowpass.Pipeline or codepass.Pipeline, for tests that run passes of their own.
	CorePipeline func(table string) *pass.Pipeline[*core.Module, *corepass.Env]
	FlowPipeline func(table string) *pass.Pipeline[*code.Module, *flowpass.Env]
	ExecPipeline func(table string) *pass.Pipeline[*code.Module, *codepass.Env]
	// TestPasses names passes of those pipelines that are no registered optimization: no flag turns
	// them off, so they run wherever they apply.
	TestPasses []string
}

// Settings returns the optimizations that are on (Opt), as common.Runner requires.
func (d *Driver) Settings() pass.Settings { return d.Opt }

// prints reports whether the unit is dumped after the step whose dump name is name.
func (d *Driver) prints(name string) bool {
	return d.PrintTo != nil && name != "" && (slices.Contains(d.PrintAfter, name) || slices.Contains(d.PrintAfter, PrintAll))
}

// enabled reports whether the optional pass of the given name may run: a test pass always may,
// a registered optimization when it is on.
func (d *Driver) enabled(name string) bool {
	return slices.Contains(d.TestPasses, name) || d.Opt.On(name)
}

// manager returns the manager of the compilation, created on first use.
func (d *Driver) manager() *pass.Run {
	if d.run == nil {
		d.run = &pass.Run{Enabled: d.enabled, VerifyEach: d.VerifyEach, Observers: []pass.Observer{observer{d}}}
	}
	return d.run
}

// observer is the observer of the steps of a compilation that writes the dumps of PrintAfter and
// calls CoreHook. It reads the fields of the driver when it is told of a step, so that a hook set
// between two stages sees the later one. A dump is one header line, which names the step and the
// label of the unit (its table, and its function for a unit of one function), and the listing of
// the unit as its IR prints it (pass.IR.Print).
type observer struct{ d *Driver }

func (observer) Before(pass.Event) bool { return true }

func (o observer) After(e pass.Event, print func(io.Writer), unit any) {
	if o.d.CoreHook != nil && e.Step.Out == corepass.Stage {
		o.d.CoreHook(e.Step.Dump, unit.(*core.Module))
	}
	if o.d.prints(e.Step.Dump) {
		fmt.Fprintf(o.d.PrintTo, "-- after %s [%s] --\n", e.Step.Dump, e.Label)
		print(o.d.PrintTo)
	}
}

// Elaborate is the name of the step that builds the Core IR of a table from its elaborated program,
// the first step of the pipeline.
const Elaborate = "elaborate"

// elaborating is the step Elaborate: it runs the build of the Core IR module that it is given.
var elaborating = pass.Lower(Elaborate, "program", corepass.IR, func(build func() (*core.Module, error)) (*core.Module, error) {
	return build()
})

// Core runs the Core part of the pipeline for the table named table: the step elaborate, which
// runs build, the elaboration of the table's program to Core IR, and then the Core passes with
// the optimizations of the driver. It returns the module after them. The module is dumped as
// "elaborate" as build made it.
//
// A failure of build is returned as the *pass.StepError that names elaborate, with its message and
// no module. A failure of a pass is an internal error that names the pass.
func (d *Driver) Core(table string, build func() (*core.Module, error)) (*core.Module, error) {
	p := corepass.Pipeline
	if d.CorePipeline != nil {
		p = d.CorePipeline(table)
	}
	s := pass.Then(elaborating, pass.Transforms(pass.Transform[*core.Module, *corepass.Env]{
		IR: corepass.IR, Pipeline: p,
		Env: func(*pass.Run, pass.Label, *core.Module) *corepass.Env {
			return &corepass.Env{Flag: d.Opt.On}
		},
	}))
	m, err := s.Run(d.manager(), pass.Label{Table: table}, build)
	if err != nil {
		return nil, stepError(err)
	}
	return m, nil
}

// Flow runs the Flow part of the pipeline on the functions of the table named table: the lowering
// flowlower of u, which its postcondition verifies, the Flow passes with the optimizations of the
// driver on each function of the module, pass by pass (pass.EachFunc), and flow-finish, which also
// runs after a failed pass. observe says that the parser reports its rules and skipped alternatives,
// and target what the backend prints (flowpass.Env). The module is dumped as "flowlower" as the
// lowering made it and as "flow-finish" as it leaves the Flow stage, and each function after each
// Flow pass, labeled with its table.
//
// The Core module of u stays the caller's: flowlower only reads it, and the module it makes shares
// the Core nodes and rules with it, which the Flow passes and flow-finish read and never change
// (flow.Forms). So Flow consumes only the module that the lowering made, and copies nothing for the
// Core module.
//
// A failure of flowlower is returned as the *pass.StepError that names it, with no module. A failure
// of a pass is an internal error that names the pass, returned with the module, in which the function
// that the pass failed on is as the lowering made it, and flow-finish has ended every function, for a
// generation that fails with it.
func (d *Driver) Flow(table string, u flowlower.Unit, observe bool, target flowpass.Target) (*code.Module, error) {
	p := flowpass.Pipeline
	if d.FlowPipeline != nil {
		p = d.FlowPipeline(table)
	}
	s := pass.Then(flowlower.Lowering, pass.Then(pass.EachFunc(moduleFuncs, pass.Transform[*code.Module, *flowpass.Env]{
		IR: flowlower.IR, Pipeline: p, BuiltVerified: true, Keep: true,
		Env: func(_ *pass.Run, _ pass.Label, m *code.Module) *flowpass.Env {
			return &flowpass.Env{Func: flow.AttrsOf(flow.FuncOf(m)), Observe: observe, Flag: d.Opt.On, Value: d.Opt.Value, Target: target}
		},
	}), flowpass.Finishing))
	m, err := s.Run(d.manager(), pass.Label{Table: table}, u)
	return m, stepError(err)
}

// Exec runs the Exec part of the pipeline on the module m of the functions of the table named table,
// which Flow returned: the conversion execlower, which its postcondition verifies, the Exec passes
// that every target runs and then target, the pipeline of the backend's own Exec passes, with the
// optimizations of the driver, each on every function of the module pass by pass (pass.EachFunc);
// observe is as for Flow. The module is dumped as "execlower" as the conversion made it, and each
// function after each Exec pass and each pass of target, labeled with its table.
//
// The passes of target run on each function as a lowered function (execlower.View), which keeps what
// the printers print: the function in its module with its variables (execlower.Lowered.Keeps), which
// the verifier of the lowered function (execlower.IR) checks after the passes of target, as
// Pipeline.Run verifies.
//
// m is consumed: execlower moves its functions into the result. A failure of execlower is returned
// as the *pass.StepError that names it, with no result. A failure of a pass or of a verification is
// an internal error that names it, returned with the result, in which the function that it failed on
// is as the failed run of passes got it (pass.Options.Keep), for a generation that fails with it.
func (d *Driver) Exec(table string, m *code.Module, target *TargetPipeline, observe bool) (*code.Module, error) {
	p := codepass.Pipeline
	if d.ExecPipeline != nil {
		p = d.ExecPipeline(table)
	}
	env := func(*pass.Run, pass.Label, any) *codepass.Env { return &codepass.Env{Observe: observe, Flag: d.Opt.On} }
	s := pass.Then(execlower.Lowering, pass.Then(pass.EachFunc(moduleFuncs, pass.Transform[*code.Module, *codepass.Env]{
		IR: execModuleIR, Pipeline: p, BuiltVerified: true, Keep: true,
		Env: func(r *pass.Run, lb pass.Label, m *code.Module) *codepass.Env { return env(r, lb, m) },
	}), pass.EachFunc(loweredFuncs, pass.Transform[*execlower.Lowered, *codepass.Env]{
		IR: loweredIR, Pipeline: target, BuiltVerified: true, Keep: true,
		Env: func(r *pass.Run, lb pass.Label, l *execlower.Lowered) *codepass.Env { return env(r, lb, l) },
	})))
	x, err := s.Run(d.manager(), pass.Label{Table: table}, m)
	return x, stepError(err)
}

// moduleFuncs runs the passes of one function on each function of a module of Code IR: on a module
// that declares what the module declares and holds that function alone (code.Module.FuncUnit).
var moduleFuncs = pass.Funcs[*code.Module, *code.Module]{
	Len:  func(m *code.Module) int { return len(m.Funcs) },
	Name: func(m *code.Module, i int) string { return m.Funcs[i].Name },
	Get:  (*code.Module).FuncUnit,
	Put:  (*code.Module).PutFuncUnit,
}

// loweredFuncs runs the passes of a target on each function of a module at the Exec stage, as a
// lowered function of the unit of that function (execlower.View).
var loweredFuncs = pass.Funcs[*code.Module, *execlower.Lowered]{
	Len:  func(m *code.Module) int { return len(m.Funcs) },
	Name: func(m *code.Module, i int) string { return m.Funcs[i].Name },
	Get:  func(m *code.Module, i int) *execlower.Lowered { return execlower.View(m.FuncUnit(i), 0) },
	Put: func(m *code.Module, i int, l *execlower.Lowered) error {
		if err := l.Keeps(); err != nil {
			return err
		}
		if l.Module.Funcs[0] != l.Func {
			return fmt.Errorf("an Exec pass moved the function %s in its module", l.Func.Name)
		}
		return m.PutFuncUnit(i, l.Module)
	},
}

// stepError returns the failure of a required step as it is, and the failure of a pass as an
// internal error.
func stepError(err error) error {
	if _, ok := err.(*pass.StepError); ok || err == nil {
		return err
	}
	return fmt.Errorf("internal error: %w", err)
}

// execModuleIR describes the Exec module of a lowered function to the Exec passes: each function of
// it is dumped.
var execModuleIR = pass.IR[*code.Module]{Stage: codepass.StageName, Verify: code.Verify, Print: code.PrintFuncs, Copy: (*code.Module).Clone}

// loweredIR describes a lowered function to the passes of a target: only the function is dumped.
var loweredIR = pass.IR[*execlower.Lowered]{
	Stage:  codepass.StageName,
	Verify: execlower.IR.Verify,
	Copy:   (*execlower.Lowered).Clone,
	Print: func(w io.Writer, l *execlower.Lowered) {
		fmt.Fprintf(w, "%s\n", code.FuncStringIn(l.Module, l.Func))
	},
}

// TargetPipeline is the pipeline of the Exec passes of one target (golang/pass, typescript/pass), which
// run on a lowered function after the Exec passes that every target runs (Exec).
type TargetPipeline = pass.Pipeline[*execlower.Lowered, *codepass.Env]

// targets holds the pipelines of the targets' Exec passes, whose passes -print-after names
// (PrintNames).
var targets = []*TargetPipeline{gopass.Pipeline, tspass.Pipeline}
