package driver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"text/tabwriter"
	"time"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	gopass "syntaxir.dev/sxir/internal/backend/codegen/golang/pass"
	tspass "syntaxir.dev/sxir/internal/backend/codegen/typescript/pass"
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// A debugger writes what the debugging settings of a compilation select to Output and runs the
// observers of the call (Options.Observers): it makes the observers of the run of the pipeline
// (observers) and, after a build, writes the reports they collected (report).
//
// The debugging settings observe the run and never change what it makes. Everything they write goes to
// Output, the dumps of each table together (out, flush); the statistics and the times that the observers
// collect are written once, by report, after the build. Their text is a debugging aid, whose form and
// names may change between versions.
type debugger struct {
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
	// Stats collects the statistics of every pass instance (pipe.Stats) for report.
	Stats bool
	// TimePasses collects the wall time of every step instance for report.
	TimePasses bool
	// OptBisectLimit, if set, lets run only the runs of the optional passes and the applications of
	// their patterns numbered up to the limit (pipe.Event.Seq), over the run, and
	// vetoes the later ones; the required steps always run. Each decision is written to Output as
	// "opt-bisect: <number> run|skip <instance> [<unit>]".
	OptBisectLimit *int
	// Output receives the dumps and the reports; nil writes nothing, whatever the settings above.
	Output io.Writer
	// call holds the observers of the call (Options.Observers), which run after those of the
	// debugging settings.
	call []pipe.Observer
	// watch holds what the observers collect over the run; observers makes it on first use.
	watch *watch
	// groups, if not nil, keeps what the observers write of each table apart until flush writes it
	// (Compile).
	groups *groups
	// backend, if not empty, is the backend that labels the steps outside a branch of a Fork: that
	// of a compilation of one run-time target, whose dumps and reports name it as those of a branch
	// do, since the debugging settings of a call that builds a parser write the builds of its
	// backends (checked.Source).
	backend string
}

// groups holds what the observers of a compilation write of each table of each target: the dumps of
// one table are written together, those of the program first, then those of each table in the order
// of its first dump, so that a table's dumps follow it from its Core IR to the Exec stage although the
// stages of every table run before the next stage of any. The tables of the targets of a Fork are
// apart, keyed by their labels (qualified), and the program of each target comes before every table.
type groups struct {
	order  []groupKey // the tables in the order of their first write
	tables map[groupKey]*bytes.Buffer
}

// groupKey is a table of the target of a backend (the label of a Fork branch, empty for a single
// target), or programLabel for the steps of its whole program.
type groupKey struct{ backend, table string }

// out returns the writer of what the observers write of the table of the branch backend: Output, or
// the group of the table in a compilation.
func (d *debugger) out(backend, table string) io.Writer {
	g := d.groups
	if g == nil {
		return d.Output
	}
	k := groupKey{backend, table}
	b := g.tables[k]
	if b == nil {
		if g.tables == nil {
			g.tables = map[groupKey]*bytes.Buffer{}
		}
		b = &bytes.Buffer{}
		g.tables[k] = b
		g.order = append(g.order, k)
	}
	return b
}

// flush writes to Output what the observers wrote of each table, the programs' first, and forgets it.
func (d *debugger) flush() error {
	g := d.groups
	if g == nil || d.Output == nil {
		return nil
	}
	var err error
	for _, program := range []bool{true, false} {
		for _, k := range g.order {
			if (k.table == programLabel) == program {
				if _, werr := d.Output.Write(g.tables[k].Bytes()); werr != nil && err == nil {
					err = werr
				}
			}
		}
	}
	d.drop()
	return err
}

// drop forgets what the observers wrote of each table.
func (d *debugger) drop() {
	if d.groups != nil {
		*d.groups = groups{}
	}
}

// restart forgets what the observers collected of a run, its dumps, statistics and times, for a
// run that replaces it (Compile).
func (d *debugger) restart() {
	d.drop()
	d.watch = nil
}

// programLabel labels the dumps of the steps that run on the whole program.
const programLabel = "program"

// prints reports whether the unit is dumped after the runs of the step or pass named name: of every
// instance of it, as its dumps name them. The plan of the program (flowplan.PlanName) is dumped only
// when PrintAfter names it: it is not an IR that a pass changes, so PrintAll leaves it out.
func (d *debugger) prints(name string) bool {
	if d.Output == nil || name == "" {
		return false
	}
	return slices.Contains(d.PrintAfter, name) || name != flowplan.PlanName && slices.Contains(d.PrintAfter, PrintAll)
}

// printsBefore reports whether the unit is dumped before the runs of the pass named name, of every
// instance of it.
func (d *debugger) printsBefore(name string) bool {
	return d.Output != nil && (slices.Contains(d.PrintBefore, name) || slices.Contains(d.PrintBefore, PrintAll))
}

// Elaborate is the name of the step that builds the Core IR of a table from its elaborated program,
// the first step of the pipeline.
const Elaborate = "elaborate"

