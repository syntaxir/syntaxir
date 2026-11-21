package corepass

import (
	"testing"

	"syntaxir.dev/sxir/internal/ir/core"
)

// TestLeaderInsertion checks the leaders of the parse with token insertion enabled. A #missing
// node is nullable only with insertion (NullableIns), so the call of a in the body of a is a left
// call only in that parse: a is a leader in the parse with insertion and not in the one without.
func TestLeaderInsertion(t *testing.T) {
	const ruleA = 0
	m := module(
		rule(ruleA, "a", choice(
			seq(&core.Missing{Lit: lit("v"), Body: lit("v")}, &core.Call{Rule: ruleA}, lit("x")),
			lit("y"),
		)),
	)
	f := Compute(m, nil)
	r := f.Rules[ruleA]
	if r.Leader {
		t.Errorf("Leader = true without insertion, want false: the #missing node is not nullable")
	}
	if !r.LeaderIns {
		t.Errorf("LeaderIns = false with insertion, want true: the #missing node is nullable with insertion")
	}
	if r.CycleIns == 0 {
		t.Errorf("CycleIns = 0 with insertion, want the cycle of a")
	}
}
