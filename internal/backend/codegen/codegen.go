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

// Derived builds the programs that a generator derives from the checked program of a grammar, as the
// engine derives them, each checked on its first call: the recognizer, in which no rule builds a
// value; the insertion program, which the parses with missing-token insertion enabled run, with the
// rule flags of insertion; and the recognizer of the insertion program.
type Derived struct {
	Recognizer, Insertion, InsertionRecognizer func() (Checked, error)
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
		pass.Flag{Name: MethodSharing, Kind: pass.KindPrinter, Default: 1, Doc: "emit one helper for generated bodies that are exactly equal (Go and TypeScript)",
			Tradeoff:  "Code against calls: smaller source, with a call where a body was",
			Mechanism: "G010", Record: 136, Backends: pass.Generated,
			Witnesses: []string{"equal bodies", "recovery"}},
		pass.Flag{Name: LiteralSharing, Kind: pass.KindPrinter, Default: 1, Doc: "emit one table for equal literals (Go; TypeScript also needs ts-literal-sharing)",
			Tradeoff:  "Smaller source against an indirection to the table",
			Mechanism: "G130", Record: 136, Backends: pass.GeneratedGo,
			Witnesses: []string{"recovery"}},
		pass.Flag{Name: UsedFeatures, Kind: pass.KindPrinter, Default: 1, Doc: "leave out the library code of the features that the grammar and the options rule out, and the declarations of the embedded library that the parser does not name (with -lib embed)",
			Tradeoff:  "The features are such as left recursion, recovery, Operators Rules, streams or Documents; `-lib only` writes the whole library. Off, the parser carries every feature; what a parse returns and charges is the same either way. Smaller source and binary, at no cost on average; single TypeScript workloads move by about 3% either way, as the JavaScript engine inlines the smaller library differently",
			Mechanism: "G250", Record: 144, Backends: pass.Generated,
			Witnesses: []string{"guards"}},
	)
}

// Params are the parameters that the printing steps of both backends declare: those above.
var Params = []string{MethodSharing, LiteralSharing, UsedFeatures}

// ParamsOf returns the values of Params that value, the parameter reader of a printing step's context
// (pipe.Ctx.Value), gives.
func ParamsOf(value func(string) int) common.Params {
	return common.Params{
		MethodSharing:  value(MethodSharing) != 0,
		LiteralSharing: value(LiteralSharing) != 0,
		UsedFeatures:   value(UsedFeatures) != 0,
	}
}
