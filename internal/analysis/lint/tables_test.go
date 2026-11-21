package lint

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend/parser"
)

// TestRuleTablesRefineLeftCalls requires the rule facts lint reads to come from the fixed point of the
// left-call refinement, not from its first rounds. A choice stops its left calls after an
// alternative that always succeeds, but a rule on a cycle is not proven to succeed: a's first
// alternative b always succeeds, which removes a's left call of itself in the second round, and
// only then is a proven to succeed, which removes c's left call of itself in the third.
func TestRuleTablesRefineLeftCalls(t *testing.T) {
	t.Parallel()
	g, err := parser.Parse("@start\nrule main = c\nrule c = a / c \"z\"\nrule a = b / a \"x\"\nrule b = \"y\"?")
	if err != nil {
		t.Fatal(err)
	}
	a := newAnalysis(g)
	for _, name := range []string{"a", "c"} {
		if a.leftRecursive(name) || !a.rule(name).Succeeds {
			t.Errorf("rule %s: cyclic %v, succeeds %v; want not cyclic and succeeding", name, a.leftRecursive(name), a.rule(name).Succeeds)
		}
	}
}
