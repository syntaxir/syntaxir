package driver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"text/tabwriter"
	"time"

	gopass "syntaxir.dev/sxir/internal/backend/codegen/golang/pass"
	tspass "syntaxir.dev/sxir/internal/backend/codegen/typescript/pass"
	"syntaxir.dev/sxir/internal/ir/code"
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

// A Driver runs the pipeline of a compilation under one Settings value: it turns the passes on and
// off by their flags, verifies the unit after each pass when asked, and writes the dumps and the
// reports that its debugging settings select to Output. Every backend that lowers from the IR runs its
// pipeline through a Driver, so a profile or a flag means the same passes whatever the backend. The
// zero value runs the speed profile and writes nothing; it verifies a unit where a run of its passes
// ends only if an optional pass of that run ran, and the output of each lowering, never after a
// required step alone.
//
// Each stage runs through the manager of internal/pass/pipe (pipe.Execute) and consumes the unit it is
// given: Core takes the module that build makes, Flow the module that flowlower makes of the functions
// of a table and Exec the module of Flow. The Core passes change their module in place, since a
// failure drops it. The Flow and Exec passes run on each function of their module, which the manager
// copies once before the first pass that runs, since after one of them fails on a function the
// required steps and the printer finish that function from the module as it was before them. What the
// caller keeps is the table's Core module, which Flow lowers the functions of the table from.
//
// The debugging settings observe the run and never change what it makes. Everything they write goes to
// Output in the order the steps run; the statistics and the times that the driver collects over all
// the stages it runs are written once, by Report, after the build. Their text is a debugging aid,
// whose form and names may change between versions.
type Driver struct {
	// Opt holds the optimizations that are on.
	Opt pass.Settings
	// PrintAfter lists the names of the steps and passes (PrintNames) after which a unit is written
	// to Output; "all" selects every step and pass.
	PrintAfter []string
	// PrintBefore lists the names of the passes (PrintBeforeNames) before whose runs the unit they
	// receive is written to Output; "all" selects every pass. A run that does not happen, because the
	// pass is off or does not apply, writes nothing.
	PrintBefore []string
	// PrintChanged writes a unit to Output after each transform that changed its text form, and,
	// before the first such dump of the unit, the unit as that transform received it. A step that
	// PrintAfter also selects is written once.
	PrintChanged bool
	// VerifyEach verifies the unit after every pass and step, required ones included, instead of
	// only where a run of passes ends and only if an optional pass of it ran, so that the error
	// names the first pass that broke an invariant.
	VerifyEach bool
	// Stats collects the statistics of every pass instance (pipe.Stats) for Report.
	Stats bool
	// TimePasses collects the wall time of every step instance for Report.
	TimePasses bool
	// OptBisectLimit, if set, lets run only the runs of the optional passes and the applications of
	// their patterns numbered up to the limit (pipe.Event.Seq), over every stage the driver runs, and
	// vetoes the later ones; the required steps always run. Each decision is written to Output as
	// "opt-bisect: <number> run|skip <instance> [<unit>]".
	OptBisectLimit *int
	// Output receives the dumps and the reports; nil writes nothing, whatever the settings above.
	Output io.Writer
	Hooks
	// numbering numbers the decided runs of the optional passes of every stage the driver runs, on
	// every table, once (pipe.Numbering): a compilation runs its stages through one driver (one
	// Runner of generate.Passes). pipeRun makes it on first use. It becomes internal to the manager
	// once one Execute runs the whole compilation (record 074, migration step 6).
	numbering *pipe.Numbering
	// watch holds what the observers of the driver collect over the stages it runs; pipeRun makes
	// it on first use.
	watch *watch
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
	// Observers watch every step of every stage the driver runs, after the dumps of the driver, such
	// as pipe.Stats for tests that pin the statistics of a pass.
	Observers []pipe.Observer
}

// Settings returns the optimizations that are on (Opt), as common.Runner requires.
func (d *Driver) Settings() pass.Settings { return d.Opt }

// prints reports whether the unit is dumped after the runs of the step or pass named name: of every
// instance of it, as its dumps name them.
func (d *Driver) prints(name string) bool {
	return d.Output != nil && name != "" && (slices.Contains(d.PrintAfter, name) || slices.Contains(d.PrintAfter, PrintAll))
}

