//go:build sxir_budget_controls_reference

package fmt_test

import "syntaxir.dev/sxir/internal/runtime/engine"

// shapeUnits is 0 in the reference build, which keeps no budget control and so counts no units; the
// round-trip tests of the shape then compare the results alone.
func shapeUnits(engine.TraceEvent) uint64 { return 0 }
