//go:build zp_budget_controls_reference

package engine

// shapeUnits is 0 in the reference build, which keeps no budget control and so counts no units; the
// round-trip tests of the shape then compare the results alone.
func shapeUnits(TraceEvent) uint64 { return 0 }
