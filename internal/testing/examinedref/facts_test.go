package examinedref

import (
	"reflect"
	"testing"

	"syntaxir.dev/sxir/internal/testing/enginetest"
)

// TestLeaders checks the leaders against the definitions in spec/core-ir/attributes.md (Rule Facts) on
// small grammars worked out by hand.
func TestLeaders(t *testing.T) {
	tests := []struct {
		name, src string
		want      []string
	}{
		{
			name: "a rule with a left call of itself is a leader",
			src:  "rule e = e \"+\" \"1\" / \"1\"",
			want: []string{"e"},
		},
		{
			name: "a rule that consumes before its recursive call is no leader",
			src:  "rule c = \"x\" c / \"y\"",
			want: nil,
		},
		{
			name: "a nullable item before a call still makes the call a left call",
			src:  "rule a = n b \"x\" / \"y\"\nrule n = \"\"\nrule b = a \"k\"",
			want: []string{"a"},
		},
		{
			name: "an alternative that always succeeds ends the left calls of a choice",
			src:  "rule a = \"\" / b\nrule b = a \"x\"",
			want: nil,
		},
		{
			name: "of a cycle, the rule declared first becomes the leader",
			src:  "rule a = b \"x\" / \"y\"\nrule b = a \"z\" / \"w\"",
			want: []string{"a"},
		},
		{
			name: "cycles that share a rule each get a leader until all pass through one",
			src:  "rule a = b \"x\" / \"y\"\nrule b = a \"z\" / c \"u\" / \"w\"\nrule c = b \"v\" / \"t\"",
			want: []string{"a", "b"},
		},
		{
			name: "the same cycles with the alternatives of the shared rule reversed give the same leaders",
			src:  "rule a = b \"x\" / \"y\"\nrule b = c \"u\" / a \"z\" / \"w\"\nrule c = b \"v\" / \"t\"",
			want: []string{"a", "b"},
		},
		{
			name: "after an operand that can match nothing, the postfix calls of an Operators Rule are left calls",
			src:  "rule e = #operators {\n    operand \"\"\n    level a { postfix x }\n}\nrule x = e \"+\"",
			want: []string{"e"},
		},
		{
			name: "after an operand that can match nothing, the infix calls of an Operators Rule are left calls",
			src:  "rule e = #operators {\n    operand \"n\" / \"\"\n    level { infix left y }\n}\nrule y = e \"*\"",
			want: []string{"e"},
		},
		{
			name: "after an operand that consumes, the postfix calls of an Operators Rule are no left calls",
			src:  "rule e = #operators {\n    operand \"n\"\n    level a { postfix x }\n}\nrule x = e \"+\"",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := enginetest.ParseGrammar(tt.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			rs, err := newRuleSet(g)
			if err != nil {
				t.Fatalf("rules: %v", err)
			}
			facts, err := analyzeRules(rs, false)
			if err != nil {
				t.Fatalf("analyze: %v", err)
			}
			var got []string
			for _, r := range g.Rules() {
				if facts.leaders[r.Name] {
					got = append(got, r.Name)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("leaders = %v, want %v", got, tt.want)
			}
		})
	}
}
