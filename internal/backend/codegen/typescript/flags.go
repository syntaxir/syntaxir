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
		pass.Flag{Name: TSLiteralSharing, Kind: pass.KindPrinter, Default: 0, Doc: "share literal tables in TypeScript parsers",
			Tradeoff:  "Smaller code against speed: a small cold parse measured about 1.5 times slower",
			Mechanism: "G130", Record: 136, Backends: pass.On(pass.BackendTypeScript),
			Witnesses: []string{"recovery"}},
		pass.Flag{Name: SparseMemo, Kind: pass.KindStorage, Default: 1, Doc: "keep the first-call table of large TypeScript parsers sparse",
			Tradeoff:  "Memory in proportion to the positions visited against a page lookup per check",
			Mechanism: "R030", Record: 136, Backends: pass.On(pass.BackendTypeScript),
			Witnesses: []string{"many memoized rules"}},
	)
}
