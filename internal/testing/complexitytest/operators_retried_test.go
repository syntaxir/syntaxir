package complexitytest

import (
	"context"
	"strings"
	"testing"

	"zeroparse.dev/zp/internal/analysis/complexity"
	"zeroparse.dev/zp/internal/testing/enginetest"
	"zeroparse.dev/zp/internal/testing/starttest"
)

// classifyMain classifies the grammar src from its rule main.
func classifyMain(t *testing.T, src string) *complexity.Complexity {
	t.Helper()
	g, err := enginetest.ParseGrammar(src)
	if err != nil {
		t.Fatal(err)
	}
	c, err := complexity.ClassifyComplexity(context.Background(), starttest.MarkAll(g), "main")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestOperatorsRetriedPartThatScans checks that the classifier keeps an Operators Rule unknown
// when the operator parts that its enclosing levels try again scan outside calls that the memo
// answers: the levels that end before the "!" each scan the "ab" that follow. The parse fails,
// since nothing after the rule consumes them, so it is measured with its syntax error; a control
// whose input succeeds would end with what follows the rule, which keeps the rule unknown on its
// own.
func TestOperatorsRetriedPartThatScans(t *testing.T) {
	t.Parallel()
	const src = "rule main = e $$\nrule e = #operators {\n    operand \"n\"\n    level a { infix right \"^\" }\n    level p { postfix \"!\" (\"ab\")* \"?\" }\n}\n"
	c := classifyMain(t, src)
	if c.Class != complexity.ClassUnknown {
		t.Errorf("classified %v, want unknown", c.Class)
	}
	skipWithoutBudget(t)
	p, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	input := func(n int) string { return rep("n^", n) + "n!" + rep("ab", n) }
	for _, b := range Backends {
		s := Series{Sizes: double(100)}
		for _, n := range s.Sizes {
			cost, err := StepsLenient(p, input(n), b, Cap)
			if err != nil || cost.ParseErr == nil {
				t.Fatalf("%s: size %d: %v, parse error %v", BackendName(b), n, err, cost.ParseErr)
			}
			s.Costs = append(s.Costs, cost)
		}
		if got := Classify(s); got != Poly(2) {
			t.Errorf("%s: %v (slopes %.2f) classified %v, want Poly(2)", BackendName(b), s.Costs, s.Slopes(), got)
		}
	}
}

// TestOperatorsRetriedCallPartWitness checks the witness of the example of
// spec/language/operators-rule.md: the levels of the prefix operators that end before a "(" each
// try the call part again, whose argument list walks its commas again at each level (the control
// "Operators Rule call part retried by nested prefix operators"), so the witness names that
// repetition, not a scan of a scan-only rule that a repeated evaluation reads from the records of
// its scan sites.
func TestOperatorsRetriedCallPartWitness(t *testing.T) {
	t.Parallel()
	c := classifyMain(t, "rule main = expr ws \"(\" [b,]* $$\n"+operatorsSpecExample)
	if c.Class != complexity.ClassUnknown || c.WitnessedDegree != 2 {
		t.Errorf("classified %v, witnessed degree %d, want unknown, witnessed 2", c.Class, c.WitnessedDegree)
	}
	found := false
	for _, cause := range c.Causes {
		if cause.Rule == "ident" || cause.Rule == "ws" {
			t.Errorf("a witness names a scan of the scan-only rule %s: %s", cause.Rule, cause.Message)
		}
		found = found || cause.Rule == "args" && strings.Contains(cause.Message, "again at each level")
	}
	if !found {
		t.Errorf("no witness names the repetition of args that each level evaluates again: %v", c.Causes)
	}
}

// TestOperatorsRetriedPartUnderLooserPrefix checks that the classifier keeps an Operators Rule
// unknown when a prefix operator binds more loosely than the right operand that it starts: the
// right operand of the prefix admits "*", tries it and its right operand, which scans the "-"
// that follow, and refuses it; the level of the prefix refuses "*" by its bound, but the "^"
// level around it does not, since its own right operand does not admit "*" by its binding
// level, so it parses the same right operand again, at every second level.
func TestOperatorsRetriedPartUnderLooserPrefix(t *testing.T) {
	t.Parallel()
	const src = "rule main = e $$\nrule e = #operators {\n    operand \"a\"\n    operand \"b\"\n    level low { prefix \"-\" }\n    level mid { infix left \"*\" }\n    level high { infix right \"^\" }\n}\n"
	c := classifyMain(t, src)
	if c.Class != complexity.ClassUnknown {
		t.Errorf("classified %v, want unknown", c.Class)
	}
	skipWithoutBudget(t)
	p, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	input := func(n int) string { return rep("a^-", n) + "b*" + rep("-", n) + ";" }
	for _, b := range Backends {
		s := Series{Sizes: double(100)}
		for _, n := range s.Sizes {
			cost, err := StepsLenient(p, input(n), b, Cap)
			if err != nil || cost.ParseErr == nil {
				t.Fatalf("%s: size %d: %v, parse error %v", BackendName(b), n, err, cost.ParseErr)
			}
			s.Costs = append(s.Costs, cost)
		}
		if got := Classify(s); got != Poly(2) {
			t.Errorf("%s: %v (slopes %.2f) classified %v, want Poly(2)", BackendName(b), s.Costs, s.Slopes(), got)
		}
	}
}
