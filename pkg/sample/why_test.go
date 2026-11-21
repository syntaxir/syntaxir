package sample_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/pkg/sample"
)

// Every missed target says what became of the attempts that tried it, with the cuts of those attempts.
func TestWhyAttempts(t *testing.T) {
	g, err := sample.New(engine.Reference(compile(t, `var d int
rule main = x y $$
rule x = !"a" [a] / "b"
rule y = {d := 0} {d > 0} "c"`)), sample.WithAttempts(2))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Next(); !errors.Is(err, sample.ErrNoInput) {
		t.Fatalf("got %v, want ErrNoInput", err)
	}
	c := g.Coverage()
	// Both attempts tried main, x and y, and both alternatives of x: they all share the cuts of the
	// two attempts.
	cuts := []sample.RuleCuts{{Rule: "x", Checks: 2}, {Rule: "y", Predicates: 2}}
	want := []sample.MissedRule{
		{Rule: "main", Why: sample.Why{Failed: 2, Cuts: cuts, MinLen: 2}},
		{Rule: "x", Why: sample.Why{Failed: 2, Cuts: cuts, MinLen: 2}},
		{Rule: "y", Why: sample.Why{Failed: 2, Cuts: cuts, MinLen: 2}},
	}
	if !reflect.DeepEqual(c.MissedRules, want) {
		t.Errorf("missed rules %+v,\nwant %+v", c.MissedRules, want)
	}
	if len(c.MissedAlternatives) != 2 || c.MissedAlternatives[0].Why.Failed != 2 || c.MissedAlternatives[1].Why.Failed != 2 {
		t.Errorf("missed alternatives %+v", c.MissedAlternatives)
	}
	line := "missed rule: y (tried in 2 attempts: 2 without a way to finish; most cuts in x (2 by checks, 0 by predicates), y (0 by checks, 2 by predicates); shortest input 2 bytes)\n"
	if r := c.Report(); !strings.Contains(r, line) {
		t.Errorf("report\n%s\nwants the line\n%s", r, line)
	}
}

// A candidate that the parser rejects counts against the targets of its derivation, and an accepted
// input clears nothing that it did not exercise.
func TestWhyRejected(t *testing.T) {
	// The last alternative is dead: "(" s ")" matches first. Only the parser finds out when the
	// nested s has no "!" check to tell.
	g, err := sample.New(engine.Reference(compile(t, `rule main = s $$
rule s = "(" s ")" / "x" / "(" s ")" "!"`)), sample.WithSeed(1), sample.WithCoverage())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Generate(10); err != nil {
		t.Fatal(err)
	}
	c := g.Coverage()
	if len(c.MissedRules) != 0 || len(c.MissedAlternatives) != 1 {
		t.Fatalf("coverage %s", c.Report())
	}
	w := c.MissedAlternatives[0].Why
	if c.MissedAlternatives[0].Index != 2 || w.Bypassed+w.Rejected+w.Failed == 0 || w.Exhausted > w.Failed || w.MinLen != 4 {
		t.Errorf("missed %+v", c.MissedAlternatives[0])
	}
}

// MinLen is the length of the shortest input through the target, from the structure of the grammar.
func TestWhyMinLen(t *testing.T) {
	g, err := sample.New(engine.Reference(compile(t, `rule main = "ab" x? ("c" / y / &"zz" z) e
rule x = "xyz"
rule y = "long" w #repeat(2,)
rule w = "er"
rule z = "zz"
rule e = #operators {
    operand "1"
    level { prefix "-"; infix left "+" }
    level { postfix "!" }
}`)))
	if err != nil {
		t.Fatal(err)
	}
	c := g.Coverage()
	got := map[string]int{}
	for _, m := range c.MissedRules {
		if !reflect.DeepEqual(m.Why, sample.Why{MinLen: m.Why.MinLen}) {
			t.Errorf("%s was tried before any attempt: %+v", m.Rule, m.Why)
		}
		got[m.Rule] = m.Why.MinLen
	}
	for _, m := range c.MissedAlternatives {
		got[m.Alternative.String()] = m.Why.MinLen
	}
	want := map[string]int{
		"main": 4, "x": 7, "y": 11, "w": 11, "z": 5, "e": 4,
		`main: choice 0: "c"`:     4,
		`main: choice 1: y`:       11,
		`main: choice 2: &"zz" z`: 5, // the lookahead's text is not counted twice
		`e: operand 0: "1"`:       4,
		`e: operator 0: "-"`:      5,
		`e: operator 1: "+"`:      6,
		`e: operator 2: "!"`:      5,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("minimal lengths %v,\nwant %v", got, want)
	}
	if r := c.Report(); !strings.Contains(r, "missed rule: x (not tried; shortest input 7 bytes)\n") {
		t.Errorf("report\n%s", r)
	}
}
