//go:build sxir_reference_node_frames

package libgo

// The reference build takes the capture frames of each parse from a slab of the parse, which the
// parser does not keep.
const nodeFrameScratch = false