// tableIR is the kind table: the build of the Core IR of one table of a program, which elaborate runs
// (common.TableSource).
var tableIR = pipe.RegisterIR(&pipe.IR[common.TableSource]{Name: "table"})

// coreKey keys the Core part of the pipeline of a table on the key of its build, so that the tables
// whose builds make the same module share one run of it (pipe.Shared). The tables of one key agree on
// the origin map, which the compilation decides once (common.TableSource.Origin): one that does not
// would get a module built for another, which fails the compilation as an internal error.
var coreKey = pipe.RegisterKey(tableIR, &pipe.Key[common.TableSource]{Name: "core-config",
	Of: func(s common.TableSource) string { return s.Key },
	Agree: func(first, later common.TableSource) error {
		if first.Origin != later.Origin {
			return fmt.Errorf("the tables of the build %q disagree on the origin map", first.Key)
		}
		return nil
	}})

// elaborating is the step Elaborate: it runs the build of the Core IR module that it is given and
// computes the attributes of its nodes (core.Analyze), so that the module it returns is a Core module
// with its attributes, as the Core passes and the Core verifier read it. Its postcondition is the
// structural verifier (core.Verify), which reads the attributes: they are what core.Analyze computes
// by construction, so recomputing them to compare (core.CheckAttributes, the rest of the verifier of
// Core IR) could only find them equal. The checks where the Core passes end compare them.
var elaborating = pipe.New(pipe.Def[common.TableSource, *core.Module]{
	Decl: pipe.Decl{Name: Elaborate, Required: true, Obligation: "The module is the elaborated program in Core IR, with its attributes."},
	In:   tableIR, Out: corepass.IR,
	Run: func(c *pipe.Ctx[common.TableSource], src common.TableSource) (*core.Module, pass.PreservedSet, error) {
		// The build and the Core passes poll the control of the context of the compilation, which
		// the module keeps, and none when the context cannot end.
		var control *cancel.Control
		if ctx := c.Context(); ctx.Done() != nil {
			control = &cancel.Control{Ctx: ctx}
		}
		m, err := src.Build(control, src.Origin)
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		core.Analyze(m)
		return m, pass.PreserveNone(), nil
	},
	Post: core.Verify,
})

// observers returns the observers of the debugging settings and then those of the call, which watch
// every step of the run, labeled as their events are.
func (d *debugger) observers() []pipe.Observer {
	if d.watch == nil {
		d.watch = &watch{}
	}
	var obs []pipe.Observer
	if o := (pipeObserver{d: d}); o.Needs() != (pipe.Needs{}) {
		obs = append(obs, o)
	}
	if d.Output != nil && d.Stats {
		if d.watch.stats == nil {
			d.watch.stats = pipe.NewStats()
		}
		obs = append(obs, d.watch.stats)
	}
	if d.Output != nil && d.TimePasses {
		obs = append(obs, (*timer)(d.watch))
	}
	if d.OptBisectLimit != nil {
		obs = append(obs, bisector{d: d})
	}
	return append(obs, d.call...)
}

// execute runs the pipeline p on in with r (pipe.Execute) and adds the time it took to the total of
// the times that report writes. The failure of a step that found the context ended before it ran
// is the cancellation of the step (cancelled).
func execute[I, O any](ctx context.Context, d *debugger, r *pipe.Run, p pipe.Pipeline[I, O], in I) (O, error) {
	w := &watchedContext{Context: ctx}
	if d.Output == nil || !d.TimePasses {
		out, err := pipe.Execute(w, r, p, in)
		return out, w.cancelled(err)
	}
	start := time.Now()
	out, err := pipe.Execute(w, r, p, in)
	d.watch.total += time.Since(start)
	return out, w.cancelled(err)
}

// watchedContext is the context of a run that keeps the first error its Err returned, the error
// that the pass manager fails a step with when it finds the context ended before the step ran.
type watchedContext struct {
	context.Context
	mu  sync.Mutex
	err error
}

func (c *watchedContext) Err() error {
	err := c.Context.Err()
	if err != nil {
		c.mu.Lock()
		if c.err == nil {
			c.err = err
		}
		c.mu.Unlock()
	}
	return err
}

// cancelled marks the failure err of the run as a cancellation (cancel.Cancellation) when it is the
// failure of a step with the error that the context returned, so that a caller tells it apart from a
// failure of the work, as it does the cancellation panic of a step (recoverCancellation). It reads
// the error the run saw and does not ask the context again, which a caller's context may answer
// otherwise, or count.
func (c *watchedContext) cancelled(err error) error {
	var se *pipe.StepError
	if err == nil || !errors.As(err, &se) || errors.As(se.Err, new(cancel.Cancellation)) {
		return err
	}
	c.mu.Lock()
	seen := c.err
	c.mu.Unlock()
	if seen != nil && errors.Is(se.Err, seen) {
		se.Err = cancel.Cancellation{Err: se.Err}
	}
	return err
}

