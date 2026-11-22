package typescript

import "syntaxir.dev/sxir/internal/pass"

// The parameters that only the TypeScript printer reads, which print-ts declares.
const (
	// TSLiteralSharing shares the literal tables of a TypeScript parser, with
	// codegen.LiteralSharing.
	TSLiteralSharing = "ts-literal-sharing"
	SparseMemo       = "sparse-memo"
)

func init() {
	pass.Register(
		pass.Flag{Name: TSLiteralSharing, Kind: pass.KindPrinter, Default: 0, Doc: "share literal tables in TypeScript parsers; off because a small cold parse measured about 1.5 times slower (design 090)"},
		pass.Flag{Name: SparseMemo, Kind: pass.KindStorage, Default: 1, Doc: "keep the first-call table of large TypeScript parsers sparse"},
	)
}
