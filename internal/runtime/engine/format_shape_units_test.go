//go:build !zp_budget_controls_reference

package engine

import "zeroparse.dev/zp/internal/runtime/state"

// shapeUnits reads the units that the parse behind the trace event has reserved so far.
func shapeUnits(e TraceEvent) uint64 { return state.TraceParser(e).Control.Used }
