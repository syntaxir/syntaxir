package common

// The parameters of the printing steps that both printers read (Params). Package codegen registers
// them as flags.
const (
	// MethodSharing emits one helper for generated bodies that are exactly equal.
	MethodSharing = "method-sharing"
	// LiteralSharing emits one table for equal literals.
	LiteralSharing = "literal-sharing"
	// UsedFeatures narrows a parser's library to the features that the analysis finds
	// (ParserFeatures); with it off, a parser has AllFeatures, the differential baseline of the
	// pruning.
	UsedFeatures = "used-features"
)

// Params are the values of the parameters that the printing step of a backend declares and gives its
// printer as data: the printer reads no settings of the compilation. Each backend's printer extends
// them with its own (the Go and TypeScript printers' Params).
type Params struct {
	// MethodSharing emits one helper for generated bodies that are exactly equal.
	MethodSharing bool
	// LiteralSharing emits one table for equal literals.
	LiteralSharing bool
	// UsedFeatures narrows the library of a parser to the features that the analysis finds
	// (ParserFeatures).
	UsedFeatures bool
	// Allow, if not nil, decides one site of a parameter that is on: the printing step's decision of
	// the pattern named like the parameter (pipe.Ctx.Allow), which a bisection limit can skip. A
	// skipped site is printed as with the parameter off. Nil allows every site.
	Allow func(param string) bool
}

// Decide reports whether one site of the parameter param, which is on, gets it (Allow).
func (p Params) Decide(param string) bool { return p.Allow == nil || p.Allow(param) }