// pipeObserver writes the dumps of PrintBefore, PrintAfter and PrintChanged for the steps of the run.
// The label of each dump names the table of its unit, if any; the label of a step in a branch of a
// Fork names its backend first, as the reports do, since the debugging settings and their writer are
// one for every target of a compilation.
type pipeObserver struct {
	d *debugger
}

func (o pipeObserver) Needs() pipe.Needs {
	changed := o.d.Output != nil && o.d.PrintChanged
	return pipe.Needs{
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
	table := tableOf(e)
	l := o.d.qualified(e.Label.Backend, pass.Label{Table: table, Func: e.Label.Func}.String())
	w := o.d.out(e.Label.Backend, table)
	before := !e.Step.Required && o.d.printsBefore(e.Step.Name) && o.d.watch.input != nil
	if before {
		// Also before a run that failed, whose input is what to look at.
		fmt.Fprintf(w, "-- before %s [%s] --\n", e.Step.Instance, l)
		o.d.watch.input(w)
	}
	if r.Err != nil {
		return
	}
	if r.Print == nil {
		return
	}
	out := w
	changed := o.d.PrintChanged && e.Step.In == e.Step.Out && r.TextChanged
	if changed {
		w := o.d.watch
		unit := l + "@" + e.Step.In
		if w.printed == nil {
			w.printed = map[string]bool{}
		}
		if !w.printed[unit] && w.input != nil && !before {
			fmt.Fprintf(out, "-- before %s [%s] --\n", e.Step.Instance, l)
			w.input(out)
		}
		w.printed[unit] = true
	}
	if changed || o.d.prints(e.Step.Name) {
		fmt.Fprintf(out, "-- after %s [%s] --\n", e.Step.Instance, l)
		r.Print(out)
	}
}

// tableOf returns the table of the unit of the event e, or programLabel for a step of the whole
// program.
func tableOf(e pipe.Event) string {
	if e.Label.Table != "" {
		return e.Label.Table
	}
	return programLabel
}

// bisector is the observer of OptBisectLimit: it vetoes the runs of optional passes and the
// applications of patterns numbered after the limit, and writes each decision to Output.
type bisector struct {
	d *debugger
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
		table := tableOf(e)
		l := b.d.qualified(e.Label.Backend, pass.Label{Table: table, Func: e.Label.Func}.String())
		fmt.Fprintf(b.d.out(e.Label.Backend, table), "opt-bisect: %d %s %s [%s]\n", e.Seq, verdict, e.Step.Instance, l)
	}
	return run
}

func (bisector) After(pipe.Event, *pipe.Result) {}

// watch holds what the observers of a driver collect over the stages it runs: the statistics and
// the times for report, and for PrintChanged the input of the running step and the units already
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

// report writes to Output the reports that the observers collected over the run: with Stats a
// "-- stats --" block, one line per pass instance that ran, in the order in which each first ran,
// with its runs, its changes, the counters it declares and the decisions of its patterns in the order
// of their declaration; with TimePasses a "-- time-passes --" block, one line per step instance that
// ran with its time in milliseconds and its share of the total, a manager line with the time of the
// runs of the manager outside the steps (verification, copies, dumps and the other observers), and a
// total line with the time of the runs of the manager. A step outside a branch of a Fork has no
// backend in its line, but in a compilation of one run-time target (debugger.backend).
func (d *debugger) report() {
	if d.Output == nil || d.watch == nil {
		return
	}
	if d.Stats && d.watch.stats != nil {
		fmt.Fprintln(d.Output, "-- stats --")
		tw := tabwriter.NewWriter(d.Output, 0, 0, 2, ' ', 0)
		for _, st := range d.watch.stats.Instances() {
			fmt.Fprintf(tw, "  %s\truns %d\tchanged %d", d.qualified(st.Backend, st.Instance), st.Runs, st.Changed)
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
			line(d.qualified(tm.backend, tm.instance), tm.elapsed)
			steps += tm.elapsed
		}
		line("manager", max(total-steps, 0))
		line("total", total)
		tw.Flush()
	}
}

// qualified returns the name of an instance or a unit in a dump or a report: with its backend before
// a slash, if any, which is the backend of the debugger outside a branch of a Fork.
func (d *debugger) qualified(backend, instance string) string {
	if backend == "" {
		backend = d.backend
	}
	return qualified(backend, instance)
}

// qualified returns the name of an instance or a unit: with its backend before a slash, if any.
func qualified(backend, instance string) string {
	if backend == "" {
		return instance
	}
	return backend + "/" + instance
}

// targetPipeline is the pipeline of the Exec passes of one target (golang/pass, typescript/pass), which
// run on a lowered function after the Exec passes that every target runs (lowering).
type targetPipeline = pipe.Pipeline[*execdata.Lowered, *execdata.Lowered]

// targets holds the Exec passes of the targets, which -print-after names (PrintNames).
var targets = [][]*pipe.Pass[*execdata.Lowered, *execdata.Lowered]{gopass.Passes, tspass.Passes}
