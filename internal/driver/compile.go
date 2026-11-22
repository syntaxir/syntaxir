package driver

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	gobackend "syntaxir.dev/sxir/internal/backend/codegen/golang"
	goprint "syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	tsbackend "syntaxir.dev/sxir/internal/backend/codegen/typescript"
	tsprint "syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Backend is the backend of a target: Go, TypeScript, VM or Closure.
type Backend = result.Backend

// Output is the result of one target (result.Output).
type Output = result.Output

// Target is one backend of a compilation with its options. Gen holds the options of Go and
// TypeScript. Its fields that Program holds (the start rule, source information, default values,
// left-recursion bindings) and that the call holds (optimization, debugging settings) must be zero or
// equal to their values; otherwise Compile fails before any step, naming the target and the field.
type Target struct {
	Backend Backend
	Gen     common.GenOptions
	// GoHooks and TSHooks are the hooks of a test in the printer of a Go or a TypeScript target
	// (goprint.Hooks, tsprint.Hooks), which observe or change the decisions of its printing. They are
	// for tests: production leaves them nil, and no package other than a test sets them. A target
	// sets at most the hooks of its own backend.
	GoHooks *goprint.Hooks
	TSHooks *tsprint.Hooks
}

// Options are the settings of a compilation, common to every target.
type Options struct {
	Settings pass.Settings
	// Debug holds the debugging settings, which apply to the whole call: the dumps, the reports and
	// the bisection limit, written to its Output, and the verification of every step.
	Debug common.Debug
	// Edits are typed edits of the assembled pipeline (internal: tests), such as a fill of one of
	// its slots (CoreSlot, FlowSlot, ExecSlot, TargetSlot) with passes of their own.
	Edits []pipe.Edit
	// Observers watch every step of the run, after the dumps of the targets: each one sees the
	// event of every step (its instance and the backend, table and function it ran on) and, as its
	// Needs ask, the unit after it, such as the Core module of a table, the plan of the program or
	// a table at the Exec stage. It is the one way for a test to read the units of a compilation;
	// production callers leave it nil.
	Observers []pipe.Observer
	// Pipeline, if not empty, is a pipeline text (internal: tests and the development tool) whose
	// body replaces the assembled pipeline. pipe.Resolve holds it to the skeleton of the assembled
	// one, so it can reorder, repeat, insert or remove optional passes and set their parameters only,
	// and it takes the target and the reach of the plan from the assembled one. Its headers, which
	// give the options of the build, are read by the reader of the text (generate.Read); Compile
	// does not read them. A call sets Pipeline or Edits, not both.
	Pipeline string
}

// The slots of the assembled pipelines, for Edits: the Core passes of a table, the Flow and the Exec
// passes that run on each function of a table, and the Exec passes of the target that run on each
// lowered function.
var (
	CoreSlot   = pipe.NewSlot("core-passes", corepass.IR)
	FlowSlot   = pipe.NewSlot("flow-passes", code.FlowStage)
	ExecSlot   = pipe.NewSlot("exec-passes", code.ExecStage)
	TargetSlot = pipe.NewSlot("target-passes", execdata.LoweredIR)
)

