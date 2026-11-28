//go:build !sxir_reference_scratch_pools

package state

// ScratchPools keeps the buffers of a finished whole-input parse in its program (SharedProgram.scratch)
// for a later parse. The reference build (sxir_reference_scratch_pools) keeps none: every parse
// allocates its own.
const ScratchPools = true
