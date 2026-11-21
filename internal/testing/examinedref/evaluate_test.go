package examinedref_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/examinedref"
)

// The expectations below are worked out by hand from the specification's definitions of the examined
// range (one past the greatest slot examined by the call or a call within it, at least its start), so
// they check the reference without the engine.
func TestReferenceExaminedByHand(t *testing.T) {
	tests := []struct {
		name, src, input string
		want             examinedref.Result
	}{
		{
			name:  "literal rejected at its second character examines two slots",
			src:   `rule main = "ab"`,
			input: "ac",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 2},
		},
		{
			name:  "literal cut short by the end of the input examines the end slot",
			src:   `rule main = "ab"`,
			input: "a",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 2},
		},
		{
			name:  "failed alternatives count",
			src:   `rule main = "ab" / "c"`,
			input: "x",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 1},
		},
		{
			name:  "a successful choice keeps the examined end of its failed alternatives",
			src:   `rule main = "x" / "a"`,
			input: "a",
			want:  examinedref.Result{Matched: true, End: 1, Examined: 1},
		},
		{
			name:  "end of input examines the slot it tests",
			src:   `rule main = "a" $$`,
			input: "ab",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 2},
		},
		{
			name:  "a failure after a cut fails the choice, and the examined end is that of the failed literal",
			src:   `rule main = "a" #cut "b" / "a" "c"`,
			input: "ac",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 2},
		},
		{
			name:  "without the cut the second alternative matches the same input",
			src:   `rule main = "a" "b" / "a" "c"`,
			input: "ac",
			want:  examinedref.Result{Matched: true, End: 2, Examined: 2},
		},
		{
			name:  "a cut that the input passes leaves the choice matched",
			src:   `rule main = "a" #cut "b" / "a" "c"`,
			input: "ab",
			want:  examinedref.Result{Matched: true, End: 2, Examined: 2},
		},
		{
			name:  "a failure after a cut with no enclosing choice fails the rule as an ordinary failure",
			src:   `rule main = "a" #cut "b"`,
			input: "ac",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 2},
		},
		{
			name:  "an optional that fails after a cut fails instead of stopping",
			src:   `rule main = ("a" #cut "b")? "a" "c"`,
			input: "ac",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 2},
		},
		{
			name:  "without the cut the optional stops and the rest matches",
			src:   `rule main = ("a" "b")? "a" "c"`,
			input: "ac",
			want:  examinedref.Result{Matched: true, End: 2, Examined: 2},
		},
		{
			name:  "a repetition whose iteration fails after a cut fails instead of stopping",
			src:   `rule main = ("a" #cut "b")* "a" "c"`,
			input: "ac",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 2},
		},
		{
			name:  "a cut does not escape its rule: the calling choice tries its next alternative",
			src:   "rule main = s \"d\" / \"a\" \"d\"\nrule s = \"a\" #cut \"b\"",
			input: "ad",
			want:  examinedref.Result{Matched: true, End: 2, Examined: 2},
		},
		{
			name:  "end of line before a line feed matches and examines the slot it tests",
			src:   "rule main = \"a\" $",
			input: "a\nb",
			want:  examinedref.Result{Matched: true, End: 1, Examined: 2},
		},
		{
			name:  "end of line before a letter fails and examines that letter",
			src:   "rule main = \"a\" $",
			input: "ab",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 2},
		},
		{
			name:  "beginning of line after a line feed examines the slot before it",
			src:   "rule main = \"a\" \"\\n\" ^ \"b\"",
			input: "a\nb",
			want:  examinedref.Result{Matched: true, End: 3, Examined: 3},
		},
		{
			name:  "beginning of line after a letter fails and examines the letter before it",
			src:   "rule main = \"a\" ^ \"b\"",
			input: "ab",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 1},
		},
		{
			name:  "positive lookahead examines what it reads and fails with it",
			src:   `rule main = &"abc" "a"`,
			input: "abd",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 3},
		},
		{
			name:  "negative lookahead that matches fails and keeps what it read",
			src:   `rule main = !"x" "a"`,
			input: "xa",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 1},
		},
		{
			name:  "optional that matches and a literal that is then rejected examine both",
			src:   `rule main = "a"? "b"`,
			input: "ac",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 2},
		},
		{
			name:  "repetition stops at the first rejected iteration and examines it",
			src:   `rule main = [0-9]+`,
			input: "12x",
			want:  examinedref.Result{Matched: true, End: 2, Examined: 3},
		},
		{
			// Growth of e at 0: the first step reads the seed failure (examined 0) and n, which
			// examines to 2; the second step reads the seed "1" (examined 2), then "+" and "2" (examined
			// 4, slot 3 being the end); the third step reads the seed "1+2" and rejects the "+" at slot 3,
			// so the call examines to 4. $$ at 3 examines slot 3 and does not extend it.
			name:  "left recursion grows to the end of the input and keeps the examined end of its steps",
			src:   "rule main = e $$\nrule e = e \"+\" n / n\nrule n = [0-9]+",
			input: "1+2",
			want:  examinedref.Result{Matched: true, End: 3, Examined: 4},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := enginetest.ParseGrammar(tt.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, _, err := examinedref.Evaluate(g, "main", tt.input)
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReferenceRejectsOutsideFragment(t *testing.T) {
	g, err := enginetest.ParseGrammar("rule main = \"a\" #recover(skip=[b]+)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, _, err := examinedref.Evaluate(g, "main", "a"); err == nil {
		t.Fatal("recovery is outside the reference fragment and must be refused")
	} else if _, ok := err.(*examinedref.UnsupportedError); !ok {
		t.Fatalf("got %T, want *UnsupportedError", err)
	}
}
