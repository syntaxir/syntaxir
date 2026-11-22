package examinedref_test

import (
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/examinedref"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// The expectations below are worked out by hand from the specification's definitions of the examined
// range (one past the greatest slot examined by the call or a call within it, at least its start), so
// they check the reference without the engine.
func TestReferenceExaminedByHand(t *testing.T) {
	tests := []struct {
		name, src, input string
		opts             examinedref.Options
		want             examinedref.Result
		// exits, when set, must be among the exits of the evaluation.
		exits []examinedref.Exit
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
			// spec/language/parser-expressions.md (Cut): a cut inside &a commits only choices inside that
			// expression, so the lookahead fails as for any other failure and the choice tries its next
			// alternative.
			name:  "a cut inside a positive lookahead does not commit the choice around it",
			src:   `rule main = (&("a" #cut "b") "a" "c") / "a" "d"`,
			input: "ad",
			want:  examinedref.Result{Matched: true, End: 2, Examined: 2},
		},
		{
			// The same for !a: its expression fails after the cut, so the negative lookahead succeeds.
			name:  "a cut inside a negative lookahead does not commit the choice around it",
			src:   `rule main = (!("a" #cut "b") "a" "c") / "a" "d"`,
			input: "ad",
			want:  examinedref.Result{Matched: true, End: 2, Examined: 2},
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
		{
			name:  "a recovery skips what its skip matches and examines what both read",
			src:   `rule main = "a" #recover(skip=[b]+)`,
			input: "bbc",
			want:  examinedref.Result{Matched: true, End: 2, Examined: 3},
		},
		{
			name:  "a recovery whose skip consumes nothing fails",
			src:   `rule main = "a" #recover(skip=[b]*)`,
			input: "c",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 1},
		},
		{
			name:  "bottom fails without examining and the next alternative matches",
			src:   `rule main = _|_ / "a"`,
			input: "a",
			want:  examinedref.Result{Matched: true, End: 1, Examined: 1},
		},
		{
			name:  "top matches without examining",
			src:   `rule main = _ "a"`,
			input: "a",
			want:  examinedref.Result{Matched: true, End: 1, Examined: 1},
		},
		{
			name:  "ignorecase matches the letters in either case",
			src:   `rule main = "ab" #ignorecase`,
			input: "AB",
			want:  examinedref.Result{Matched: true, End: 2, Examined: 2},
		},
		{
			// The predicate reads the capture and examines nothing: "2" is rejected after "1" matched.
			name:  "a predicate over a capture decides the match and examines nothing",
			src:   "rule main = x:[0-9] { text($x) == \"1\" } \"a\"",
			input: "2a",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 1},
		},
		{
			name:  "a binding is visible to the predicates after it",
			src:   "var n int\nrule main = { n := 2 } b\nrule b = { n == 2 } \"a\"",
			input: "a",
			want:  examinedref.Result{Matched: true, End: 1, Examined: 1},
		},
		{
			// After 3 the tail tries "+" and "*" at the end slot 5; $$ examines slot 5 as well.
			name:  "an Operators Rule examines the operator parts it tries after the last operand",
			src:   "rule main = e $$\nrule e = #operators {\n operand [0-9]\n level add { infix left \"+\" }\n level mul { infix left \"*\" }\n}",
			input: "1+2*3",
			want:  examinedref.Result{Matched: true, End: 5, Examined: 6},
		},
		{
			// level(e, mul) applies the operators of mul and tighter ones, so the selected "+" ends the
			// call after 1, having examined it; the call reports level 1, the level below mul.
			name:  "a level-restricted call ends before a looser operator it has examined",
			src:   "rule main = level(e, mul) \"+\" [0-9]\nrule e = #operators {\n operand [0-9]\n level add { infix left \"+\" }\n level mul { infix left \"*\" }\n}",
			input: "1+2",
			want:  examinedref.Result{Matched: true, End: 3, Examined: 3},
			exits: []examinedref.Exit{{Rule: "e", Level: 1, Pos: 0, End: 1, Examined: 2, Matched: true}},
		},
		{
			name:  "a missing token is inserted where its literal fails",
			src:   `rule main = "a" "b" #missing "c"`,
			input: "ac",
			opts:  examinedref.Options{MissingTokens: true},
			want:  examinedref.Result{Matched: true, End: 2, Examined: 2},
		},
		{
			name:  "without token insertion the same literal fails",
			src:   `rule main = "a" "b" #missing "c"`,
			input: "ac",
			want:  examinedref.Result{Matched: false, End: 0, Examined: 2},
		},
		{
			name:  "no token is inserted in a lookahead",
			src:   `rule main = &("b" #missing) "a"`,
			input: "a",
			opts:  examinedref.Options{MissingTokens: true},
			want:  examinedref.Result{Matched: false, End: 0, Examined: 1},
		},
		{
			// In bytes, decoding reads E2 82 and rejects "A" as the third byte: an invalid byte of one
			// position whose decoding examined three slots.
			name:  "a truncated sequence in bytes examines the byte that invalidates it",
			src:   `rule main = .`,
			input: "\xe2\x82A",
			opts:  examinedref.Options{Unit: input.Bytes},
			want:  examinedref.Result{Matched: true, End: 1, Examined: 3},
		},
		{
			// The first byte of "é" already differs from "b": the bytes after it are not examined.
			name:  "a literal rejected in bytes examines the first byte that differs",
			src:   `rule main = "b"`,
			input: "é",
			opts:  examinedref.Options{Unit: input.Bytes},
			want:  examinedref.Result{Matched: false, End: 0, Examined: 1},
		},
		{
			name:  "a literal cut short in bytes examines the end slot",
			src:   `rule main = "éb"`,
			input: "é",
			opts:  examinedref.Options{Unit: input.Bytes},
			want:  examinedref.Result{Matched: false, End: 0, Examined: 3},
		},
		{
			name:  "a replacement character in a literal matches one invalid byte",
			src:   "rule main = \"\uFFFD\" \"a\"",
			input: "\xffa",
			opts:  examinedref.Options{Unit: input.Bytes},
			want:  examinedref.Result{Matched: true, End: 2, Examined: 2},
		},
		{
			// The predicate reads a struct that it constructs itself, not the value of an action.
			name:  "a predicate constructs a struct",
			src:   "type P = { X int }\nrule main = { P{X: 1} != nil } \"a\"",
			input: "a",
			want:  examinedref.Result{Matched: true, End: 1, Examined: 1},
		},
		{
			name:  "the same input in code points is three characters",
			src:   `rule main = .`,
			input: "\xe2\x82A",
			want:  examinedref.Result{Matched: true, End: 1, Examined: 1},
		},
		{
			name:  "an inline action is a call of its hidden rule",
			src:   "rule main = (\"a\" => 1) \"b\"",
			input: "ab",
			want:  examinedref.Result{Matched: true, End: 2, Examined: 2},
			exits: []examinedref.Exit{{Rule: "_main_1", Pos: 0, End: 1, Examined: 1, Matched: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := enginetest.ParseGrammar(tt.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, exits, err := examinedref.EvaluateWith(g, "main", tt.input, tt.opts)
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for _, x := range tt.exits {
				if !slices.Contains(exits, x) {
					t.Errorf("no exit %+v among %+v", x, exits)
				}
			}
		})
	}
}

// TestReferenceRejectsActionValues checks that a predicate reading the value of a user action makes
// the input fall outside the reference, naming the rule.
func TestReferenceRejectsActionValues(t *testing.T) {
	g, err := enginetest.ParseGrammar("rule main = x:n { $x == 1 } \"a\"\nrule n int = [0-9] => 1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, _, err = examinedref.Evaluate(g, "main", "1a")
	uns, ok := err.(*examinedref.UnsupportedError)
	if !ok {
		t.Fatalf("got %v (%T), want *UnsupportedError", err, err)
	}
	if want := "a predicate that reads the value of the action of rule n"; uns.Construct != want {
		t.Fatalf("construct %q, want %q", uns.Construct, want)
	}
}
