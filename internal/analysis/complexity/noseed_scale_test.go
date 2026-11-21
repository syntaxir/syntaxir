package complexity_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/analysis/complexity"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/complexitytest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/starttest"
)

// TestDroppedSeedWithoutSourceScale measures growths of a leader that contains no scanning
// source, in which a later step fails after it read the seed and ends in an alternative that the
// first step did not reach: its result is longer than the seed but does not include it. On xxy,
// the second step of E reads the seed x, takes "x"? and fails on "x" against y, and "x" "x" "y"
// matches from the start. Such a leader is the only rule of its cycle, so no growth nests in one
// of its steps, and the steps that drop the seed are a constant number of each growth: the
// grammars are classified linear and their cost grows linearly. The control has a scanning source
// (the call of E in F, a leader called in the body of another rule), and its dropped steps throw
// away a growth at every position, so its cost grows as n^2 and it is not classified linear. The
// reference build has no compute limit, so it checks the classes alone.
func TestDroppedSeedWithoutSourceScale(t *testing.T) {
	cases := []struct {
		name, src string
		input     func(n int) string
		order     complexitytest.Order
	}{
		{"literal alternative under a list",
			"@start\nrule main = (E \";\")* $$\nrule E = (E \"x\"? / \"\") \"x\" / \"x\" \"x\" \"y\"\n",
			func(n int) string { return strings.Repeat("xxy;xxx;", n) }, complexitytest.Linear},
		{"right recursion after the dropped seed",
			"@start\nrule main = E $$\nrule E = (E \"x\"? / \"\") \"x\" / \"x\" A \"y\"\nrule A = \"x\" A / \"\"\n",
			func(n int) string { return strings.Repeat("x", n) + "y" }, complexitytest.Linear},
		{"right recursion after the dropped seed under a list",
			"@start\nrule main = (E \";\")* $$\nrule E = (E \"x\"? / \"\") \"x\" / \"x\" A \"y\"\nrule A = \"x\" A / \"\"\n",
			func(n int) string { return strings.Repeat("xxxxxxxxxy;", n) }, complexitytest.Linear},
		{"control: a leader with a scanning source",
			"@start\nrule main = E $$\nrule E = (E [x]* / \"\") \"x\" / \"x\" F\nrule F = E \";\"\n",
			func(n int) string { return strings.Repeat("x", n+1) + strings.Repeat(";", n) }, complexitytest.Poly(2)},
	}
	for _, c := range cases {
		g, err := enginetest.ParseGrammar(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		g = starttest.MarkAll(g)
		got, err := complexity.ClassifyComplexityControlled(g, "main", nil)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if linear := got.Class == complexity.ClassLinear; linear != (c.order == complexitytest.Linear) {
			t.Errorf("%s: classified %v, measured order %v expected", c.name, got.Class, c.order)
		}
		if !state.BudgetControlsEnabled {
			continue
		}
		p, err := enginetest.Compile(g, "main")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, b := range complexitytest.Backends {
			s := complexitytest.Series{Sizes: []int{125, 250, 500}}
			for _, n := range s.Sizes {
				cost, err := complexitytest.StepsLenient(p, c.input(n), b, complexitytest.Cap)
				if err != nil {
					t.Fatalf("%s: %s: size %d: %v", c.name, complexitytest.BackendName(b), n, err)
				}
				if c.order == complexitytest.Linear && cost.ParseErr != nil {
					t.Fatalf("%s: %s: size %d: %v", c.name, complexitytest.BackendName(b), n, cost.ParseErr)
				}
				s.Costs = append(s.Costs, cost)
			}
			t.Logf("%s: %s: costs %v (slopes %.2f)", c.name, complexitytest.BackendName(b), s.Costs, s.Slopes())
			if got := complexitytest.Classify(s); got != c.order {
				t.Errorf("%s: %s: costs %v (slopes %.2f) measured %v, want %v", c.name, complexitytest.BackendName(b), s.Costs, s.Slopes(), got, c.order)
			}
		}
	}
}
