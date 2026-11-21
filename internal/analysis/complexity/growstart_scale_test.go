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

// TestGrowStartScale measures the grammars whose growing unit goes on at its start in the first
// step and whose later alternative can begin with what it matches there, and holds the classifier
// to the measurement: a grammar whose cost grows faster than linearly is not classified linear.
// The cost is the least compute limit under which the parse ends, which is deterministic. In a
// later step the unit scans the x after the seed and fails, and the later alternative consumes
// the character at the position where the rule grows and starts a growth at the next position,
// which scans the x again, so the cost grows as n^2. Without the check (the mutant) each grammar
// is classified linear, and the test fails. The reference build has no compute limit, so it checks
// the classes alone.
func TestGrowStartScale(t *testing.T) {
	checkGrowScale(t, complexity.SetGrowStart, []growScaleCase{
		{"later alternative after a shared literal",
			"@start\nrule main = E $$\nrule E = (E [x]* / \"\") \"x\" / \"x\" F\nrule F = E \";\"\n",
			func(n int) string { return strings.Repeat("x", n+1) + strings.Repeat(";", n) }},
		{"later alternative after a shared call",
			"@start\nrule main = G \"!\"\nrule G = (G [x]* / \"\") \"(\" L / \"(\" G \"y\" / \"b\" [x]*\nrule L = \"(\" L / \"b\"\n",
			func(n int) string {
				return strings.Repeat("(", n) + "b" + strings.Repeat("x", n) + strings.Repeat("y", n) + "!"
			}},
	})
}

// TestGrowNullableScale measures the grammars whose growing unit goes on at its start in the
// first step through a path that matches without consuming input, before a negative lookahead
// that examines a character with which the later alternative cannot begin, as TestGrowStartScale
// does. On x^n ; the path matches at each x, and the later alternative consumes it in a later step
// and starts a growth at the next position, which scans the x again, so the cost grows as n^2.
// Without the rule (the mutant) each grammar is classified linear, and the test fails.
func TestGrowNullableScale(t *testing.T) {
	input := func(n int) string { return strings.Repeat("x", n) + ";" }
	checkGrowScale(t, complexity.SetGrowNullable, []growScaleCase{
		{"path that ends with a negative lookahead",
			"@start\nrule main = E $$\nrule E = ((E [x]* / \"\") !\";\" / \"x\" F) \"x\"\nrule F = E \",\"\n", input},
		{"path with a negative lookahead and an empty literal",
			"@start\nrule main = E $$\nrule E = ((E [x]* / \"\") !\";\" \"\" / \"x\" F) \"x\"\nrule F = E \",\"\n", input},
	})
}

type growScaleCase struct {
	name, src string
	input     func(n int) string
}

// checkGrowScale measures each case at three sizes on every backend and requires a cost that grows
// as n^2, and classifies it with the check that set turns on and off: on, it must not be linear;
// off (the mutant), it must be linear, or the grammar would not witness the check.
func checkGrowScale(t *testing.T, set func(bool) func(), cases []growScaleCase) {
	t.Helper()
	checkGrowScaleAt(t, set, []int{125, 250, 500}, cases)
}

