package examinedref_test

import (
	"reflect"
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
			if !reflect.DeepEqual(got, tt.want) {
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

// TestReferenceActionValues checks the values of Value Actions that predicates read: scalars, structs
// with unset fields, casts to struct types and joins, named node conversions, Operators Rule actions,
// and a runtime error of an action, which makes the parse fail.
func TestReferenceActionValues(t *testing.T) {
	const dyn = "type A = { N int }\ntype B = { S string }\ntype U ~ A | B\n" +
		"rule dyn _ = (\"x\" => A{N: 1}) / (\"y\" => B{S: \"s\"})\n"
	const sum = "rule expr int = #operators {\n operand d:[0-9] => len(text($d))\n" +
		" level { infix left \"+\" => $lhs + $rhs }\n level { prefix \"-\" => 0 - $rhs }\n}\n"
	tests := []struct {
		name, src, input string
		want             examinedref.Result
	}{
		{"a scalar value decides a predicate", "rule main = x:n { $x == 1 } \"a\"\nrule n int = [0-9] => 1", "1a",
			examinedref.Result{Matched: true, End: 2, Examined: 2}},
		{"a scalar value fails a predicate", "rule main = x:n { $x == 2 } \"a\"\nrule n int = [0-9] => 1", "1a",
			examinedref.Result{Matched: false, End: 0, Examined: 1}},
		{"an unset field reads as its zero value", "type P = { X int; Y string; Z []int }\n" +
			"rule main = p:q { $p.Y == \"\" && $p.X == 3 && len($p.Z) == 0 } \"a\"\nrule q P = [0-9] => P{X: 3}", "1a",
			examinedref.Result{Matched: true, End: 2, Examined: 2}},
		{"a cast to a struct type admits its values", dyn + "rule main = d:dyn { $d.(A).N == 1 } \"a\" / \"y\" \"b\"", "xa",
			examinedref.Result{Matched: true, End: 2, Examined: 2}},
		{"a failed cast fails the predicate", dyn + "rule main = d:dyn { $d.(A).N == 1 } \"a\" / \"y\" \"b\"", "yb",
			examinedref.Result{Matched: true, End: 2, Examined: 2}},
		{"a cast to an alias of a join admits its members", dyn + "rule main = d:dyn { $d.(U) != nil } \"a\"", "ya",
			examinedref.Result{Matched: true, End: 2, Examined: 2}},
		{"a conversion names the node", "type N = node[string]\nrule main = x:n { $x.(N) != nil } \"a\"\n" +
			"rule n = t:[0-9] => N{$t}", "1a", examinedref.Result{Matched: true, End: 2, Examined: 2}},
		{"an Operators Rule action reads its operands", "rule main = e:expr { $e == 3 } \"!\"\n" + sum, "1+1+1!",
			examinedref.Result{Matched: true, End: 6, Examined: 6}},
		{"a prefix action reads its operand", "rule main = e:expr { $e == -1 } \"!\"\n" + sum, "-1!",
			examinedref.Result{Matched: true, End: 3, Examined: 3}},
		{"an Operators Rule's own action reads the folded value", "rule main = e:ex { $e == 4 } \"!\"\n" +
			"rule ex int = #operators {\n operand [0-9] => 2\n level { infix left \"+\" => $lhs + $rhs }\n} => $value.(int)", "1+1!",
			examinedref.Result{Matched: true, End: 4, Examined: 4}},
		{"a lambda reads a capture of a repetition element", "rule main = rs:(r:[a-z] \";\")* { map($rs, (e) => e.$r)[1].Start == 2 } $$", "a;b;",
			examinedref.Result{Matched: true, End: 4, Examined: 5}},
		{"an iteration that makes no capture is still a tuple", "rule main = xs:(n:\"a\" / -\"b\")* { map($xs, (e) => e.$n)[1] == nil } $$", "ab",
			examinedref.Result{Matched: true, End: 2, Examined: 3}},
		{"a runtime error of an action fails the parse", "var z int\nrule main = n \"a\"\nrule n int = [0-9] => 1 / z", "1a",
			examinedref.Result{Matched: true, End: 2, Examined: 2, ActionErrors: []int{0}}},
		{"an action fails on an abandoned path", "var z int\nrule main = n \"b\" / [0-9] \"a\"\nrule n int = [0-9] => 1 / z", "1a",
			examinedref.Result{Matched: true, End: 2, Examined: 2, ActionErrors: []int{0}}},
		{"an action fails inside a lookahead", "var z int\nrule main = &n [0-9] \"a\"\nrule n int = [0-9] => 1 / z", "1a",
			examinedref.Result{Matched: true, End: 2, Examined: 2, ActionErrors: []int{0}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, err := enginetest.ParseGrammar(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, _, err := examinedref.Evaluate(g, "main", tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestReferenceNoStartValue checks that a parse that builds no value of the start rule at its top
// call, as a stream parse does, meets no error of the start rule's action there, while a nested call
// of the start rule still runs it; and that the reference refuses the option for a leader.
func TestReferenceNoStartValue(t *testing.T) {
	g, err := enginetest.ParseGrammar("rule main = (\"(\" main \")\")? xs:(([a-z] \";\")*) => 10 / (len($xs) - 2)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, tc := range []struct {
		input       string
		every, none []int
	}{
		{"a;a;", []int{0}, nil},
		{"(a;)a;a;", []int{1}, nil},
		{"(a;a;)a;", []int{0}, []int{0}},
		{"a;", nil, nil},
	} {
		for _, noStart := range []bool{false, true} {
			want := tc.every
			if noStart {
				want = tc.none
			}
			got, _, err := examinedref.EvaluateWith(g, "main", tc.input, examinedref.Options{NoStartValue: noStart})
			if err != nil {
				t.Fatal(err)
			}
			if !got.Matched || got.End != len(tc.input) || !slices.Equal(got.ActionErrors, want) {
				t.Errorf("%q NoStartValue=%v: got %+v, want a match of the input with action errors %v", tc.input, noStart, got, want)
			}
		}
	}
	lr, err := enginetest.ParseGrammar("rule main = main \"a\" / \"a\"")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, _, err := examinedref.EvaluateWith(lr, "main", "aa", examinedref.Options{NoStartValue: true}); err == nil {
		t.Error("a leader that builds no value at its top call: got no error")
	}
}

// TestReferenceMarksGrowthReads checks that the exits of calls that read a growth in progress are
// marked, so that a skipped call is not justified by the failing seed of a leader.
func TestReferenceMarksGrowthReads(t *testing.T) {
	g, err := enginetest.ParseGrammar("rule main = e $$\nrule e = e \"+\" n / n\nrule n = [0-9]")
	if err != nil {
		t.Fatal(err)
	}
	_, exits, err := examinedref.Evaluate(g, "main", "1+2")
	if err != nil {
		t.Fatal(err)
	}
	var reads, failedReads int
	var last examinedref.Exit
	for _, x := range exits {
		if x.Growing {
			if x.Rule != "e" || x.Pos != 0 {
				t.Fatalf("a read of a growth by %+v", x)
			}
			reads++
			if !x.Matched {
				failedReads++
			}
		}
		if x.Rule == "e" && x.Pos == 0 {
			last = x
		}
	}
	// Three steps read the seed, "1" and "1+2"; the last does not grow.
	if reads != 3 || failedReads != 1 || last.Growing || !last.Matched || last.End != 3 {
		t.Fatalf("%d reads, %d failing, last exit of e %+v; exits %+v", reads, failedReads, last, exits)
	}
}
