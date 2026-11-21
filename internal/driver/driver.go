package driver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	gopass "zeroparse.dev/zp/internal/backend/codegen/golang/pass"
	tspass "zeroparse.dev/zp/internal/backend/codegen/typescript/pass"
	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/middleend/codepass"
	"zeroparse.dev/zp/internal/middleend/corepass"
	"zeroparse.dev/zp/internal/middleend/execlower"
	"zeroparse.dev/zp/internal/middleend/flowlower"
	"zeroparse.dev/zp/internal/middleend/flowlower/lowerdata"
	"zeroparse.dev/zp/internal/middleend/flowpass"
	"zeroparse.dev/zp/internal/pass"
	"zeroparse.dev/zp/internal/pass/pipe"
)

// A Driver runs the pipeline of a compilation under one Settings value: it turns the passes on and
// off by their flags, verifies the unit after each pass when asked, and writes the dumps that
// PrintAfter selects. Every backend that lowers from the IR runs its pipeline through a Driver, so a
// profile or a flag means the same passes whatever the backend. The zero value runs the speed
// profile and dumps nothing; it verifies a unit where a run of its passes ends only if an optional
// pass of that run ran, and the output of each lowering, never after a required step alone.
//
// Each stage runs through the manager of internal/pass/pipe (pipe.Execute) and consumes the unit it is
// given: Core takes the module that build makes, Flow the module that flowlower makes of the functions
// of a table and Exec the module of Flow. The Core passes change their module in place, since a
// failure drops it. The Flow and Exec passes run on each function of their module, which the manager
// copies once before the first pass that runs, since after one of them fails on a function the
// required steps and the printer finish that function from the module as it was before them. What the
// caller keeps is the table's Core module, which Flow lowers the functions of the table from.
type Driver struct {
	// Opt holds the optimizations that are on.
	Opt pass.Settings
	// PrintAfter lists the names of the steps and passes (PrintNames) after which a unit is written
	// to PrintTo; "all" selects every step and pass.
	PrintAfter []string
	PrintTo    io.Writer
	// VerifyEach verifies the unit after every pass and step, required ones included, instead of
	// only where a run of passes ends and only if an optional pass of it ran, so that the error
	// names the first pass that broke an invariant.
	VerifyEach bool
	Hooks
	// numbering numbers the decided runs of the optional passes of every stage the driver runs, on
	// every table, once (pipe.Numbering): a compilation runs its stages through one driver (one
	// Runner of generate.Passes). pipeRun makes it on first use. It becomes internal to the manager
	// once one Execute runs the whole compilation (record 074, migration step 6).
	numbering *pipe.Numbering
	// observers watch every run of the driver as well, for the tests of this package.
	observers []pipe.Observer
}

// Hooks are the test hooks of the pipelines, which replace or observe a stage (generate.Hooks). A
// pass of a pipeline of a hook that is no registered optimization declares that it is a test pass
// (pipe.Decl.Test), which no flag turns off.
type Hooks struct {
	// CoreHook, if set, is called with the name of the step and the Core module after elaborate
	// built it and after each Core pass that ran, before it is verified.
	CoreHook func(pass string, m *core.Module)
	// CorePipeline, FlowPipeline and ExecPipeline, if set, return the pipeline of the Core, Flow
	// or Exec passes that runs on the module of the named table in place of corepass.Pipeline,
	// flowpass.Pipeline or codepass.Pipeline, for tests that run passes of their own. A Flow or Exec
	// pipeline runs its passes on each function (flowpass.OnFuncs, codepass.OnFuncs).
	CorePipeline func(table string) pipe.Pipeline[*core.Module, *core.Module]
	FlowPipeline func(table string) pipe.Pipeline[flowpass.Unit, flowpass.Unit]
	ExecPipeline func(table string) pipe.Pipeline[codepass.Unit, codepass.Unit]
}

// Settings returns the optimizations that are on (Opt), as common.Runner requires.
func (d *Driver) Settings() pass.Settings { return d.Opt }

// prints reports whether the unit is dumped after the step whose dump name is name.
func (d *Driver) prints(name string) bool {
	return d.PrintTo != nil && name != "" && (slices.Contains(d.PrintAfter, name) || slices.Contains(d.PrintAfter, PrintAll))
}

