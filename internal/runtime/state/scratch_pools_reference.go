//go:build sxir_reference_scratch_pools

package state

// The reference build allocates the buffers of every whole-input parse and keeps none.
const ScratchPools = false
