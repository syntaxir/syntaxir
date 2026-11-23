package complexity

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/starttest"
)

// TestEnumerationBudgetCharged checks that building the paths of a continuation spends the
// exclusion budget, and that a unit whose enumeration runs out of it gets a reason: the paths
// are counted steps, so the cut is the same on every run.
func TestEnumerationBudgetCharged(t *testing.T) {
	g, err := parser.Parse("@start\nrule s = \"a\" \"b\"\n")
	if err != nil {
		t.Fatal(err)
	}
	k, _, err := prepareClassifier(starttest.MarkAll(g), "s", nil)
	if err != nil {
		t.Fatal(err)
	}
	x := newExcl(k, true, decidedUnitsK)
	paths := [][]sym{{{kind: symSkip}}, {{kind: symSkip}}}
	if _, ok := x.concat(paths, paths); !ok {
		t.Fatal("a concatenation within the budget failed")
	}
	if got := x.enumerationWhy(); got != "" {
		t.Fatalf("a reason before the budget ran out: %q", got)
	}
	*x.work = 0
	if _, ok := x.concat(paths, paths); ok {
		t.Fatal("a concatenation past the budget succeeded")
	}
	if got := x.enumerationWhy(); got == "" {
		t.Fatal("no reason for an enumeration that ran out of the budget")
	}
}
