package corepass

import (
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/pass"
)

// The optimization passes of Core IR as a pipeline (design 063). Generators run it with Run; a
// pass has the name of the flag that turns it off. The passes know no target and no generation
// mode: what depends on them is decided by the Flow passes (internal/middleend/flowpass).

// AttributesAnalysis is the analysis that core.Analyze computes: Nullable, First and Eff of every node.
// A pass that adds or replaces nodes makes it stale; a pass preserves it only by saying so.
const AttributesAnalysis pass.Analysis = "attributes"

// Stage is the name of the Core IR stage of the pipeline, which the steps on Core IR take and
// return (pass.Step).
const Stage = "core"

// entry is the name of the step that builds the Core IR, elaborate, under which a run dumps the
// module as it reaches the first pass. The driver declares the step, and this package cannot import
// it.
const entry = "elaborate"

// Names of the passes. A name is also the name of the flag that turns the pass off.
const (
	PassGuards = "guards"
	PassScans  = "scans"
)

// Env is the context of a run of the pipeline.
type Env struct {
	// Flag reports whether the optimization of the given name is on. Nil means every one is.
	Flag func(name string) bool
}

// On reports whether the optimization of the given name is on.
func (e *Env) On(name string) bool { return e.Flag == nil || e.Flag(name) }

// Pipeline is the pipeline of the Core passes: choice guards and scans.
var Pipeline = &pass.Pipeline[*core.Module, *Env]{
	Passes: []pass.Pass[*core.Module, *Env]{
		{
			Name: PassGuards,
			// Guards read the First sets that Analyze computes. Without them the alternatives of a choice
			// get no first-character guard and the Flow form calls the rules of the later alternatives at
			// every position (duckdb: block_comment, 84,000 calls against none), and nothing reports it.
			Requires:   []pass.Analysis{AttributesAnalysis},
			Obligation: "A guard is the first terminal of its alternative, which the alternative cannot match without.",
			// Guards mark the alternatives of choices and add no node: the attributes hold.
			Run: func(m *core.Module, _ *Env) (*core.Module, pass.PreservedSet, error) {
				Guards(m)
				return m, pass.Preserve(string(AttributesAnalysis)), nil
			},
		},
		{
			Name: PassScans,
			// A Stream is replaced as its repetition only where it delivers no elements (Eff).
			Requires:   []pass.Analysis{AttributesAnalysis},
			Obligation: "Consumes the same input and records the same expectations as the repetition it replaces.",
			Run: func(m *core.Module, _ *Env) (*core.Module, pass.PreservedSet, error) {
				Scans(m)
				return m, pass.PreserveNone(), nil
			},
		},
	},
	Analyses: map[pass.Analysis]func(*core.Module){AttributesAnalysis: core.Analyze},
	Verify:   Verify,
	// The attributes, which the analysis writes into the nodes, travel with the copy.
	Copy: (*core.Module).Clone,
}

// RunOptions selects what a run of Pipeline does besides running the passes.
type RunOptions struct {
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

// Run runs the enabled passes of Pipeline on m, and verifies the module. It returns the module after
// them, and the names of the passes that ran. It consumes m (pass.Pipeline.Run). The verifier checks
// the attributes of m and does not compute them: m must have them, as elaborate makes it, unless a
// pass that requires them runs, before which the run computes them.
func Run(m *core.Module, e *Env, o RunOptions) (*core.Module, []string, error) {
	return Pipeline.Run(m, e, o.Options(e))
}

// Options returns the options of the generic pipeline for a run under e, for a caller that runs a
// pipeline of its own (tests).
func (o RunOptions) Options(e *Env) pass.Options[*core.Module] {
	return pass.Options[*core.Module]{Enabled: e.On, VerifyEach: o.VerifyEach, Dump: o.Dump, Entry: entry}
}
