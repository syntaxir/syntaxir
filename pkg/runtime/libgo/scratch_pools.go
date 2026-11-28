//go:build !sxir_reference_scratch_pools

package libgo

// scratchPools keeps the parser of a finished whole-input parse, with its scratch memory, in ppool or
// tpool for a later parse. The reference build (sxir_reference_scratch_pools) keeps none: every parse
// makes its own parser.
const scratchPools = true