// checkGrowScaleAt is checkGrowScale at the given sizes.
func checkGrowScaleAt(t *testing.T, set func(bool) func(), sizes []int, cases []growScaleCase) {
	t.Helper()
	defer set(true)()
	for _, c := range cases {
		g, err := enginetest.ParseGrammar(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		g = starttest.MarkAll(g)
		p, err := enginetest.Compile(g, "main")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		backends := complexitytest.Backends
		if !state.BudgetControlsEnabled {
			backends = nil
		}
		for _, b := range backends {
			s := complexitytest.Series{Sizes: sizes}
			for _, n := range s.Sizes {
				cost, err := complexitytest.StepsLenient(p, c.input(n), b, complexitytest.Cap)
				if err != nil {
					t.Fatalf("%s: %s: size %d: %v", c.name, complexitytest.BackendName(b), n, err)
				}
				s.Costs = append(s.Costs, cost)
			}
			t.Logf("%s: %s: costs %v (slopes %.2f)", c.name, complexitytest.BackendName(b), s.Costs, s.Slopes())
			if got := complexitytest.Classify(s); got != complexitytest.Poly(2) {
				t.Errorf("%s: %s: costs %v (slopes %.2f) measured %v, want %v", c.name, complexitytest.BackendName(b), s.Costs, s.Slopes(), got, complexitytest.Poly(2))
			}
		}
		for _, on := range []bool{true, false} {
			restore := set(on)
			got, err := complexity.ClassifyComplexityControlled(g, "main", nil)
			restore()
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			linear := got.Class == complexity.ClassLinear || got.Class == complexity.ClassLinearEnv
			switch {
			case on && linear:
				t.Errorf("%s: classified %v, measured %v", c.name, got.Class, complexitytest.Poly(2))
			case !on && !linear:
				// The mutant must claim linear, or the grammar would not witness the check.
				t.Errorf("%s without the check: classified %v, want linear", c.name, got.Class)
			}
		}
	}
}

// droppedStepMain repeats l, and the pieces of input that l leaves, on ("ab+{")^n "}".
const droppedStepMain = "@start\nrule main = (l / \"+{\" / \"}\")* $$\n"

// droppedStepScan scans the rest of the input and fails at its end, through a repetition whose
// element calls a left-recursive rule, so that it is no run site.
const droppedStepScan = "rule z = \"b\" \"+\" \"{\" (c / x)* \"}\" \"!\"\nrule c = \"a\" / \"b\" / \"+\" / \"{\"\nrule x = x \"#\" / \"#\"\n"

// droppedStepSizes are the sizes measured: each step of a growth scans the rest of the input, so
// smaller sizes than the default show the slope at a fraction of the time.
var droppedStepSizes = []int{40, 80, 160}

func droppedStepInput(n int) string { return strings.Repeat("ab+{", n) + "}" }

// TestGrowStartDroppedStepScale measures grammars in which a later step of a growth of l goes on
// to a part of the body that the first step did not reach, and holds the check of a growing unit
// at the start of its first-step paths to them. The first step matches the seed ab through the
// inner alternative "a"; the second reads the seed, matches +, fails at { and goes on to the last
// alternative at the position where l grows. That alternative begins with a, as the first-step
// path does: z scans the rest of the input and fails, the part fails or ends short of the seed,
// the step is dropped, and the next growth four characters on scans the rest again, so the cost
// grows as n^2. Without the check (the mutant) each grammar is classified linear.
func TestGrowStartDroppedStepScale(t *testing.T) {
	checkGrowScaleAt(t, complexity.SetGrowStart, droppedStepSizes, []growScaleCase{
		{"part that fails after the scan", droppedStepMain + "rule l = (l \"+\" / \"a\") \"b\" / \"a\" z\n" + droppedStepScan, droppedStepInput},
		{"part that gives up the scan in an option", droppedStepMain + "rule l = (l \"+\" / \"a\") \"b\" / \"a\" z?\n" + droppedStepScan, droppedStepInput},
		{"part that gives up the scan in a called option", droppedStepMain + "rule l = (l \"+\" / \"a\") \"b\" / \"a\" w\nrule w = z?\n" + droppedStepScan, droppedStepInput},
		{"part that gives up the scan in a repetition", droppedStepMain + "rule l = (l \"+\" / \"a\") \"b\" / \"a\" z*\n" + droppedStepScan, droppedStepInput},
		{"part that gives up the scan in an optional choice", droppedStepMain + "rule l = (l \"+\" / \"a\") \"b\" / \"a\" (z / \"q\")?\n" + droppedStepScan, droppedStepInput},
	})
}

// TestGrowStartDroppedStepControls measures grammars near those of TestGrowStartDroppedStepScale
// that stay linear and are classified linear: an earlier alternative that the later steps
// evaluate again and that matches again, so the part with the scan is never reached, and a part
// that cannot begin with what the first-step path matches. The reference build has no compute
// limit, so it checks the classes alone.
func TestGrowStartDroppedStepControls(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"earlier alternative matches again", droppedStepMain + "rule l = l \"+\" \"b\" / \"a\" \"b\" / \"a\" z\n" + droppedStepScan},
		{"part that begins otherwise", droppedStepMain + "rule l = (l \"+\" / \"a\") \"b\" / \"q\" z\n" + droppedStepScan},
	} {
		g, err := enginetest.ParseGrammar(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		g = starttest.MarkAll(g)
		got, err := complexity.ClassifyComplexityControlled(g, "main", nil)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.Class != complexity.ClassLinear {
			t.Errorf("%s: classified %v, want linear\nreasons %v", c.name, got.Class, got.Reasons)
		}
		if !state.BudgetControlsEnabled {
			continue
		}
		p, err := enginetest.Compile(g, "main")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, b := range complexitytest.Backends {
			s := complexitytest.Series{Sizes: droppedStepSizes}
			for _, n := range s.Sizes {
				cost, err := complexitytest.StepsLenient(p, droppedStepInput(n), b, complexitytest.Cap)
				if err != nil {
					t.Fatalf("%s: %s: size %d: %v", c.name, complexitytest.BackendName(b), n, err)
				}
				s.Costs = append(s.Costs, cost)
			}
			if got := complexitytest.Classify(s); got != complexitytest.Linear {
				t.Errorf("%s: %s: costs %v (slopes %.2f) measured %v, want linear", c.name, complexitytest.BackendName(b), s.Costs, s.Slopes(), got)
			}
		}
	}
}
