package driver

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	goprint "syntaxir.dev/sxir/internal/backend/codegen/golang/print"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/execlower"
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
}

// The slots of the assembled pipelines, for Edits: the Core passes of a table, the Flow and the Exec
// passes that run on each function of a table, and the Exec passes of the target that run on each
// lowered function.
var (
	CoreSlot   = pipe.NewSlot("core-passes", corepass.IR)
	FlowSlot   = pipe.NewSlot("flow-passes", code.FlowStage)
	ExecSlot   = pipe.NewSlot("exec-passes", code.ExecStage)
	TargetSlot = pipe.NewSlot("target-passes", execlower.LoweredIR)
)

// Compile runs one pipeline for the targets under one manager. On an error it returns no output and
// the errors of every failed target, joined in target order. Until the pipelines of several targets
// are forked (record 074, migration step 9), a call compiles exactly one target.
func Compile(ctx context.Context, p *Program, o Options, targets ...Target) ([]Output, error) {
	if len(targets) != 1 {
		return nil, fmt.Errorf("a compilation takes one target, not %d", len(targets))
	}
	t := targets[0]
	if err := checkTarget(p, o, t); err != nil {
		return nil, err
	}
	if err := CheckDebug(o.Debug); err != nil {
		return nil, err
	}
	// The target's options that the program and the call hold are theirs.
	po := p.Options()
	t.Gen.SourceInfo, t.Gen.DefaultValue, t.Gen.LeftRecursionBindings = po.SourceInfo, po.DefaultValue, po.LeftRecursionBindings
	t.Gen.Debug = o.Debug
	c := &compilation{p: p, t: t, o: o}
	a, err := assemble(c)
	if err != nil {
		return nil, err
	}
	pl, err := pipe.Apply(a.pipeline, o.Edits...)
	if err != nil {
		return nil, err
	}
	d := debugger(o)
	out, err := execute(ctx, d, &pipe.Run{Settings: o.Settings, VerifyEach: o.Debug.VerifyEach, Observers: d.observers("")}, pl, c)
	// The dumps are written also on an error, whose dumps show what led to it; the reports only
	// after a build.
	if ferr := d.flush(); ferr != nil && err == nil {
		err = ferr
	}
	if err != nil {
		return nil, a.error(err)
	}
	d.Report()
	return []Output{out}, nil
}

// debugger returns the driver whose observers write what the debugging settings of the call o select
// and run its observers, with the dumps of each table kept together (Driver.out).
func debugger(o Options) *Driver {
	g := o.Debug
	return &Driver{Opt: o.Settings, PrintAfter: g.PrintAfter, PrintBefore: g.PrintBefore, PrintChanged: g.PrintChanged,
		VerifyEach: g.VerifyEach, Stats: g.Stats, TimePasses: g.TimePasses, OptBisectLimit: g.OptBisectLimit,
		Output: g.Output, Hooks: Hooks{Observers: o.Observers}, groups: &groups{}, numbering: &pipe.Numbering{}}
}

// assembled is the pipeline of one target and the steps whose failure is an error of the compilation
// itself, such as a grammar that the target cannot generate a parser for.
type assembled struct {
	pipeline pipe.Pipeline[*compilation, Output]
	// steps are the instances of those steps: required steps such as elaborate or the printer, and
	// splits ("each <name>") whose parts are decided from the program.
	steps []string
}

// assemble returns the pipeline of the target of the compilation c, from the program to its output,
// with the target as its printer reads it (common.TargetFeature).
func assemble(c *compilation) (assembled, error) {
	switch c.t.Backend {
	case result.Go:
		opts, err := goprint.Validate(c.t.Gen)
		if err != nil {
			return assembled{}, err
		}
		c.t.Gen = opts
		p := c.p
		c.target = goprint.NewGoTarget(p.View(), p.Recognizer, p.Start(), opts, c.o.Settings)
		return goPipeline(c)
	}
	return assembled{}, fmt.Errorf("internal error: no pipeline is assembled for the backend %s", c.t.Backend)
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
