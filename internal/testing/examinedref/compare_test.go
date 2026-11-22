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
		{Rule: "e", Pos: 0, End: 0, Examined: 0, Growing: true},
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
		{name: "a skipped leader call whose evaluation matches and whose seed fails", diffs: 1, calls: []examinedref.Observed{
			{Exit: examinedref.Exit{Rule: "e", Pos: 0, End: 0}, Depth: 2, Skipped: true}, call(ref[4], 1)}},
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

// TestCompareParse checks the comparison of a whole parse: the start call of a parse that completes,
// and the abort of one that aborts, which must happen where an action of the reference fails, after
// the reference's exits before that action and no others.
func TestCompareParse(t *testing.T) {
	a := examinedref.Exit{Rule: "a", End: 1, Examined: 1, Matched: true}
	b := examinedref.Exit{Rule: "b", Pos: 1, End: 1, Examined: 2}
	c := examinedref.Exit{Rule: "c", Pos: 1, End: 2, Examined: 2, Matched: true}
	start := examinedref.Exit{Rule: "main", End: 2, Examined: 2, Matched: true}
	exits := []examinedref.Exit{a, b, c, start}
	obs := func(xs ...examinedref.Exit) []examinedref.Observed {
		var out []examinedref.Observed
		for _, x := range xs {
			depth := 2
			if x.Rule == "main" {
				depth = 1
			}
			out = append(out, examinedref.Observed{Exit: x, Depth: depth})
		}
		return out
	}
	skippedB := append(obs(a), examinedref.Observed{Exit: examinedref.Exit{Rule: "b", Pos: 1, End: 1}, Depth: 2, Skipped: true})
	res := func(errs ...int) examinedref.Result {
		return examinedref.Result{Matched: true, End: 2, Examined: 2, ActionErrors: errs}
	}
	tests := []struct {
		name     string
		res      examinedref.Result
		observed []examinedref.Observed
		aborted  bool
		actions  examinedref.Actions
		differs  bool
	}{
		{"a parse that completes as the reference does agrees", res(), obs(a, b, c, start), false, examinedref.EveryAction, false},
		{"a parse that ends without its start call differs", res(), obs(a, b, c), false, examinedref.EveryAction, true},
		{"an abort after the exits before the failing action agrees", res(2), obs(a, b), true, examinedref.EveryAction, false},
		{"an abort after a skip of the last call before the failing action agrees", res(2), skippedB, true, examinedref.EveryAction, false},
		{"an abort before the failing action differs", res(2), obs(a), true, examinedref.EveryAction, true},
		{"an abort after the failing action differs", res(2), obs(a, b, c), true, examinedref.EveryAction, true},
		{"an abort after a call the reference makes only after the failing action differs", res(2), obs(c, b), true, examinedref.EveryAction, true},
		{"an abort where no action of the reference fails differs", res(), obs(a, b), true, examinedref.EveryAction, true},
		{"an abort before any call agrees with an action that fails first", res(0), nil, true, examinedref.EveryAction, false},
		{"an abort after a call differs from an action that fails first", res(0), obs(a), true, examinedref.EveryAction, true},
		{"a parse that runs every action must abort at the first failure", res(1, 3), obs(a, b, c), true, examinedref.EveryAction, true},
		{"a parse that runs every action must not complete when one fails", res(2), obs(a, b, c, start), false, examinedref.EveryAction, true},
		{"a recognizer may abort at a later failure", res(1, 3), obs(a, b, c), true, examinedref.ReadActions, false},
		{"a recognizer may complete when an action fails", res(2), obs(a, b, c, start), false, examinedref.ReadActions, false},
		{"a recognizer must abort at a failure", res(1, 3), obs(a, b), true, examinedref.ReadActions, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if d := examinedref.CompareParse(tc.res, exits, "main", tc.observed, tc.aborted, tc.actions); (len(d) > 0) != tc.differs {
				t.Fatalf("differences %q, want some: %v", d, tc.differs)
			}
		})
	}
}
