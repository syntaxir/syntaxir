package common

import (
	"io"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Passes are the pass settings of one generation, which the driver resolves from the GenOptions of
// the generation (internal/driver/generate): the optimizations of GenOptions.Optimize, Opt and NoOpt,
// and the dumps, the verification and the reports of GenOptions.Debug. The
// generators run the pipelines through a Runner, which is the only way to read the resolved
// optimizations (Runner.Settings, Generator.Opt), so that a profile or a flag means the same passes
// for the passes and the printers whatever the backend, and the back end does not import the driver.
type Passes interface {
	// Validate returns the error of resolving the settings: a profile or an optimization that does
	// not exist, a value that it does not take, a name that Opt gives twice, a Debug.PrintAfter list
	// that names an unknown step, or a Debug that writes and has no Output. A generator asks for it where it
	// validates its options, so that the error of an option that it checks first comes first. It
	// returns no settings: they come from the Runner.
	Validate() error
	// Runner returns the runner of the pipelines with the settings, which writes the dumps and the
	// reports that Debug selects to w, or none if w is nil: a generator passes Debug.Output for the
	// run that makes its output and nil for a run whose output it drops. The verification of
	// Debug.VerifyEach applies to both. Validate must have returned no error.
	Runner(w io.Writer) Runner
}

// A Runner runs the pass pipelines of the IR for a generator, under the settings of its Passes. A
// failure is an internal error that names the pass. The driver implements it (driver.Driver).
type Runner interface {
	// Settings returns the optimizations that the runner runs the pipelines with: the settings of
	// its Passes, which are the only optimizations that a generator reads (Generator.Opt), so that
	// the printers and the passes use the same ones.
	Settings() pass.Settings
	// Core runs the Core part of the pipeline for the table named table: the step elaborate, which
	// runs build, the elaboration of the table's program to Core IR, and the Core passes. It returns
	// the module after them. A failure of build is a *pass.StepError with the message of build.
	Core(table string, build func() (*core.Module, error)) (*core.Module, error)
	// Flow runs the Flow part of the pipeline on the functions of the table named table: the
	// lowering of u (flowlower), the Flow passes on each function and flow-finish; observe says that
	// the parser reports its rules and skipped alternatives, and target which forms the backend
	// prints (flowpass.ObserveFeature, flowpass.TargetFeature). A failure of the lowering is a *pass.StepError, returned with no module;
	// a failure of a pass is returned with the module, ended by flow-finish.
	Flow(table string, u lowerdata.Unit, observe bool, target flowpass.Target) (*code.Module, error)
	// Exec runs the Exec part of the pipeline on the module m of the functions of the table named
	// table, which Flow returned: the conversion to Exec IR (execlower), the Exec passes that every
	// target runs and then p, the pipeline of the target's own Exec passes, on each function;
	// observe is as for Flow. A failure of the conversion is a *pass.StepError, returned with no
	// module; a failure of a pass is returned with the module.
	Exec(table string, m *code.Module, p TargetPipeline, observe bool) (*code.Module, error)
	// LowerTable runs the pipeline of the planned table p, the table named table, from its lowering
	// to the Exec stage in one run: the steps of Flow and then those of Exec with p, the pipeline of
	// the target's own Exec passes, on the module of the table, whose functions the passes keep. A
	// failure of the lowering or the conversion is a *pass.StepError, returned with no table; a
	// failure of a pass is returned with the table, and the optional passes after it do not run.
	// Plan runs the step that plans the program c (flowplan.Planning): the routes of the calls of
	// its rules and the rules that its entry points reach. The plan has no dump.
	Plan(c *plandata.Cores) (*plandata.Program, error)
	LowerTable(table string, u plandata.Planned, observe bool, flowTarget flowpass.Target, p TargetPipeline) (*plandata.Table[code.Exec], error)
	// Report writes the reports of Debug.Stats and Debug.TimePasses over everything the runner ran
	// to its writer, if it has one. A generator calls it once, after the last pipeline of the run
	// that makes its output.
	Report()
}

// TargetPipeline is the pipeline of the Exec passes of one target (golang/pass, typescript/pass), which
// run on a lowered function after the Exec passes that every target runs (Runner.Exec).
type TargetPipeline = pipe.Pipeline[*execlower.Lowered, *execlower.Lowered]