// Compile runs one pipeline for the targets under one manager. On an error it returns no output and
// the errors of every failed target, joined in target order. Until the pipelines of several targets
// are forked (record 074, migration step 9), a call compiles exactly one target. A nil program
// compiles the TypeScript library alone (common.LibOnly), the one target that has no program.
func Compile(ctx context.Context, p *Program, o Options, targets ...Target) ([]Output, error) {
	if len(targets) != 1 {
		return nil, fmt.Errorf("a compilation takes one target, not %d", len(targets))
	}
	t := targets[0]
	if p == nil {
		return compileLibrary(ctx, o, t)
	}
	c, err := newCompilation(p, o, t)
	if err != nil {
		return nil, err
	}
	a, err := c.pipeline()
	if err != nil {
		return nil, err
	}
	d := newDebugger(o)
	out, err := c.run(ctx, d, a)
	if r, ok := out.(*common.Replan); ok && err == nil {
		// The fallback of planning: the printed code enters other rules than the plan reaches, so
		// the compilation runs again with the reach of the printed code, and what the observers of
		// the debugging settings collected of the first run is dropped.
		d.restart()
		if a.pipeline, err = pipe.Apply(a.pipeline, pipe.SetConst(plandata.ReachFeature, r.Reach)); err != nil {
			return nil, fmt.Errorf("internal error: %w", err)
		}
		if out, err = c.run(ctx, d, a); err == nil {
			if _, again := out.(*common.Replan); again {
				err = fmt.Errorf("internal error: the %s printer asks again for another reach than the one it printed", t.Backend)
			}
		}
	}
	// The dumps are written also on an error, whose dumps show what led to it; the reports only
	// after a build.
	if ferr := d.flush(); ferr != nil && err == nil {
		err = ferr
	}
	if err != nil {
		return nil, err
	}
	d.report()
	return []Output{out}, nil
}

// compileLibrary compiles the target t without a program: the TypeScript library alone
// (common.LibOnly), whose pipeline starts at the library (tsLibraryPipeline).
func compileLibrary(ctx context.Context, o Options, t Target) ([]Output, error) {
	a, pl, err := libraryCompilation(o, t)
	if err != nil {
		return nil, err
	}
	src, err := tsbackend.LibrarySource()
	if err != nil {
		return nil, err
	}
	d := newDebugger(o)
	out, err := execute(ctx, d, &pipe.Run{Settings: o.Settings, VerifyEach: o.Debug.VerifyEach, Observers: d.observers()}, pl, src)
	if err != nil {
		err = a.error(err)
	}
	if ferr := d.flush(); ferr != nil && err == nil {
		err = ferr
	}
	if err != nil {
		return nil, err
	}
	d.report()
	return []Output{out}, nil
}

// Describe returns the pipeline that Compile runs for the target of p under o, as text without
// headers, which the reader of the build writes (generate.Describe): the assembled pipeline with the
// edits of o, or the pipeline of the text of o. A nil program describes the TypeScript library
// alone. Until the pipelines of several targets are forked (record 074, migration step 9), it takes
// exactly one target. The pipeline is that of the first run: the reach of the plan is none, and
// the fallback of planning, which a run of the text repeats, gives the second run its reach.
func Describe(p *Program, o Options, targets ...Target) (*pipe.Description, error) {
	if len(targets) != 1 {
		return nil, fmt.Errorf("a compilation takes one target, not %d", len(targets))
	}
	t := targets[0]
	if p == nil {
		_, pl, err := libraryCompilation(o, t)
		if err != nil {
			return nil, err
		}
		return pipe.Describe(pl), nil
	}
	c, err := newCompilation(p, o, t)
	if err != nil {
		return nil, err
	}
	a, err := c.pipeline()
	if err != nil {
		return nil, err
	}
	return pipe.Describe(a.pipeline), nil
}

// newCompilation checks the target t of p under o and returns its compilation, with the options of
// the target that the program and the call hold set to theirs.
func newCompilation(p *Program, o Options, t Target) (*compilation, error) {
	if err := checkTarget(p, o, t); err != nil {
		return nil, err
	}
	if err := CheckDebug(o.Debug); err != nil {
		return nil, err
	}
	po := p.Options()
	t.Gen.SourceInfo, t.Gen.DefaultValue, t.Gen.LeftRecursionBindings = po.SourceInfo, po.DefaultValue, po.LeftRecursionBindings
	t.Gen.Debug = o.Debug
	return &compilation{p: p, t: t, o: o}, nil
}

