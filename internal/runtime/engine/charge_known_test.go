package engine

// knownChargeDivergences lists the pairs of charge case and backend that diverge from the closure
// backend today, as "case|backend", and as "case|table" the cases
// whose closure units differ from chargeTableUnits. The backends do not yet charge the
// abstract units that spec/runtime/parsing.md requires; each change that makes a backend charge
// them removes its pairs, and a new divergence fails the tests.
var knownChargeDivergences = map[string]bool{
	"choice of sequences|table":     true,
	"class repetition|table":        true,
	"class sequence|table":          true,
	"literal|table":                 true,
	"lookahead|table":               true,
	"repeat or class|table":         true,
	"rules|table":                   true,
	"duckdb|bytecode":               true,
	"duckdb|bytecode-iterative":     true,
	"golang|bytecode":               true,
	"golang|bytecode-iterative":     true,
	"python|bytecode":               true,
	"python|bytecode-iterative":     true,
	"typescript|bytecode":           true,
	"typescript|bytecode-iterative": true,
	"xml|bytecode":                  true,
	"xml|bytecode-iterative":        true,
	"yaml|bytecode":                 true,
	"yaml|bytecode-iterative":       true,
}
