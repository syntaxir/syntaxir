//go:build sxir_reference_scratch_pools

package libgo

// The reference build makes a parser for every whole-input parse and keeps none.
const scratchPools = false
