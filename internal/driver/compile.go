package driver

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	gobackend "syntaxir.dev/sxir/internal/backend/codegen/golang"
	goprint "syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	tsbackend "syntaxir.dev/sxir/internal/backend/codegen/typescript"
	tsprint "syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/cancel"
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

// Compile runs one pipeline for the targets under one manager. A single target runs its own
// pipeline; several run as the branches of one Fork, in order, labeled with their backends (go, ts;
// a second target of one backend is go#2), and share what their sharing keys allow, such as the
// Core IR of a table. On an error it returns no output and the errors of every failed target,
// joined in target order. A nil program compiles the TypeScript library alone (common.LibOnly), the
// one target that has no program, with no other target.
func Compile(ctx context.Context, p *Program, o Options, targets ...Target) ([]Output, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("a compilation takes at least one target")
	}
	if p == nil {
		if len(targets) != 1 {
			return nil, fmt.Errorf("the TypeScript library alone is compiled alone, not with %d other targets", len(targets)-1)
		}
		return compileLibrary(ctx, o, targets[0])
	}
	c, err := newCompilation(p, o, targets)
	if err != nil {
		return nil, err
	}
	a, err := c.pipeline()
	if err != nil {
		return nil, err
	}
	d := newDebugger(o)
	outs, err := c.run(ctx, d, a)
	if err == nil {
		// The fallback of planning: the printed code of a target enters other rules than the plan
		// reaches, so the compilation runs again with the reach of the printed code in the branches of
		// those targets, and what the observers of the debugging settings collected of the first run
		// is dropped. The other targets run again too; their Core IR is shared with the targets that
		// run with another reach, if their keys allow.
		var edits []pipe.Edit
		for i, out := range outs {
			if r, ok := out.(*common.Replan); ok {
				edits = append(edits, a.reach(i, r.Reach))
			}
		}
		if len(edits) > 0 {
			d.restart()
			if a, err = a.apply(edits...); err != nil {
				return nil, fmt.Errorf("internal error: %w", err)
			}
			if outs, err = c.run(ctx, d, a); err == nil {
				for i, out := range outs {
					if _, again := out.(*common.Replan); again {
						err = fmt.Errorf("internal error: the %s printer asks again for another reach than the one it printed", c.targets[i].t.Backend)
					}
				}
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
	return outs, nil
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
	out, err := execute(ctx, d, &pipe.Run{Settings: o.Settings, VerifyEach: o.Debug.VerifyEach, Observers: d.observers(), Recover: recoverCancellation}, pl, src)
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

// Describe returns the pipeline that Compile runs for the targets of p under o, as text without
// headers, which the reader of the build writes (generate.Describe): the assembled pipeline with the
// edits of o, or the pipeline of the text of o. A nil program describes the TypeScript library
// alone. The pipeline is that of the first run: the reach of the plan is none, and the fallback of
// planning, which a run of the text repeats, gives the second run its reach.
func Describe(p *Program, o Options, targets ...Target) (*pipe.Description, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("a compilation takes at least one target")
	}
	if p == nil {
		if len(targets) != 1 {
			return nil, fmt.Errorf("the TypeScript library alone is compiled alone, not with %d other targets", len(targets)-1)
		}
		_, pl, err := libraryCompilation(o, targets[0])
		if err != nil {
			return nil, err
		}
		return pipe.Describe(pl), nil
	}
	c, err := newCompilation(p, o, targets)
	if err != nil {
		return nil, err
	}
	a, err := c.pipeline()
	if err != nil {
		return nil, err
	}
	return a.describe(), nil
}

// compilation is a compilation of the program p for its targets under the options o of the call.
type compilation struct {
	p       *Program
	o       Options
	targets []target
}

// target is one target of a compilation: the target with its options completed from the program
// and the call, the target as its printer reads it (codegen.TargetFeature), which the assembled
// pipeline provides, and the name of its branch when the compilation has several targets.
type target struct {
	t      Target
	target common.Target
	branch string
}

// consuming reports whether every target of c is a run-time backend (the VM or the closures), whose
// compilation stops at the first failure: a generation goes on after a failing optional pass, which
// it rolls back, and reports the first error after its required steps have run.
func (c *compilation) consuming() bool {
	for _, t := range c.targets {
		if t.t.Backend != result.VM && t.t.Backend != result.Closure {
			return false
		}
	}
	return true
}

// newCompilation checks the targets of p under o and returns their compilation, with the options of
// each target that the program and the call hold set to theirs and the target decided.
func newCompilation(p *Program, o Options, targets []Target) (*compilation, error) {
	for _, t := range targets {
		if err := checkTarget(p, o, t); err != nil {
			return nil, err
		}
	}
	if err := CheckDebug(o.Debug); err != nil {
		return nil, err
	}
	c := &compilation{p: p, o: o}
	seen := map[Backend]int{}
	po := p.Options()
	for _, t := range targets {
		t.Gen.SourceInfo, t.Gen.DefaultValue, t.Gen.LeftRecursionBindings = po.SourceInfo, po.DefaultValue, po.LeftRecursionBindings
		t.Gen.Debug = o.Debug
		seen[t.Backend]++
		branch := t.Backend.String()
		if n := seen[t.Backend]; n > 1 {
			branch += "#" + strconv.Itoa(n)
		}
		x, err := decide(p, t)
		if err != nil {
			if len(targets) > 1 {
				err = fmt.Errorf("target %s: %w", branch, err)
			}
			return nil, err
		}
		x.branch = branch
		c.targets = append(c.targets, x)
	}
	return c, nil
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

// pipeline returns the pipeline that the first run of the compilation c runs: the assembled one with
// the edits of the call, or the one of its pipeline text.
func (c *compilation) pipeline() (assembly, error) {
	a := c.assemble()
	var err error
	if len(c.targets) == 1 {
		a.one, err = callPipeline(a.one, c.o)
	} else {
		a.fork, err = callPipeline(a.fork, c.o)
	}
	return a, err
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
// settings and of the call that d runs, and returns the output of each target in order. A
// compilation of run-time targets only stops at the first failure (pipe.Run.Stop) and so consumes
// its units without a rollback copy (consuming): the build of a parser fails on any internal error.
func (c *compilation) run(ctx context.Context, d *debugger, a assembly) ([]Output, error) {
	r := &pipe.Run{Settings: c.o.Settings, VerifyEach: c.o.Debug.VerifyEach, Observers: d.observers(), Recover: recoverCancellation,
		Stop: c.consuming()}
	if !a.forked {
		out, err := execute(ctx, d, r, a.one, c.p)
		if err != nil {
			return nil, a.error(err)
		}
		return []Output{out}, nil
	}
	outs, err := execute(ctx, d, r, a.fork, c.p)
	if err != nil {
		return nil, a.forkError(err)
	}
	return outs, nil
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
	pipeline pipe.Pipeline[*Program, Output]
	// steps are the instances of those steps: required steps such as elaborate or the printer, and
	// splits ("each <name>") whose parts are decided from the program.
	steps []string
}

// assembly is the pipeline of a compilation: that of its one target, or the Fork of the pipelines
// of its targets, one branch for each, and the steps whose failure is an error of the compilation
// itself.
type assembly struct {
	forked   bool
	one      pipe.Pipeline[*Program, Output]
	fork     pipe.Pipeline[*Program, []Output]
	branches []string
	steps    []string
}

// decide checks the options of the target t of p that do not depend on the grammar, completing
// them, and returns the target as its printer reads it (codegen.TargetFeature).
func decide(p *Program, t Target) (target, error) {
	if p.Start() == nil && (t.Backend == result.Go || t.Backend == result.TypeScript) {
		return target{}, fmt.Errorf("the %s program of a run-time build generates no parser: it has no start rule", p.Role())
	}
	switch t.Backend {
	case result.Go:
		opts, err := goprint.Validate(t.Gen)
		if err != nil {
			return target{}, err
		}
		t.Gen = opts
		return target{t: t, target: gobackend.NewTarget(p.checked(), p.recognizer, p.Start(), opts, t.GoHooks)}, nil
	case result.TypeScript:
		opts, err := tsprint.Validate(t.Gen)
		if err != nil {
			return target{}, err
		}
		t.Gen = opts
		return target{t: t, target: tsbackend.NewTarget(p.checked(), p.recognizer, p.Start(), opts, t.TSHooks)}, nil
	case result.VM:
		if err := runTimeGen(t); err != nil {
			return target{}, err
		}
		return target{t: t, target: &vmTarget{prog: p.prog}}, nil
	case result.Closure:
		if err := runTimeGen(t); err != nil {
			return target{}, err
		}
		return target{t: t, target: &closureTarget{prog: p.prog}}, nil
	}
	return target{}, fmt.Errorf("internal error: no pipeline is assembled for the backend %s", t.Backend)
}

// runTimeGen reports an option of the target t of a run-time backend, which generates no code, that
// only a generating backend reads: its options are those that Program and the call hold.
func runTimeGen(t Target) error {
	g := t.Gen
	g.SourceInfo, g.DefaultValue, g.LeftRecursionBindings, g.Debug = false, false, false, common.Debug{}
	g.Start, g.Optimize, g.Opt, g.NoOpt = "", "", nil, nil
	if !reflect.DeepEqual(g, common.GenOptions{}) {
		return fmt.Errorf("target %s: Gen sets an option of a generating backend", t.Backend)
	}
	return nil
}

// assemble returns the pipeline of the compilation c, from the program to the output of each
// target: the pipeline of each target with the target as its printer reads it
// (codegen.TargetFeature) and no reach of the plan (plandata.ReachFeature), which the fallback of
// planning sets for a second run; with several targets, each is a branch of one Fork.
func (c *compilation) assemble() assembly {
	var a assembly
	var branches []pipe.Branch[*Program, Output]
	for _, x := range c.targets {
		var t assembled
		switch x.t.Backend {
		case result.Go:
			t = goPipeline()
		case result.TypeScript:
			t = tsPipeline()
		case result.VM:
			t = vmPipeline()
		case result.Closure:
			t = closurePipeline()
		}
		pl := pipe.WithConst(codegen.TargetFeature, x.target, pipe.WithConst(plandata.ReachFeature, map[plandata.Entry]bool(nil), t.pipeline))
		for _, s := range t.steps {
			if !slices.Contains(a.steps, s) {
				a.steps = append(a.steps, s)
			}
		}
		a.one = pl
		branches = append(branches, pipe.B(x.branch, pl))
		a.branches = append(a.branches, x.branch)
	}
	if len(c.targets) > 1 {
		a.forked, a.one, a.fork = true, pipe.Pipeline[*Program, Output]{}, pipe.Fork(branches...)
	}
	return a
}

// describe returns the description of the pipeline of a.
func (a assembly) describe() *pipe.Description {
	if a.forked {
		return pipe.Describe(a.fork)
	}
	return pipe.Describe(a.one)
}

// apply returns a with the edits applied.
func (a assembly) apply(edits ...pipe.Edit) (assembly, error) {
	var err error
	if a.forked {
		a.fork, err = pipe.Apply(a.fork, edits...)
	} else {
		a.one, err = pipe.Apply(a.one, edits...)
	}
	return a, err
}

// reach returns the edit that gives the plan of the target i of a the reach r.
func (a assembly) reach(i int, r map[plandata.Entry]bool) pipe.Edit {
	e := pipe.SetConst(plandata.ReachFeature, r)
	if a.forked {
		return pipe.InBranch(a.branches[i], e)
	}
	return e
}

// error returns the failure err of a run of the pipeline of one target: the failure of one of the
// steps a.steps as the *pass.StepError that names it, with the message of the step, and any other
// failure (a pass, a verification, a join, an invalid pipeline) as an internal error.
func (a assembly) error(err error) error {
	return assembled{steps: a.steps}.error(err)
}

// forkError returns the failure err of a run of the Fork of a, which joins the failures of its
// branches in their order: the failure of each as error makes it, named with its target.
func (a assembly) forkError(err error) error {
	j, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return a.error(err)
	}
	var errs []error
	for _, e := range j.Unwrap() {
		var se *pipe.StepError
		if errors.As(e, &se) && se.Label.Backend != "" {
			errs = append(errs, fmt.Errorf("target %s: %w", se.Label.Backend, a.error(e)))
		} else {
			errs = append(errs, a.error(e))
		}
	}
	return errors.Join(errs...)
}

// error returns the failure err of a run of the pipeline: the failure of one of the steps a.steps as
// the *pass.StepError that names it, with the message of the step, a cancellation (the context of
// the compilation ended) as it is, naming the step that saw it, and any other failure (a pass, a
// verification, a join, an invalid pipeline) as an internal error.
func (a assembled) error(err error) error {
	var se *pipe.StepError
	if errors.As(err, &se) && slices.Contains(a.steps, se.Step) {
		return &pass.StepError{Step: pass.Step{Name: se.Step, Required: true}, Err: se.Err}
	}
	if errors.As(err, new(cancel.Cancellation)) {
		return err
	}
	return fmt.Errorf("internal error: %w", err)
}

// recoverCancellation is the recovery of a compilation (pipe.Run.Recover): the cancellation panic of
// a step (cancel.Cancellation), which the Core build and the Core passes raise when the context of
// the compilation ends, becomes the failure of the step; any other value panics again.
func recoverCancellation(x any) error {
	if c, ok := x.(cancel.Cancellation); ok {
		return c
	}
	panic(x)
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
	case g.Start != "" && (p.Start() == nil || g.Start != p.Start().Name):
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
