package golang

import "syntaxir.dev/sxir/internal/pass"

// The parameters that only the Go printer reads, which print-go declares.
const (
	TypedObjectSlabs     = "typed-object-slabs"
	TypedListSlabs       = "typed-list-slabs"
	TypedOutputBootstrap = "typed-output-bootstrap"
	TypedChunkGrowth     = "typed-chunk-growth"
)

func init() {
	pass.Register(
		pass.Flag{Name: TypedObjectSlabs, Kind: pass.KindStorage, Default: 1, Doc: "allocate typed objects in slabs (typed Go)"},
		pass.Flag{Name: TypedListSlabs, Kind: pass.KindStorage, Default: 1, Doc: "allocate typed lists in slabs (typed Go)"},
		pass.Flag{Name: TypedOutputBootstrap, Kind: pass.KindStorage, Default: 1, Doc: "size the first slab of the typed output from the input (typed Go)"},
		pass.Flag{Name: TypedChunkGrowth, Kind: pass.KindStorage, Default: 1, Doc: "grow the slabs of typed values with the values allocated before them (typed Go)"},
	)
}