// libraryCompilation checks the target t of a compilation without a program under o, which compiles
// only the TypeScript library alone (common.LibOnly), and returns its pipeline as assembled and as
// the call runs it.
func libraryCompilation(o Options, t Target) (libraryPipeline, pipe.Pipeline[*library.Library, Output], error) {
	var none pipe.Pipeline[*library.Library, Output]
	if t.Backend != result.TypeScript || t.Gen.Lib != common.LibOnly {
		return libraryPipeline{}, none, fmt.Errorf("target %s: a compilation without a program compiles only the TypeScript library alone (LibOnly)", t.Backend)
	}
	if t.GoHooks != nil || t.TSHooks != nil {
		return libraryPipeline{}, none, fmt.Errorf("target %s: the library alone has no printer hooks", t.Backend)
	}
	if err := CheckDebug(o.Debug); err != nil {
		return libraryPipeline{}, none, err
	}
	t.Gen.Debug = o.Debug
	a := tsLibraryPipeline(t)
	pl, err := callPipeline(a.pipeline, o)
	return a, pl, err
}

// pipeline decides the target of the compilation c and returns the pipeline that its first run
// runs: the assembled one with the edits of the call, or the one of its pipeline text.
func (c *compilation) pipeline() (assembled, error) {
	if err := c.decide(); err != nil {
		return assembled{}, err
	}
	a, err := c.assemble()
	if err != nil {
		return assembled{}, err
	}
	if a.pipeline, err = callPipeline(a.pipeline, c.o); err != nil {
		return assembled{}, err
	}
	return a, nil
}

// callPipeline returns the pipeline that the call o runs in place of the assembled pipeline ref: ref
// with the edits of o, or the pipeline of the text of o, which must take and return what ref does
// and have its skeleton (pipe.Resolve).
func callPipeline[I, O any](ref pipe.Pipeline[I, O], o Options) (pipe.Pipeline[I, O], error) {
	if o.Pipeline == "" {
		return pipe.Apply(ref, o.Edits...)
	}
	if len(o.Edits) > 0 {
		return ref, fmt.Errorf("a compilation takes a pipeline text or edits of the assembled pipeline, not both")
	}
	d, err := pipe.Parse(o.Pipeline)
	if err != nil {
		return ref, err
	}
	return pipe.Resolve(d, ref)
}

// run executes the pipeline a of the compilation c once, with the observers of the debugging
// settings and of the call that d runs.
func (c *compilation) run(ctx context.Context, d *debugger, a assembled) (Output, error) {
	out, err := execute(ctx, d, &pipe.Run{Settings: c.o.Settings, VerifyEach: c.o.Debug.VerifyEach, Observers: d.observers()}, a.pipeline, c)
	if err != nil {
		return nil, a.error(err)
	}
	return out, nil
}

// newDebugger returns the debugger of the call o: its observers write what the debugging settings
// select, with the dumps of each table kept together (debugger.out), and run the observers of the
// call.
func newDebugger(o Options) *debugger {
	g := o.Debug
	return &debugger{PrintAfter: g.PrintAfter, PrintBefore: g.PrintBefore, PrintChanged: g.PrintChanged,
		Stats: g.Stats, TimePasses: g.TimePasses, OptBisectLimit: g.OptBisectLimit,
		Output: g.Output, call: o.Observers, groups: &groups{}}
}

// assembled is the pipeline of one target and the steps whose failure is an error of the compilation
// itself, such as a grammar that the target cannot generate a parser for.
type assembled struct {
	pipeline pipe.Pipeline[*compilation, Output]
	// steps are the instances of those steps: required steps such as elaborate or the printer, and
	// splits ("each <name>") whose parts are decided from the program.
	steps []string
}

// decide checks the options of the target of the compilation c that do not depend on the grammar,
// completing them, and sets the target as its printer reads it (codegen.TargetFeature).
func (c *compilation) decide() error {
	p := c.p
	switch c.t.Backend {
	case result.Go:
		opts, err := goprint.Validate(c.t.Gen)
		if err != nil {
			return err
		}
		c.t.Gen = opts
		c.target = gobackend.NewTarget(p.checked, p.recognizer, p.Start(), opts, c.t.GoHooks)
		return nil
	case result.TypeScript:
		opts, err := tsprint.Validate(c.t.Gen)
		if err != nil {
			return err
		}
		c.t.Gen = opts
		c.target = tsbackend.NewTarget(p.checked, p.recognizer, p.Start(), opts, c.t.TSHooks)
		return nil
	}
	return fmt.Errorf("internal error: no pipeline is assembled for the backend %s", c.t.Backend)
}

