package common

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
)

// Target is the target of a compilation as its printer reads it: the program, the options of the
// target and the settings of the compilation. Each generating backend has its own.
type Target interface {
	// Backend names the backend of the target, which is the text of the feature.
	Backend() string
}

// Replan is the output of a printer whose printed code enters other rules than the plan of the
// program reaches (plandata.Program.Reach), in place of its files: Reach is the reach of the printed
// code. Printing can leave out calls that the Core IR has, such as a call after an expression that
// never matches, so the plan reaches rules that nothing printed enters. The driver then runs the
// compilation again with Reach as the reach of the plan (plandata.ReachFeature), which is the fallback
// for planning, and returns the output of that run.
type Replan struct {
	Lang  result.Backend
	Reach map[plandata.Entry]bool
}

// Backend returns the backend of the printer.
func (r *Replan) Backend() result.Backend { return r.Lang }

// TableSource is the unit table: the build of the Core IR of one table of a program, which the step
// elaborate runs, and the key of the build, which is the same for the tables whose builds make the
// same module, so that they share one run of the Core part of the pipeline. Build polls control (nil
// for none), which the module keeps (core.Module.Control), and unwinds as cancel.Cancellation when
// its context ends.
type TableSource struct {
	Build func(control *cancel.Control) (*core.Module, error)
	Key   string
}
