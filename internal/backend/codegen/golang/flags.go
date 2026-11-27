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
		pass.Flag{Name: TypedObjectSlabs, Kind: pass.KindStorage, Default: 1, Doc: "allocate typed objects in slabs (typed Go)",
			Tradeoff:  "Fewer allocations against memory: an object that escapes keeps the unused objects of its chunk",
			Mechanism: "A250", Record: 136, Backends: pass.On(pass.BackendTypedGo),
			Witnesses: []string{"guards"}},
		pass.Flag{Name: TypedListSlabs, Kind: pass.KindStorage, Default: 1, Doc: "allocate typed lists in slabs (typed Go)",
			Tradeoff:  "Fewer allocations against memory, as for objects",
			Mechanism: "A260", Record: 136, Backends: pass.On(pass.BackendTypedGo),
			Witnesses: []string{"guards"}},
		pass.Flag{Name: TypedOutputBootstrap, Kind: pass.KindStorage, Default: 1, Doc: "size the first slab of the typed output from the input (typed Go)",
			Tradeoff:  "A small parse keeps little memory, against a few allocations before the slabs start",
			Mechanism: "A070", Record: 136, Backends: pass.On(pass.BackendTypedGo),
			Witnesses: []string{"guards"}},
		pass.Flag{Name: TypedChunkGrowth, Kind: pass.KindStorage, Default: 1, Doc: "grow the slabs of typed values with the values allocated before them (typed Go)",
			Tradeoff:  "Allocations that grow with the logarithm of the output against an unused tail of at most 1/33 of the values",
			Mechanism: "A280", Record: 139, Backends: pass.On(pass.BackendTypedGo),
			Witnesses: []string{"guards"}},
	)
}