// Elaborate is the name of the step that builds the Core IR of a table from its elaborated program,
// the first step of the pipeline.
const Elaborate = "elaborate"

// programIR is the kind of the build of the Core IR of a table, which elaborate runs.
var programIR = pipe.RegisterIR(&pipe.IR[func() (*core.Module, error)]{Name: "program"})

// elaborating is the step Elaborate: it runs the build of the Core IR module that it is given and
// computes the attributes of its nodes (core.Analyze), so that the module it returns is a Core module
// with its attributes, as the Core passes and the Core verifier read it.
var elaborating = pipe.New(pipe.Def[func() (*core.Module, error), *core.Module]{
	Decl: pipe.Decl{Name: Elaborate, Required: true, Obligation: "The module is the elaborated program in Core IR, with its attributes."},
	In:   programIR, Out: corepass.IR,
	Run: func(_ *pipe.Ctx[func() (*core.Module, error)], build func() (*core.Module, error)) (*core.Module, pass.PreservedSet, error) {
		m, err := build()
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		core.Analyze(m)
		return m, pass.PreserveNone(), nil
	},
})

// Core runs the Core part of the pipeline for the table named table: the step elaborate, which
// runs build, the elaboration of the table's program to Core IR, and then the Core passes with
// the optimizations of the driver. It returns the module after them. The module is dumped as
// "elaborate" as build made it, with the attributes of its nodes.
//
// The passes change the module in place: a failure drops it (pipe.Run.Stop). A failure of build is
// returned as the *pass.StepError that names elaborate, with its message and no module. A failure
// of a pass is an internal error that names the pass.
func (d *Driver) Core(table string, build func() (*core.Module, error)) (*core.Module, error) {
	p := corepass.Pipeline()
	if d.CorePipeline != nil {
		p = d.CorePipeline(table)
	}
	m, err := pipe.Execute(context.Background(), d.pipeRun(table, true), pipe.Then(pipe.Of(elaborating), p), build)
	if err != nil {
		return nil, pipeError(err, Elaborate)
	}
	return m, nil
}

// pipeRun returns the manager of a stage of the pipeline that runs on the units of the table named
// table under the settings of the driver, numbered after the stages that the driver ran before; stop
// says that the stage consumes its unit (pipe.Run.Stop).
func (d *Driver) pipeRun(table string, stop bool) *pipe.Run {
	if d.numbering == nil {
		d.numbering = &pipe.Numbering{}
	}
	r := &pipe.Run{Settings: d.Opt, VerifyEach: d.VerifyEach, Stop: stop, Numbering: d.numbering}
	if o := (pipeObserver{d: d, table: table}); o.Needs() != (pipe.Needs{}) {
		r.Observers = []pipe.Observer{o}
	}
	r.Observers = append(r.Observers, d.observers...)
	return r
}

// pipeObserver writes the dumps of PrintAfter and calls CoreHook for the steps of a stage that runs
// through the manager of internal/pass/pipe. A stage runs on the units of one table, which the
// observer adds to the label of each dump.
type pipeObserver struct {
	d     *Driver
	table string
}

func (o pipeObserver) Needs() pipe.Needs {
	return pipe.Needs{Units: o.d.CoreHook != nil, Print: o.d.PrintTo != nil && len(o.d.PrintAfter) > 0}
}

func (pipeObserver) Before(pipe.Event, func(io.Writer)) bool { return true }

func (o pipeObserver) After(e pipe.Event, r *pipe.Result) {
	if !r.Ran || r.Err != nil {
		return
	}
	if m, ok := r.Unit.(*core.Module); ok && o.d.CoreHook != nil {
		o.d.CoreHook(e.Step.Instance, m)
	}
	if r.Print != nil && o.d.prints(e.Step.Instance) {
		l := pass.Label{Table: o.table, Func: e.Label.Func}
		fmt.Fprintf(o.d.PrintTo, "-- after %s [%s] --\n", e.Step.Instance, l)
		r.Print(o.d.PrintTo)
	}
}

