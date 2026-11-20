package common

import (
	"io"

	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/middleend/execlower"
	"zeroparse.dev/zp/internal/middleend/flowlower"
	"zeroparse.dev/zp/internal/middleend/flowpass"
	"zeroparse.dev/zp/internal/pass"
	"zeroparse.dev/zp/internal/pass/pipe"
)

// Passes are the pass settings of one generation, which the driver resolves from the GenOptions of
// the generation (internal/driver/generate): the optimizations of GenOptions.Optimize, Opt and NoOpt,
// the dumps of GenOptions.PrintAfter and the verification of GenOptions.VerifyEachPass. The
// generators run the pipelines through a Runner, which is the only way to read the resolved
// optimizations (Runner.Settings, Generator.Opt), so that a profile or a flag means the same passes
// for the passes and the printers whatever the backend, and the back end does not import the driver.
type Passes interface {
	// Validate returns the error of resolving the settings: a profile or an optimization that does
	// not exist, a value that it does not take, a name that Opt gives twice, or a PrintAfter list
	// that names an unknown step or has no PrintTo writer. A generator asks for it where it
	// validates its options, so that the error of an option that it checks first comes first. It
	// returns no settings: they come from the Runner.
	Validate() error
	// Runner returns the runner of the pipelines with the settings, which writes the dumps that
	// PrintAfter selects to w, or none if w is nil. Validate must have returned no error.
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
	Flow(table string, u flowlower.Unit, observe bool, target flowpass.Target) (*code.Module, error)
	// Exec runs the Exec part of the pipeline on the module m of the functions of the table named
	// table, which Flow returned: the conversion to Exec IR (execlower), the Exec passes that every
	// target runs and then p, the pipeline of the target's own Exec passes, on each function;
	// observe is as for Flow. A failure of the conversion is a *pass.StepError, returned with no
	// module; a failure of a pass is returned with the module.
	Exec(table string, m *code.Module, p TargetPipeline, observe bool) (*code.Module, error)
}

// TargetPipeline is the pipeline of the Exec passes of one target (golang/pass, typescript/pass), which
// run on a lowered function after the Exec passes that every target runs (Runner.Exec).
type TargetPipeline = pipe.Pipeline[*execlower.Lowered, *execlower.Lowered]
