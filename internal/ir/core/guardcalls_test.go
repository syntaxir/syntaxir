package core

import (
	"slices"
	"testing"
)

// TestGuardCalls checks the calls that the evaluation of an expression makes before its first
// terminal fails: those on the path of First, outermost first, and none where the path has a choice,
// meets a leader or a call with a binding level, or enters a Nest.
func TestGuardCalls(t *testing.T) {
	// a = x:@("ab" c);  b = c;  c = -[0-9] "q";  l = l "x" / "y" (leader);  o = d;  d = l;  e = b "z"
	digit := class('0', '9')
	m := module(
		rule(0, "a", &Capture{Slot: 0, Name: "x", Body: &Text{Body: seq(lit("ab"), &Call{Rule: 2})}}, "x"),
		rule(1, "b", &Call{Rule: 2}),
		rule(2, "c", seq(&Drop{Body: digit}, lit("q"))),
		rule(3, "l", choice(seq(&Call{Rule: 3}, lit("x")), lit("y"))),
		rule(4, "o", &Call{Rule: 5}),
		rule(5, "d", &Call{Rule: 3}),
		rule(6, "n", &Nest{Rule: 1, Body: &Nest{Rule: 2, Body: seq(&Drop{Body: &ClassNode{Class: digit.Class}}, lit("q"))}}),
		rule(7, "e", seq(&Call{Rule: 1}, lit("z"))),
	)
	m.Rules[3].Policy.Leader = true
	analyzed(t, m)
	for _, c := range []struct {
		rule  int
		calls []int
		ok    bool
	}{
		{0, nil, true},
		{1, []int{2}, true},
		{2, nil, true},
		{3, nil, false}, // a choice
		{4, nil, false}, // a leader on the path
		{6, nil, false}, // a Nest
		{7, []int{1, 2}, true},
	} {
		calls, ok := GuardCalls(m, m.Rules[c.rule].Body)
		if ok != c.ok || !slices.Equal(calls, c.calls) {
			t.Errorf("rule %s: calls %v ok %t, want %v %t", m.Rules[c.rule].Name, calls, ok, c.calls, c.ok)
		}
	}
	if calls, ok := GuardCalls(m, &Call{Rule: 2, Level: 1}); ok {
		t.Errorf("a call with a binding level: calls %v, want none", calls)
	}
}