// pipeError returns the failure of one of the required steps named required as the *pass.StepError
// that names it, with the message of the step, and any other failure (a pass, a verification) as an
// internal error.
func pipeError(err error, required ...string) error {
	var se *pipe.StepError
	if errors.As(err, &se) && slices.Contains(required, se.Step) {
		return &pass.StepError{Step: pass.Step{Name: se.Step, Required: true}, Err: se.Err}
	}
	return fmt.Errorf("internal error: %w", err)
}

// Flow runs the Flow part of the pipeline on the functions of the table named table: the lowering
// flowlower of u, which its postcondition verifies, the Flow passes with the optimizations of the
// driver on each function of the module, pass by pass (pipe.PerFunc), and flow-finish, which also
// runs after a failed pass. observe says that the parser reports its rules and skipped alternatives,
// and target what the backend prints (flowpass.ObserveFeature, flowpass.TargetFeature). The module is
// dumped as "flowlower" as the lowering made it and as "flow-finish" as it leaves the Flow stage, and
// each function after each Flow pass, labeled with its table.
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
func (d *Driver) Flow(table string, u lowerdata.Unit, observe bool, target flowpass.Target) (*code.Module, error) {
	p := flowpass.Pipeline()
	if d.FlowPipeline != nil {
		p = d.FlowPipeline(table)
	}
	s := pipe.Then3(pipe.Of(flowlower.Lowering),
		pipe.WithConst(flowpass.ObserveFeature, observe, pipe.WithConst(flowpass.TargetFeature, target, p)),
		pipe.Of(flowpass.Finishing))
	m, err := pipe.Execute(context.Background(), d.pipeRun(table, false), s, u)
	if err != nil {
		return m.Module(), pipeError(err, flowlower.Name)
	}
	return m.Module(), nil
}

// Exec runs the Exec part of the pipeline on the module m of the functions of the table named table,
// which Flow returned: the conversion execlower, which its postcondition verifies, the Exec passes
// that every target runs and then target, the pipeline of the backend's own Exec passes, with the
// optimizations of the driver, each on every function of the module pass by pass (pipe.PerFunc). The
// module is dumped as "execlower" as the conversion made it, and each function after each Exec pass
// and each pass of target, labeled with its table. observe is as for Flow: the feature
// flowpass.ObserveFeature, which no Exec pass reads yet.
//
// The passes of target run on each function as a lowered function (execlower.LoweredFuncs), which
// keeps what the printers print: the function in its module with its variables (execlower.Lowered.Keeps),
// which the verifier of the lowered function (execlower.LoweredIR) checks after the passes of target.
//
// m is consumed: execlower moves its functions into the result. A failure of execlower is returned
// as the *pass.StepError that names it, with no result. A failure of a pass or of a verification is
// an internal error that names it, returned with the result, in which the function that it failed on
// is as it was before the passes of its segment, for a generation that fails with it.
func (d *Driver) Exec(table string, m *code.Module, target TargetPipeline, observe bool) (*code.Module, error) {
	p := codepass.Pipeline()
	if d.ExecPipeline != nil {
		p = d.ExecPipeline(table)
	}
	s := pipe.Then(pipe.Of(execlower.Lowering), pipe.WithConst(flowpass.ObserveFeature, observe,
		pipe.Then(p, pipe.PerFunc(execlower.LoweredFuncs, target))))
	// The module is at the Flow stage, as Flow made it.
	x, err := pipe.Execute(context.Background(), d.pipeRun(table, false), s, code.At[code.Flow]{}.With(m))
	if err != nil {
		return x.Module(), pipeError(err, execlower.Name)
	}
	return x.Module(), nil
}

// TargetPipeline is the pipeline of the Exec passes of one target (golang/pass, typescript/pass), which
// run on a lowered function after the Exec passes that every target runs (Exec).
type TargetPipeline = pipe.Pipeline[*execlower.Lowered, *execlower.Lowered]

// targets holds the Exec passes of the targets, which -print-after names (PrintNames).
var targets = [][]*pipe.Pass[*execlower.Lowered, *execlower.Lowered]{gopass.Passes, tspass.Passes}
