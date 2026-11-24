package lint_test

import (
	"context"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/compiler"
	"syntaxir.dev/sxir/pkg/grammar"
	"syntaxir.dev/sxir/pkg/lint"
	"syntaxir.dev/sxir/pkg/runtime"
)

// TestBudgetExcludesMissingTokenInsertion checks that the derived budget bounds a parse without
// missing-token insertion and that no bound of its form holds for a parse with it. With insertion
// enabled, every rule that contains #missing, directly or through the rules it calls, is evaluated
// again at every call, so the grammar below backtracks without memoization: on d nested
// parentheses its cost doubles with each level, although it inserts no token. The class is
// Linear, and the units with insertion grow as 2^d, so the derived default does not apply to a
// parse with insertion. The reference build counts no units, so there the test skips.
func TestBudgetExcludesMissingTokenInsertion(t *testing.T) {
	if !state.BudgetControlsEnabled {
		t.Skip("budget controls are unavailable in the reference build")
	}
	const src = "@start\nrule main = e $$\nrule e = t \"+\" e / t\nrule t = \"(\" e \")\" #missing / \"x\"\n"
	g, err := enginetest.ParseGrammar(src)
	if err != nil {
		t.Fatal(err)
	}
	c, err := lint.Classify(context.Background(), g, "main")
	if err != nil {
		t.Fatal(err)
	}
	if c.Class != lint.Linear || c.Budget == nil {
		t.Fatalf("got class %v, budget %v", c.Class, c.Budget)
	}
	p, err := compiler.NewReferenceParser(grammar.FromString("missing.sxir", src))
	if err != nil {
		t.Fatal(err)
	}
	units := func(d, insertions int) uint64 {
		t.Helper()
		var u uint64
		in := strings.Repeat("(", d) + "x" + strings.Repeat(")", d)
		if _, err := p.Parse(in, runtime.WithMissingTokens(insertions), runtime.WithComputeReport(&u)); err != nil {
			t.Fatalf("d=%d, insertion limit %d: %v", d, insertions, err)
		}
		return u
	}
	var prev uint64
	for _, d := range []int{4, 8, 12, 16} {
		bound := c.Budget.PerUnit*uint64(2*d+1) + c.Budget.Fixed
		off, on := units(d, 0), units(d, 1)
		t.Logf("d=%d: bound %d, without insertion %d, with insertion %d", d, bound, off, on)
		if off > bound {
			t.Errorf("d=%d: %d units without insertion exceed the derived bound %d", d, off, bound)
		}
		// Four more levels multiply the units by 2^4; 15 leaves room for the constant terms.
		if prev != 0 && on < 15*prev {
			t.Errorf("d=%d: %d units with insertion, want at least 15 times %d at d=%d", d, on, prev, d-4)
		}
		prev = on
	}
	if bound := c.Budget.PerUnit*33 + c.Budget.Fixed; prev <= bound {
		t.Errorf("d=16: %d units with insertion are within the derived bound %d", prev, bound)
	}
}
