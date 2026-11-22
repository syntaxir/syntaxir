package examinedref_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/examinedref"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// TestCompareCalls checks that the comparison reports each kind of difference between reported calls
// and the reference, and accepts the calls a producer may leave out or make several times.
func TestCompareCalls(t *testing.T) {
	ref := []examinedref.Exit{
		{Rule: "n", Pos: 0, End: 1, Examined: 2, Matched: true},
		{Rule: "e", Pos: 0, End: 0, Examined: 0},
		{Rule: "e", Pos: 0, End: 1, Examined: 2, Matched: true},
		{Rule: "e", Level: 1, Pos: 2, End: 2, Examined: 3},
		{Rule: "main", Pos: 0, End: 1, Examined: 2, Matched: true},
	}
	res := examinedref.Result{Matched: true, End: 1, Examined: 2}
	call := func(x examinedref.Exit, depth int) examinedref.Observed {
		return examinedref.Observed{Exit: x, Depth: depth}
	}
	tests := []struct {
		name  string
		calls []examinedref.Observed
		diffs int
	}{
		{name: "the same calls", calls: []examinedref.Observed{call(ref[0], 3), call(ref[1], 3), call(ref[2], 2), call(ref[4], 1)}},
		{name: "fewer calls and both steps of a growth", calls: []examinedref.Observed{call(ref[2], 2), call(ref[1], 3), call(ref[4], 1)}},
		{name: "an examined end too large", diffs: 1, calls: []examinedref.Observed{
			call(examinedref.Exit{Rule: "n", Pos: 0, End: 1, Examined: 3, Matched: true}, 3), call(ref[4], 1)}},
		{name: "an examined end too small", diffs: 1, calls: []examinedref.Observed{
			call(examinedref.Exit{Rule: "n", Pos: 0, End: 1, Examined: 1, Matched: true}, 3), call(ref[4], 1)}},
		{name: "a wrong end", diffs: 1, calls: []examinedref.Observed{
			call(examinedref.Exit{Rule: "n", Pos: 0, End: 2, Examined: 2, Matched: true}, 3), call(ref[4], 1)}},
		{name: "a wrong outcome", diffs: 1, calls: []examinedref.Observed{
			call(examinedref.Exit{Rule: "n", Pos: 0, End: 0, Examined: 2}, 3), call(ref[4], 1)}},
		{name: "a wrong level", diffs: 1, calls: []examinedref.Observed{
			call(examinedref.Exit{Rule: "e", Level: 2, Pos: 2, End: 2, Examined: 3}, 2), call(ref[4], 1)}},
		{name: "a call the reference does not make", diffs: 1, calls: []examinedref.Observed{
			call(examinedref.Exit{Rule: "n", Pos: 1, End: 1, Examined: 2}, 3), call(ref[4], 1)}},
		{name: "a skipped call that fails", calls: []examinedref.Observed{
			{Exit: examinedref.Exit{Rule: "e", Level: 1, Pos: 2, End: 2}, Depth: 2, Skipped: true}, call(ref[4], 1)}},
		{name: "a skipped call that matches", diffs: 1, calls: []examinedref.Observed{
			{Exit: examinedref.Exit{Rule: "n", Pos: 0, End: 0}, Depth: 2, Skipped: true}, call(ref[4], 1)}},
		// A start call with a wrong examined end differs as a call and as the start call.
		{name: "a start call with another examined end", diffs: 2, calls: []examinedref.Observed{
			call(examinedref.Exit{Rule: "main", Pos: 0, End: 1, Examined: 3, Matched: true}, 1)}},
		{name: "no start call", diffs: 1, calls: []examinedref.Observed{call(ref[0], 3)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := append(examinedref.CompareCalls(ref, tt.calls), examinedref.CompareStart(res, "main", tt.calls)...)
			if len(diffs) != tt.diffs {
				t.Fatalf("%d differences, want %d: %q", len(diffs), tt.diffs, diffs)
			}
		})
	}
}

// TestRecorder checks that the recorder keeps the exits and skipped calls of a trace and drops the
// enter events.
func TestRecorder(t *testing.T) {
	var r examinedref.Recorder
	r.Trace(instrument.TraceEvent{Kind: instrument.TraceEnter, Rule: "main", Depth: 1})
	r.Trace(instrument.TraceEvent{Kind: instrument.TraceSkipped, Rule: "a", Pos: 1, Depth: 2})
	r.Trace(instrument.TraceEvent{Kind: instrument.TraceExit, Rule: "main", Depth: 1, End: 2, Examined: 3, Matched: true})
	want := []examinedref.Observed{
		{Exit: examinedref.Exit{Rule: "a", Pos: 1, End: 1}, Depth: 2, Skipped: true},
		{Exit: examinedref.Exit{Rule: "main", End: 2, Examined: 3, Matched: true}, Depth: 1},
	}
	if len(r.Calls) != len(want) || r.Calls[0] != want[0] || r.Calls[1] != want[1] {
		t.Fatalf("calls %+v, want %+v", r.Calls, want)
	}
	r.Reset()
	if len(r.Calls) != 0 {
		t.Fatalf("calls after Reset: %+v", r.Calls)
	}
}