// assemble returns the pipeline of the target of the compilation c, from the program to its output,
// with the target as its printer reads it (codegen.TargetFeature) and no reach of the plan
// (plandata.ReachFeature), which the fallback of planning sets for a second run.
func (c *compilation) assemble() (assembled, error) {
	var a assembled
	var err error
	switch c.t.Backend {
	case result.Go:
		a, err = goPipeline(c)
	case result.TypeScript:
		a, err = tsPipeline(c)
	default:
		err = fmt.Errorf("internal error: no pipeline is assembled for the backend %s", c.t.Backend)
	}
	if err != nil {
		return assembled{}, err
	}
	a.pipeline = pipe.WithConst(codegen.TargetFeature, c.target, pipe.WithConst(plandata.ReachFeature, map[plandata.Entry]bool(nil), a.pipeline))
	return a, nil
}

// error returns the failure err of a run of the pipeline: the failure of one of the steps a.steps as
// the *pass.StepError that names it, with the message of the step, and any other failure (a pass, a
// verification, a join, an invalid pipeline) as an internal error.
func (a assembled) error(err error) error {
	var se *pipe.StepError
	if errors.As(err, &se) && slices.Contains(a.steps, se.Step) {
		return &pass.StepError{Step: pass.Step{Name: se.Step, Required: true}, Err: se.Err}
	}
	return fmt.Errorf("internal error: %w", err)
}

// checkTarget reports a field of the options of t that Program or the call holds and that is neither
// zero nor equal to their value.
func checkTarget(p *Program, o Options, t Target) error {
	g := t.Gen
	po := p.Options()
	bad := func(field string) error {
		return fmt.Errorf("target %s: %s differs from the value of the compilation", t.Backend, field)
	}
	switch {
	case g.Start != "" && g.Start != p.Start().Name:
		return bad("Start")
	case g.SourceInfo && !po.SourceInfo:
		return bad("SourceInfo")
	case g.DefaultValue && !po.DefaultValue:
		return bad("DefaultValue")
	case g.LeftRecursionBindings && !po.LeftRecursionBindings:
		return bad("LeftRecursionBindings")
	case !reflect.DeepEqual(g.Debug, common.Debug{}) && !reflect.DeepEqual(g.Debug, o.Debug):
		return bad("Debug")
	case t.GoHooks != nil && t.Backend != result.Go:
		return fmt.Errorf("target %s: GoHooks are set for a target of another backend", t.Backend)
	case t.TSHooks != nil && t.Backend != result.TypeScript:
		return fmt.Errorf("target %s: TSHooks are set for a target of another backend", t.Backend)
	}
	if g.Optimize != "" || len(g.Opt) > 0 || len(g.NoOpt) > 0 {
		s, err := GenSettings(g)
		if err != nil {
			return err
		}
		if !s.Equal(o.Settings) {
			return bad("Optimize, Opt and NoOpt")
		}
	}
	return nil
}

// GenSettings returns the optimizations that o.Optimize, o.Opt and o.NoOpt select, in that order, or
// an error for a profile or an optimization that does not exist, a value that it does not take, or a
// name that Opt gives twice (keys that are the same once their spaces are trimmed).
func GenSettings(o common.GenOptions) (pass.Settings, error) {
	s, err := Profile(o.Optimize)
	if err != nil {
		return pass.Settings{}, err
	}
	given := map[string]string{}
	for _, n := range slices.Sorted(maps.Keys(o.Opt)) {
		name := strings.TrimSpace(n)
		if prev, dup := given[name]; dup {
			return pass.Settings{}, fmt.Errorf("optimization %s is given twice in Opt (%q and %q)", name, prev, n)
		}
		given[name] = n
		if s, err = s.With(name, o.Opt[n]); err != nil {
			return pass.Settings{}, err
		}
	}
	return s.Without(o.NoOpt...)
}