// printsBefore reports whether the unit is dumped before the runs of the pass named name, of every
// instance of it.
func (d *Driver) printsBefore(name string) bool {
	return d.Output != nil && (slices.Contains(d.PrintBefore, name) || slices.Contains(d.PrintBefore, PrintAll))
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
	m, err := execute(d, d.pipeRun(table, true), pipe.Then(pipe.Of(elaborating), p), build)
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
	if d.watch == nil {
		d.watch = &watch{}
	}
	r := &pipe.Run{Settings: d.Opt, VerifyEach: d.VerifyEach, Stop: stop, Numbering: d.numbering}
	if o := (pipeObserver{d: d, table: table}); o.Needs() != (pipe.Needs{}) {
		r.Observers = []pipe.Observer{o}
	}
	if d.Output != nil && d.Stats {
		if d.watch.stats == nil {
			d.watch.stats = pipe.NewStats()
		}
		r.Observers = append(r.Observers, d.watch.stats)
	}
	if d.Output != nil && d.TimePasses {
		r.Observers = append(r.Observers, (*timer)(d.watch))
	}
	if d.OptBisectLimit != nil {
		r.Observers = append(r.Observers, bisector{d: d, table: table})
	}
	r.Observers = append(r.Observers, d.Observers...)
	return r
}

// execute runs the pipeline p on in with r (pipe.Execute) and adds the time it took to the total of
// the times that Report writes.
func execute[I, O any](d *Driver, r *pipe.Run, p pipe.Pipeline[I, O], in I) (O, error) {
	if d.Output == nil || !d.TimePasses {
		return pipe.Execute(context.Background(), r, p, in)
	}
	start := time.Now()
	out, err := pipe.Execute(context.Background(), r, p, in)
	d.watch.total += time.Since(start)
	return out, err
}

// pipeObserver writes the dumps of PrintBefore, PrintAfter and PrintChanged and calls CoreHook for the steps of a
// stage that runs through the manager of internal/pass/pipe. A stage runs on the units of one table,
// which the observer adds to the label of each dump; the label of a step in a branch of a Fork names
// its backend first, as the reports do, since the debugging settings and their writer are one for
// every target of a compilation.
type pipeObserver struct {
	d     *Driver
	table string
}

func (o pipeObserver) Needs() pipe.Needs {
	changed := o.d.Output != nil && o.d.PrintChanged
	return pipe.Needs{
		Units:       o.d.CoreHook != nil,
		Print:       o.d.Output != nil && (len(o.d.PrintAfter) > 0 || o.d.PrintChanged),
		Before:      changed || o.d.Output != nil && len(o.d.PrintBefore) > 0,
		TextChanged: changed,
	}
}

// Before keeps the dump of the input of a step, which PrintBefore writes before the dumps after the
// run, and PrintChanged before the first change of the unit. Both are written by After, once the run
// is known to have happened: a run that an observer vetoed is not dumped.
func (o pipeObserver) Before(e pipe.Event, in func(io.Writer)) bool {
	if !e.Step.Pattern {
		o.d.watch.input = in
	}
	return true
}

func (o pipeObserver) After(e pipe.Event, r *pipe.Result) {
	if e.Step.Pattern || !r.Ran {
		return
	}
	l := qualified(e.Label.Backend, pass.Label{Table: o.table, Func: e.Label.Func}.String())
	before := !e.Step.Required && o.d.printsBefore(e.Step.Name) && o.d.watch.input != nil
	if before {
		// Also before a run that failed, whose input is what to look at.
		fmt.Fprintf(o.d.Output, "-- before %s [%s] --\n", e.Step.Instance, l)
		o.d.watch.input(o.d.Output)
	}
	if r.Err != nil {
		return
	}
	if m, ok := r.Unit.(*core.Module); ok && o.d.CoreHook != nil {
		o.d.CoreHook(e.Step.Instance, m)
	}
	if r.Print == nil {
		return
	}
	changed := o.d.PrintChanged && e.Step.In == e.Step.Out && r.TextChanged
	if changed {
		w := o.d.watch
		unit := l + "@" + e.Step.In
		if w.printed == nil {
			w.printed = map[string]bool{}
		}
		if !w.printed[unit] && w.input != nil && !before {
			fmt.Fprintf(o.d.Output, "-- before %s [%s] --\n", e.Step.Instance, l)
			w.input(o.d.Output)
		}
		w.printed[unit] = true
	}
	if changed || o.d.prints(e.Step.Name) {
		fmt.Fprintf(o.d.Output, "-- after %s [%s] --\n", e.Step.Instance, l)
		r.Print(o.d.Output)
	}
}

