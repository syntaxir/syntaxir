package corepass

import (
	"context"
	"io"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The optimization passes of Core IR as a pipeline (design 063). The driver runs them in the
// pipeline of a compilation, and a run-time backend with Run; a pass has the name of the flag that
// turns it off. The passes know no target and no generation mode: what depends on them is decided by
// the Flow passes (internal/middleend/flowpass).

// The attributes of the nodes of a Core module (Nullable, NullableIns, First and Eff, which
// core.Analyze computes) are content of the unit, not an analysis: elaborate makes a module with
// them computed, the passes read them from the nodes, and a pass that changes nodes keeps them
// current. The verifier of the kind (Verify) rejects a module whose attributes are stale.

// entry is the name of the step that builds the Core IR, elaborate, under which Run dumps the module
// as it reaches the first pass. The driver declares the step, and this package cannot import it.
const entry = "elaborate"

// Names of the passes. A name is also the name of the flag that turns the pass off.
const (
	PassGuards = "guards"
	PassScans  = "scans"
)

// CounterGuarded is the counter of guards: the alternatives that it gave a guard.
const CounterGuarded = "guarded"

// CounterScanned is the counter of scans: the repetitions that it replaced by a scan.
const CounterScanned = "scanned"

// GuardsPass marks each alternative of a choice with the first terminal it cannot match without.
var GuardsPass = pipe.New(pipe.Def[*core.Module, *core.Module]{
	Decl: pipe.Decl{
		Name:       PassGuards,
		Obligation: "A guard is the first terminal of its alternative, which the alternative cannot match without.",
		Counters:   []string{CounterGuarded},
	},
	In: IR, Out: IR,
	Run: func(c *pipe.Ctx[*core.Module], m *core.Module) (*core.Module, pass.PreservedSet, error) {
		// Guards read the First sets of the nodes. Without them the alternatives of a choice get no
		// first-character guard and the Flow form calls the rules of the later alternatives at every
		// position (duckdb: block_comment, 84,000 calls against none); the verifier rejects a module
		// whose First sets are missing or stale.
		c.Count(CounterGuarded, setGuards(m))
		return m, pass.PreserveNone(), nil
	},
})

// ScansPass runs a value-free repetition of a class or of any character as a scan.
var ScansPass = pipe.New(pipe.Def[*core.Module, *core.Module]{
	Decl: pipe.Decl{
		Name:       PassScans,
		Obligation: "Consumes the same input and records the same expectations as the repetition it replaces.",
		Counters:   []string{CounterScanned},
	},
	In: IR, Out: IR,
	Run: func(c *pipe.Ctx[*core.Module], m *core.Module) (*core.Module, pass.PreservedSet, error) {
		// A Stream is replaced as its repetition only where it delivers no elements (Eff). A Scan
		// takes the attributes of the repetition it replaces, which are those that core.Analyze
		// computes for it, so the attributes stay current.
		c.Count(CounterScanned, replaceScans(m))
		return m, pass.PreserveNone(), nil
	},
})

// Passes are the Core passes in the order they run: choice guards and scans.
var Passes = []*pipe.Pass[*core.Module, *core.Module]{GuardsPass, ScansPass}

// Pipeline is the pipeline of the Core passes.
func Pipeline() pipe.Pipeline[*core.Module, *core.Module] { return pipe.Seq(IR, Passes...) }

// Names returns the names of the Core passes in the order they run.
func Names() []string {
	names := make([]string, len(Passes))
	for i, p := range Passes {
		names[i] = p.Name()
	}
	return names
}

// RunOptions selects what a run of the Core passes does besides running them.
type RunOptions struct {
	// Settings turns the passes on and off by their flags; the zero value runs every pass.
	Settings pass.Settings
	// VerifyEach verifies the module after every pass.
	VerifyEach bool
	// Dump, if not nil, is called with "elaborate" before the first pass and with the name of each pass
	// that ran, after it.
	Dump func(pass string, m *core.Module)
}

// Verify verifies the Core module m: the attributes of its nodes are those that core.Analyze computes
// (core.CheckAttributes), and the module holds the invariants of core.Verify, which reads them. It
// never writes m: elaborate computes the attributes, and a pass that changes them keeps them so.
func Verify(m *core.Module) error {
	if err := core.CheckAttributes(m); err != nil {
		return err
	}
	return core.Verify(m)
}

// Run runs the enabled Core passes on m, and verifies the module if a pass ran. It returns the
// module after them, and the names of the passes that ran. It consumes m: the passes change it in
// place, and a failure drops it (pipe.Run.Stop). The verifier checks the attributes of m and does
// not compute them: m must have them, as elaborate makes it.
func Run(m *core.Module, o RunOptions) (*core.Module, []string, error) {
	if o.Dump != nil {
		o.Dump(entry, m)
	}
	ob := &ranObserver{dump: o.Dump}
	out, err := pipe.Execute(context.Background(), &pipe.Run{Settings: o.Settings, VerifyEach: o.VerifyEach, Stop: true, Observers: []pipe.Observer{ob}}, Pipeline(), m)
	if err != nil {
		return nil, ob.ran, err
	}
	return out, ob.ran, nil
}

// ranObserver records the passes that ran, and dumps the module after each of them.
type ranObserver struct {
	dump func(pass string, m *core.Module)
	ran  []string
}

func (o *ranObserver) Needs() pipe.Needs                       { return pipe.Needs{Units: o.dump != nil} }
func (o *ranObserver) Before(pipe.Event, func(io.Writer)) bool { return true }

func (o *ranObserver) After(e pipe.Event, r *pipe.Result) {
	if !r.Ran || r.Err != nil {
		return
	}
	o.ran = append(o.ran, e.Step.Instance)
	if o.dump != nil {
		o.dump(e.Step.Instance, r.Unit.(*core.Module))
	}
}
