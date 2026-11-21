//go:build !sxir_budget_controls_reference

package fmt_test

import (
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
)

// shapeUnits reads the units that the parse behind the trace event has reserved so far.
func shapeUnits(e engine.TraceEvent) uint64 { return state.TraceParser(e).Control.Used }