// bisector is the observer of OptBisectLimit: it vetoes the runs of optional passes and the
// applications of patterns numbered after the limit, and writes each decision to Output.
type bisector struct {
	d     *Driver
	table string
}

func (bisector) Needs() pipe.Needs { return pipe.Needs{Veto: true} }

func (b bisector) Before(e pipe.Event, _ func(io.Writer)) bool {
	if e.Seq == 0 {
		return true // a required step
	}
	run := e.Seq <= *b.d.OptBisectLimit
	if b.d.Output != nil {
		verdict := "skip"
		if run {
			verdict = "run"
		}
		l := qualified(e.Label.Backend, pass.Label{Table: b.table, Func: e.Label.Func}.String())
		fmt.Fprintf(b.d.Output, "opt-bisect: %d %s %s [%s]\n", e.Seq, verdict, e.Step.Instance, l)
	}
	return run
}

func (bisector) After(pipe.Event, *pipe.Result) {}

// watch holds what the observers of a driver collect over the stages it runs: the statistics and
// the times for Report, and for PrintChanged the input of the running step and the units already
// dumped.
type watch struct {
	stats   *pipe.Stats
	times   []*timing
	byKey   map[timingKey]*timing
	total   time.Duration // the time of the runs of the manager
	input   func(io.Writer)
	printed map[string]bool // by label and kind
}

type timingKey struct{ backend, instance string }

// timing is the time of one step instance, summed over the units it ran on.
type timing struct {
	timingKey
	elapsed time.Duration
}

// timer is the observer of TimePasses: it sums the time of each step instance that ran, required
// steps included, in the order in which each first ran. A replayed result took no time.
type timer watch

func (*timer) Needs() pipe.Needs                       { return pipe.Needs{} }
func (*timer) Before(pipe.Event, func(io.Writer)) bool { return true }

func (t *timer) After(e pipe.Event, r *pipe.Result) {
	if e.Step.Pattern || !r.Ran {
		return
	}
	k := timingKey{e.Label.Backend, e.Step.Instance}
	tm := t.byKey[k]
	if tm == nil {
		if t.byKey == nil {
			t.byKey = map[timingKey]*timing{}
		}
		tm = &timing{timingKey: k}
		t.byKey[k] = tm
		t.times = append(t.times, tm)
	}
	tm.elapsed += r.Elapsed
}

// Report writes to Output the reports that the driver collected over the stages it ran: with Stats a
// "-- stats --" block, one line per pass instance that ran, in the order in which each first ran,
// with its runs, its changes, the counters it declares and the decisions of its patterns in the order
// of their declaration; with TimePasses a "-- time-passes --" block, one line per step instance that
// ran with its time in milliseconds and its share of the total, a manager line with the time of the
// runs of the manager outside the steps (verification, copies, dumps and the other observers), and a
// total line with the time of the runs of the manager. A step outside a branch of a Fork has no
// backend in its line.
func (d *Driver) Report() {
	if d.Output == nil || d.watch == nil {
		return
	}
	if d.Stats && d.watch.stats != nil {
		fmt.Fprintln(d.Output, "-- stats --")
		tw := tabwriter.NewWriter(d.Output, 0, 0, 2, ' ', 0)
		for _, st := range d.watch.stats.Instances() {
			fmt.Fprintf(tw, "  %s\truns %d\tchanged %d", qualified(st.Backend, st.Instance), st.Runs, st.Changed)
			for _, c := range st.Counters {
				fmt.Fprintf(tw, "\t%s %d", c.Name, c.Value)
			}
			for _, p := range st.Patterns {
				fmt.Fprintf(tw, "\t%s applied %d skipped %d", p.Pattern, p.Applied, p.Skipped)
			}
			fmt.Fprintln(tw)
		}
		tw.Flush()
	}
	if d.TimePasses {
		fmt.Fprintln(d.Output, "-- time-passes --")
		tw := tabwriter.NewWriter(d.Output, 0, 0, 2, ' ', 0)
		total, steps := d.watch.total, time.Duration(0)
		line := func(name string, el time.Duration) {
			share := 0.0
			if total > 0 {
				share = 100 * float64(el) / float64(total)
			}
			fmt.Fprintf(tw, "  %s\t%10.3fms\t%5.1f%%\n", name, float64(el)/float64(time.Millisecond), share)
		}
		for _, tm := range d.watch.times {
			line(qualified(tm.backend, tm.instance), tm.elapsed)
			steps += tm.elapsed
		}
		line("manager", max(total-steps, 0))
		line("total", total)
		tw.Flush()
	}
}

