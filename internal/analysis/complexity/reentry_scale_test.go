package complexity_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/complexitytest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/starttest"
)

// TestReentryWitnessScale measures the grammar whose continuation a re-enters the cycle on every
// path, which the classifier leaves out of the comparison as a whole and classifies linear: its
// cost is linear on every backend on nested parentheses, where each level tries "(" c ")" "y",
// fails at z, and goes on to the later alternatives, a among them, whose left call of c is
// answered at the position. The alternative "n" before a matches in every step of a growth of c,
// so no input grows c with "+", and the input holds none.
func TestReentryWitnessScale(t *testing.T) {
	if !state.BudgetControlsEnabled {
		t.Skip("the reference build has no compute limit")
	}
	const src = "@start\nrule main = c $$\nrule a = c \"+\"\nrule c = \"n\" / \"(\" c \")\" \"y\" / \"(\" c \")\" \"z\" / a\n"
	g, err := enginetest.ParseGrammar(src)
	if err != nil {
		t.Fatal(err)
	}
	p, err := enginetest.Compile(starttest.MarkAll(g), "main")
	if err != nil {
		t.Fatal(err)
	}
	input := func(n int) string { return strings.Repeat("(", n) + "n" + strings.Repeat(")z", n) }
	for _, b := range complexitytest.Backends {
		s := complexitytest.Series{Sizes: []int{125, 250, 500}}
		for _, n := range s.Sizes {
			cost, err := complexitytest.StepsLenient(p, input(n), b, complexitytest.Cap)
			if err != nil {
				t.Fatalf("%s: size %d: %v", complexitytest.BackendName(b), n, err)
			}
			s.Costs = append(s.Costs, cost)
			if cost.ParseErr != nil || cost.Capped {
				t.Fatalf("%s: size %d: the parse failed: %+v", complexitytest.BackendName(b), n, cost)
			}
		}
		t.Logf("%s: costs %v (slopes %.2f)", complexitytest.BackendName(b), s.Costs, s.Slopes())
		if got := complexitytest.Classify(s); got != complexitytest.Linear {
			t.Errorf("%s: costs %v (slopes %.2f) measured %v, want linear", complexitytest.BackendName(b), s.Costs, s.Slopes(), got)
		}
	}
}
