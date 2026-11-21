package corepass

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/ir/core"
)

// TestLeadersOverlappingCycles checks the leaders of two overlapping cycles, a-b and b-c, in both orders of
// the alternatives of b. The rule declared first on any cycle that passes through no leader becomes one: a
// is declared first on the a-b cycle, and then b is declared first on the b-c cycle, so both orders give
// the leaders a and b.
func TestLeadersOverlappingCycles(t *testing.T) {
	const (
		ruleA = 0
		ruleB = 1
		ruleC = 2
	)
	tests := []struct {
		name string
		b    []core.Node // the alternatives of b, in order
	}{
		{"a alternative first", []core.Node{
			seq(&core.Call{Rule: ruleA}, lit("z")),
			seq(&core.Call{Rule: ruleC}, lit("u")),
			lit("w"),
		}},
		{"c alternative first", []core.Node{
			seq(&core.Call{Rule: ruleC}, lit("u")),
			seq(&core.Call{Rule: ruleA}, lit("z")),
			lit("w"),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := module(
				rule(ruleA, "a", choice(seq(&core.Call{Rule: ruleB}, lit("x")), lit("y"))),
				rule(ruleB, "b", choice(tt.b...)),
				rule(ruleC, "c", choice(seq(&core.Call{Rule: ruleB}, lit("v")), lit("t"))),
			)
			f := Compute(m, nil)
			var got []string
			for i, r := range f.Rules {
				if r.Leader {
					got = append(got, m.Rules[i].Name)
				}
			}
			if want := []string{"a", "b"}; !slices.Equal(got, want) {
				t.Errorf("leaders = %v, want %v", got, want)
			}
		})
	}
}
