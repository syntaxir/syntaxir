package common

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
}