// qualified returns the name of an instance or a unit in a dump or a report: with its backend before
// a slash, if any.
func qualified(backend, instance string) string {
	if backend == "" {
		return instance
	}
	return backend + "/" + instance
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

// LowerTable runs the pipeline of the planned table p, the table named table, from its lowering to
// the Exec stage, in one run of the manager:
//
//	flowlower!
//	within module@flow {
//	  func funcs@flow { Flow passes }
//	  flow-finish!
//	}
//	execlower!
//	within module@exec {
//	  func funcs@exec { Exec passes }
//	  func lowered-funcs@exec { target }
//	}
//
// The lowering flowlower lowers the functions of the plan, with its decisions (plandata.Planned.Refuse),
// to the table at the Flow stage, which its postcondition verifies. The Flow passes run with the
// optimizations of the driver on each function of the module, pass by pass (pipe.PerFunc), and
// flow-finish ends each function, also after a failed pass. observe says that the parser reports its
// rules and skipped alternatives, and flowTarget what the backend prints (flowpass.ObserveFeature,
// flowpass.TargetFeature). The conversion execlower converts the module to the Exec stage, which its
// postcondition verifies, and the Exec passes that every target runs and then target, the pipeline of
// the backend's own Exec passes, run on each function. The module is dumped as "flowlower" as the
// lowering made it, as "flow-finish" as it leaves the Flow stage and as "execlower" as the conversion
// made it, and each function after each pass, labeled with its table.
//
// The passes of target run on each function as a lowered function (execlower.LoweredFuncs), which
// keeps what the printers print: the function in its module with its variables
// (execlower.Lowered.Keeps), which the verifier of the lowered function (execlower.LoweredIR) checks
// after the passes of target. Every pass keeps the functions of the plan in the module, which the
// lenses of the module check (plandata.Table.With).
//
// The plan and the Core module of p stay the caller's: flowlower only reads them, and the module it
// makes shares the Core nodes and rules with the Core module, which the passes and flow-finish read and
// never change (flow.Forms). So the run consumes only the module that the lowering made, and copies
// nothing for the Core module.
//
// A failure of flowlower or execlower is returned as the *pass.StepError that names it, with no table.
// A failure of a pass or of a verification is an internal error that names it, returned with the
// table, in which the function that it failed on is as it was before the passes of its segment, for a
// generation that fails with it; the optional passes after it do not run.
func (d *Driver) LowerTable(table string, p plandata.Planned, observe bool, flowTarget flowpass.Target, target TargetPipeline) (*plandata.Table[code.Exec], error) {
	s := pipe.Then3(pipe.Of(flowlower.Lowering), d.flowPasses(table, observe, flowTarget), d.execPasses(table, observe, target))
	t, err := execute(d, d.pipeRun(table, false), s, p)
	if err != nil {
		return t, pipeError(err, flowlower.Name, execlower.Name)
	}
	return t, nil
}

// LowerFlowTable runs the pipeline of the planned table p, the table named table, from its lowering
// to the end of the Flow stage, for a backend that runs the functions of the table at the Flow stage:
// the steps of LowerTable before execlower, with the same dumps and failures.
func (d *Driver) LowerFlowTable(table string, p plandata.Planned, observe bool, target flowpass.Target) (*plandata.Table[code.Flow], error) {
	s := pipe.Then(pipe.Of(flowlower.Lowering), d.flowPasses(table, observe, target))
	t, err := execute(d, d.pipeRun(table, false), s, p)
	if err != nil {
		return t, pipeError(err, flowlower.Name)
	}
	return t, nil
}

// flowPasses returns the Flow stage of the pipeline of a table after its lowering: the Flow passes of
// the table named table (FlowPipeline, if set) and flow-finish, on the module of the table.
func (d *Driver) flowPasses(table string, observe bool, target flowpass.Target) pipe.Pipeline[*plandata.Table[code.Flow], *plandata.Table[code.Flow]] {
	p := flowpass.Pipeline()
	if d.FlowPipeline != nil {
		p = d.FlowPipeline(table)
	}
	return pipe.Within(plandata.FlowModule, pipe.Then(
		pipe.WithConst(flowpass.ObserveFeature, observe, pipe.WithConst(flowpass.TargetFeature, target, p)),
		pipe.Of(flowpass.Finishing)))
}

// execPasses returns the Exec stage of the pipeline of a table: execlower and then, on the module of
// the table, the Exec passes of the table named table (ExecPipeline, if set) and target.
func (d *Driver) execPasses(table string, observe bool, target TargetPipeline) pipe.Pipeline[*plandata.Table[code.Flow], *plandata.Table[code.Exec]] {
	p := codepass.Pipeline()
	if d.ExecPipeline != nil {
		p = d.ExecPipeline(table)
	}
	return pipe.Then(pipe.Of(execlower.Lowering), pipe.Within(plandata.ExecModule,
		pipe.WithConst(flowpass.ObserveFeature, observe, pipe.Then(p, pipe.PerFunc(execlower.LoweredFuncs, target)))))
}

// Flow runs the Flow stage of LowerFlowTable on the functions of u, which no plan holds, and returns
// the module of the table: the functions that the TypeScript printer lowers one at a time, until their
// routes are planned (record 074, migration step 5). The functions of u are planned in its order,
// each keyed by its index (plandata.Key.Line), and u.Refuse is the decision of the plan.
func (d *Driver) Flow(table string, u lowerdata.Unit, observe bool, target flowpass.Target) (*code.Module, error) {
	plan := &plandata.Plan{}
	for i, f := range u.Funcs {
		s := &plandata.FuncSpec{Key: plandata.Key{Line: i}, Func: f}
		if f.Fallback != nil {
			s.Fallback = &plandata.FuncSpec{Key: s.Key, Func: *f.Fallback}
		}
		plan.Add(s)
	}
	p := plandata.Planned{Plan: plan, IR: &plandata.TableIR{M: u.Module, Opts: u.Opts}}
	if u.Refuse != nil {
		p.Refuse = func(k plandata.Key, f *code.Func) bool { return u.Refuse(k.Line, f) }
	}
	t, err := d.LowerFlowTable(table, p, observe, target)
	if t == nil {
		return nil, err
	}
	return t.Module.Module(), err
}

// Exec runs the Exec stage of LowerTable on the module m of the functions of the table named table,
// which Flow returned, and returns the module at the Exec stage. m is consumed: execlower moves its
// functions into the result.
func (d *Driver) Exec(table string, m *code.Module, target TargetPipeline, observe bool) (*code.Module, error) {
	// The module is at the Flow stage, as Flow made it, and no plan holds its functions.
	t, err := plandata.NewTable(nil, nil, code.At[code.Flow]{}.With(m))
	if err != nil {
		return nil, err
	}
	x, err := execute(d, d.pipeRun(table, false), d.execPasses(table, observe, target), t)
	if err != nil {
		err = pipeError(err, execlower.Name)
	}
	if x == nil {
		return nil, err
	}
	return x.Module.Module(), err
}

// TargetPipeline is the pipeline of the Exec passes of one target (golang/pass, typescript/pass), which
// run on a lowered function after the Exec passes that every target runs (Exec).
type TargetPipeline = pipe.Pipeline[*execlower.Lowered, *execlower.Lowered]

// targets holds the Exec passes of the targets, which -print-after names (PrintNames).
var targets = [][]*pipe.Pass[*execlower.Lowered, *execlower.Lowered]{gopass.Passes, tspass.Passes}
