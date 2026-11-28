//go:build !sxir_reference_node_frames

package libgo

// nodeFrameScratch selects capture frames from parser scratch, which a parser keeps for its next
// parse. The reference build (sxir_reference_node_frames) takes them from a slab of each parse
// instead.
const nodeFrameScratch = true
