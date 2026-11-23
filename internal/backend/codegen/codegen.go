// Package codegen holds what the generating backends declare alike for their printing steps: the
// checked program as a backend reads it, the target of the pipeline that a printing step reads, and
// the parameters that the Go and TypeScript
// printing steps share, which each step reads from its context and gives its printer as data
// (common.Params). The print packages import none of it: they receive the target's program and the
// parameters' values.
package codegen

import (
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/middleend/actionpass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Checked is a checked program as a generating backend reads it: the view that its printer reads and
// the input of the elaboration of its tables to Core IR (elaborate.Program), which the steps of the
// pipeline build the Core IR from and no printer reads.
type Checked struct {
	View *common.Program
	Elab elaborate.Program
}

// TargetFeature is the target of the pipeline of a backend (with target=go), which its printing step
// reads. Its text names the backend; a pipeline text cannot provide it as a constant, since the
// target holds the program.
var TargetFeature = pipe.NewFeature("target", common.Target.Backend, nil)

// The parameters that the Go and TypeScript printing steps share.
const (
	// MethodSharing emits one helper for generated bodies that are exactly equal.
	MethodSharing = "method-sharing"
	// LiteralSharing emits one table for equal literals.
	LiteralSharing = "literal-sharing"
	// UsedFeatures narrows a parser's library to the features that the analysis finds
	// (common.ParserFeatures); with it off, a parser has common.AllFeatures, the differential
	// baseline of the pruning.
	UsedFeatures = "used-features"
)

func init() {
	pass.Register(
		pass.Flag{Name: MethodSharing, Kind: pass.KindPrinter, Default: 1, Doc: "emit one helper for generated bodies that are exactly equal (Go and TypeScript)"},
		pass.Flag{Name: LiteralSharing, Kind: pass.KindPrinter, Default: 1, Doc: "emit one table for equal literals (Go; TypeScript also needs ts-literal-sharing)"},
		pass.Flag{Name: UsedFeatures, Kind: pass.KindPrinter, Default: 1, Doc: "leave out the library code of the features that the grammar and the options rule out (Go with the embedded library); off, the parser carries every feature"},
	)
}

// Params are the parameters that the printing steps of both backends declare: those above and the
// flag of the pass text-dispatch (actionpass.PassTextDispatch), which the printers pass to the action
// lowering they still run.
var Params = []string{MethodSharing, LiteralSharing, UsedFeatures, actionpass.PassTextDispatch}

// ParamsOf returns the values of Params that value, the parameter reader of a printing step's context
// (pipe.Ctx.Value), gives.
func ParamsOf(value func(string) int) common.Params {
	return common.Params{
		MethodSharing:  value(MethodSharing) != 0,
		LiteralSharing: value(LiteralSharing) != 0,
		UsedFeatures:   value(UsedFeatures) != 0,
		TextDispatch:   value(actionpass.PassTextDispatch) != 0,
	}
}
