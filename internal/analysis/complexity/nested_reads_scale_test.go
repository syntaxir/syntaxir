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

// TestNestedGrowthReadsScale measures growths of a leader T nested in the steps of another
// leader R at the same position, where T reads the growing result of R. When no result of T
// includes that growing result, the growth of T is fixed by the outcomes of its reads: which unit
// that holds a read fails innermost. When a later result of T includes it after an earlier one
// that does not, a later step of T fails after its left call read the seed in a unit that matched
// in the first step without it, and goes on with a continuation that holds the read; condition 2
// checks that unit against the continuation. The baseline, whose T reads nothing of R, is
// classified linear. The other grammars read R in T and are all classified unknown; none of them
// measures more than linear on inputs that grow R through T, so no linear grammar of this shape is
// known, and the classification is conservative here. In the first four, every result of T
// includes the growing result of R from the first, and R's own alternatives are not committing.
// The case of the late read takes the shape that condition 2 excludes: on t(rk)^n r, a step of T
// reads its seed in (T "x"? / "") "t", fails, and reaches R "k", which its first step did not
// evaluate, and the check of that alternative of T against R "k" refuses it. The control is the
// cut that condition 5 excludes in a cycle with more than one leader: a cut passed after a left
// call of R makes a later step of R reach S "x", which its first step did not, so each step of S
// grows R over the b characters again, and its cost grows as n^2 in a Document parse, which
// compares the examined ranges of growing results in a context: a parse that compares none merges
// the steps that differ only in their ranges and stays linear on these inputs, so the control is
// measured on the first parse of a Document. The grammar
// T = T "w" / R !"y" "x" / "t" "y" is left out because the inputs tried fail early, and a
// positive lookahead with a left call of T before the read, which condition 5 also excludes, is
// left out as a control because it measured linear. The reference build has no compute limit, so
// it checks the classes alone.
func TestNestedGrowthReadsScale(t *testing.T) {
	const r = "rule R = R \"z\" / T \"r\" / \"q\"\n"
	cases := []struct {
		name, src string
		input     func(n int) string
		order     complexitytest.Order
		linear    bool
		// document measures the first parse of a Document instead of an ordinary parse.
		document bool
		// reason, when set, is part of a reason for the class unknown that the case must have.
		reason string
	}{
		{"baseline: T reads nothing of R",
			"@start\nrule main = R $$\n" + r + "rule T = T \"w\" / \"t\"\n",
			func(n int) string { return "t" + strings.Repeat("w", n) + "r" + strings.Repeat("z", n) }, complexitytest.Linear, true, false, ""},
		{"read under an option before a literal",
			"@start\nrule main = R $$\n" + r + "rule T = T \"w\" / (R \"x\"? / \"\") \"t\" / \"t\" \"t\" \"y\"\n",
			func(n int) string { return strings.Repeat("tr", n) }, complexitytest.Linear, false, false, ""},
		{"read before a literal",
			"@start\nrule main = R $$\n" + r + "rule T = T \"w\" / R \"x\" / \"t\"\n",
			func(n int) string { return "tr" + strings.Repeat("xr", n) }, complexitytest.Linear, false, false, ""},
		{"read before two literals",
			"@start\nrule main = R $$\n" + r + "rule T = T \"w\" / R \"x\" \"x\" / \"t\"\n",
			func(n int) string { return "tr" + strings.Repeat("xxr", n) }, complexitytest.Linear, false, false, ""},
		{"read before a literal with a later alternative",
			"@start\nrule main = R $$\n" + r + "rule T = T \"w\" / R \"x\" / \"t\" / \"q\" \"y\"\n",
			func(n int) string { return "tr" + strings.Repeat("xr", n) }, complexitytest.Linear, false, false, ""},
		{"late read after a unit that read the seed",
			"@start\nrule main = R $$\n" + r + "rule T = T \"w\" / (T \"x\"? / \"\") \"t\" / R \"k\"\n",
			func(n int) string { return "t" + strings.Repeat("rk", n) + "r" }, complexitytest.Linear, false, false,
			`the alternative at 4:18 in rule T is not committing against the later alternative R "k"`},
		{"control: a cut passed after a left call",
			"@start\nrule main = S $$\nrule S = S \"w\" / R \"z\"\nrule R = (R #cut \"b\" / S \"y\" / \"c\") / S \"x\"\n",
			func(n int) string { return "c" + strings.Repeat("b", n) + "z" + strings.Repeat("xz", n) }, complexitytest.Poly(2), false, true, ""},
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
		if linear := got.Class == complexity.ClassLinear; linear != c.linear {
			t.Errorf("%s: classified %v, linear %v expected", c.name, got.Class, c.linear)
		}
		if c.reason != "" && !hasReason(got.Reasons, c.reason) {
			t.Errorf("%s: no reason containing %q in %v", c.name, c.reason, got.Reasons)
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
				measure := complexitytest.StepsLenient
				if c.document {
					measure = complexitytest.DocumentStepsLenient
				}
				cost, err := measure(p, c.input(n), b, complexitytest.Cap)
				if err != nil {
					t.Fatalf("%s: %s: size %d: %v", c.name, complexitytest.BackendName(b), n, err)
				}
				if cost.ParseErr != nil {
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

// hasReason reports whether one of the reasons for the class unknown contains part.
func hasReason(reasons []complexity.ComplexityCause, part string) bool {
	for _, r := range reasons {
		if strings.Contains(r.Message, part) {
			return true
		}
	}
	return false
}
