package codegentest

import (
	"strings"
	"unicode/utf8"
)

// InsertionCorpus returns grammars that use #missing, with inputs on which their parses insert
// tokens, fail at the limit or need no insertion: the examined shapes that use #missing, the action
// cases over missing tokens and the shapes of the engine's missing-token tests (literals, rollback
// and cuts, lookaheads, repetitions, twins, left recursion, Operators Rule candidates, labels with
// consuming recovery, predicates and memo dependencies, retained results).
func InsertionCorpus() []Case {
	var out []Case
	for _, c := range ExaminedShapes() {
		if strings.Contains(c.Src, "#missing") {
			out = append(out, c)
		}
	}
	for _, cases := range [][]ActionCase{MissingActionCases(), MissingFieldActionCases(), RecoveredStructActionCases(true)} {
		for _, c := range cases {
			out = append(out, c.Case)
		}
	}
	token := "\nrule token = \";\" #missing"
	ops := func(operand, level string) string {
		return "rule main = e $$\nrule e = #operators {\n    operand " + operand + "\n    level { " + level + " }\n}"
	}
	out = append(out, NullableOperandInsertionCases()...)
	return append(out,
		Case{Name: "a literal after a multibyte character", Src: `rule main = "é" ";" #missing $$`, Inputs: []string{"é", "é;", "é;;", "", "\xc3"}},
		Case{Name: "a partial literal", Src: `rule main = "ab" #missing "a" $$`, Inputs: []string{"a", "aba", "ab", ""}},
		Case{Name: "invalid UTF-8 before a literal", Src: `rule main = . ";" #missing $$`, Inputs: []string{"\xff", "\xff;", "a"}},
		Case{Name: "an insertion rolled back", Src: "rule main = (token \"x\" / token \"y\") $$" + token, Inputs: []string{"x", "y", ";y", "", "z"}},
		Case{Name: "captures of insertions", Src: `rule main = (x:(";" #missing) "a" / ";" #missing "b") $$`, Inputs: []string{"a", "b", ";a", ";b", ""}},
		Case{Name: "a cut after an insertion", Src: `rule main = (";" #missing #cut "x" / "y") $$`, Inputs: []string{"x", "y", ";x", ""}},
		Case{Name: "a chain of insertions", Src: `rule main = ";" #missing ")" #missing $$`, Inputs: []string{"", ";", ")", ";)", "x"}},
		Case{Name: "a negative lookahead of an insertion", Src: "rule main = !token token $$" + token, Inputs: []string{"", ";", "x"}},
		Case{Name: "a positive lookahead of an insertion", Src: "rule main = (&token \"x\" / token) $$" + token, Inputs: []string{"", "x", ";x"}},
		Case{Name: "an insertion before a lookahead", Src: "rule main = (token &token / token) $$" + token, Inputs: []string{"", ";", ";;"}},
		Case{Name: "a repetition of insertions", Src: `rule main = (";" #missing)* $$`, Inputs: []string{"", ";;", "x"}},
		Case{Name: "a nonempty repetition of insertions", Src: `rule main = (";" #missing)+ $$`, Inputs: []string{"", ";", "x"}},
		Case{Name: "nested repetitions of insertions", Src: `rule main = ((";" #missing)*)* $$`, Inputs: []string{"", ";", "x"}},
		Case{Name: "a counted repetition of insertions", Src: `rule main = (";" #missing) #repeat(2) $$`, Inputs: []string{"", ";", ";;"}},
		Case{Name: "twins of an inserting rule", Src: "rule main = token #atomic token $$" + token, Inputs: []string{"", ";", ";;"}},
		Case{Name: "an insertion seeding left recursion", Src: "rule main = e $$\nrule e = e \"x\" / \";\" #missing", Inputs: []string{"", "x", "xx", ";x"}},
		Case{Name: "an insertion seeding left recursion under a lookahead", Src: "rule main = e $$\nrule e = e !e \"x\" / \";\" #missing", Inputs: []string{"", "x", "xx"}},
		Case{Name: "an insertion seeding indirect left recursion", Src: "rule main = e $$\nrule e = other \"x\" / \";\" #missing\nrule other = e", Inputs: []string{"", "x", "xx"}},
		Case{Name: "an insertion in a growth", Src: "rule main = e $$\nrule e = e \";\" #missing / \"x\"", Inputs: []string{"x", "x;", ""}},
		Case{Name: "an inserted infix operator", Src: ops(`"a"`, `infix left "+" #missing`), Inputs: []string{"aa", "a+a", "a", "aaa"}},
		Case{Name: "an inserted postfix operator", Src: ops(`"a"`, `postfix "!" #missing`), Inputs: []string{"a", "a!", ""}},
		Case{Name: "an inserted infix operator over an empty operand", Src: ops(`""`, `infix left "+" #missing`), Inputs: []string{"", "+"}},
		Case{Name: "a label outside an insertion", Src: `rule main = ";" @error("semicolon") #missing $$`, Inputs: []string{"", ";", "x"}},
		Case{Name: "a label inside an insertion", Src: `rule main = ";" #missing @error("semicolon") $$`, Inputs: []string{"", ";", "x"}},
		Case{Name: "nested labels of an insertion", Src: `rule main = (";" #missing @error("inner")) @error("semicolon") $$`, Inputs: []string{"", ";", "x"}},
		Case{Name: "an insertion under consuming recovery", Src: `rule main = (";" #missing) #recover(skip="!") @error("semicolon") $$`, Inputs: []string{"", ";", "!", "x"}},
		Case{Name: "a predicate over an insertion", Src: "rule main = x:(token #atomic) {len($x) == 0} $$" + token, Inputs: []string{"", ";"}},
		Case{Name: "memo dependencies on an insertion", Src: "rule main = (outer \"x\" / outer \"y\") $$\nrule outer = inner\nrule inner = \";\" #missing", Inputs: []string{"y", "x", ";y", ""}},
		Case{Name: "an action after an insertion", Src: "type R = { N int }\nrule main = \";\" #missing => R{N: 1/0}", Inputs: []string{"", ";"}},
		Case{Name: "right recursion through an insertion", Src: "rule main = e $$\nrule e = \";\" #missing e / \"x\"", Inputs: []string{"x", ";x", ""}},
		Case{Name: "retained results with insertions", Src: "rule main = e $$\nrule e = \";\" #missing e / \"x\" \";\" #missing", Inputs: []string{"x", "x;", ";x", ""}},
		Case{Name: "a missing field", Src: "type P = { M missing }\nrule main = \"a\" m:(\";\" #missing) $$ => P{M: $m.(_).(missing)}", Inputs: []string{"a", "a;"}},
	)
}

// IncrementalInsertionCorpus is InsertionCorpus with cases whose repetitions a Document resumes and a
// stream commits: elements that insert, and elements whose insertion backtracks, which takes an
// attempt of the limit and records no error.
func IncrementalInsertionCorpus() []Case {
	item := func(body string) string { return "rule main = item* $$\nrule item = " + body }
	return append(InsertionCorpus(),
		Case{Name: "elements that insert", Src: item(`"a" ";" #missing`),
			Inputs: []string{"a;a;a;", "a;aa;a", "aaa", "a;a;a;a;a", "a"}},
		Case{Name: "elements whose insertion backtracks", Src: item(`"a" ";" #missing "!" / "a" "?"`),
			Inputs: []string{"a?", "a?a?a?", "a;!a?", "a?a?a?a?a?", "a!a?"}},
	)
}

// InsertionCases returns grammars with #missing and inputs on which a parse inserts missing tokens: in
// sequences, repetitions, memoized and left-recursive rules, Operators Rules, lookaheads, @error labels
// and predicates over the missing value, and inputs that need more insertions than a small limit.
func InsertionCases() []Case {
	return []Case{
		{Name: "token insertion", Src: "rule main = \"(\" [a-z]* \")\" #missing \";\" #missing $$",
			Inputs: []string{"(a);", "(a;", "(a)", "(a", "(", "a", ""}},
		{Name: "token insertion in left recursion", Src: "rule main = e $$\nrule e = e \"+\" #missing n / n\nrule n = [0-9]",
			Inputs: []string{"1+2", "12", "123", "1+23", "1 2", "1"}},
		{Name: "insertion in a repetition", Src: "rule main = item* $$\nrule item = word sep\nrule word = \"a\"+ #atomic\nrule sep = \";\" #missing",
			Inputs: []string{"a;a;", "aa", "a;a", "a a", "a;;", ""}},
		{Name: "insertion past the limit", Src: "rule main = (x \";\" #missing)* $$\nrule x = [a-z]",
			Inputs: []string{"a;b;c;", "abc", "ab", "abcdef"}},
		{Name: "memoized rules that insert", Src: "rule main = a \"x\" / a \"y\" / a \"z\" $$\nrule a = \"(\" b \")\" #missing\nrule b = [a-z]* / \"[\" a \"]\" #missing",
			Inputs: []string{"(b)x", "(bz", "([(b)]y", "([(b]z", "(y", "x"}},
		{Name: "lookaheads with token insertion", Src: "rule main = !(\"x\" #missing) \"y\" #missing &(\"z\" #missing) $$",
			Inputs: []string{"", "y", "z", "yz", "x"}},
		{Name: "insertion in an @error label", Src: "rule main = (\"a\" #missing \"b\") @error(\"m1\") (\"c\" #missing) $$",
			Inputs: []string{"abc", "b", "bc", "ab", ""}},
		{Name: "the message of an inserted token",
			Src:    "rule main = [a\\né]* x:(\";\" #missing) { $x.(missing).message == \"2:2: syntax error\" } \"b\" $$",
			Inputs: []string{"\nab", "a\nab", "ab", "\néb", "\n;b"}},
		{Name: "insertion in an Operators Rule", Src: "rule main = e $$\nrule e = #operators {\n operand [0-9]+ #atomic\n operand \"(\" e \")\" #missing\n" +
			" level add { infix left \"+\" }\n level post { postfix \"[\" e \"]\" #missing }\n}",
			Inputs: []string{"1+2", "(1+2", "1[2", "1[2+(3", "(", "1+"}},
		{Name: "insertion in a recovery", Src: "rule main = stmt* $$\nrule stmt = (\"let\" \" \" [a-z]+ \";\" #missing) #recover(skip=[^;]* \";\")",
			Inputs: []string{"let a;", "let a", "let alet b;", "lex a;let b", ";;"}},
		{Name: "left recursion only through an insertion", Src: "rule main = a $$\nrule a = \"(\" #missing a \"x\" / \"y\"",
			Inputs: []string{"y", "(yx", "yx", "yxx", "x", ""}},
	}
}

// TypedInsertionCases returns typed grammars with #missing: missing values in struct fields of the types
// missing, node[string] | missing and error, in optional fields and lists, and in joins, an alias of a
// node and missing and one of a declared struct and missing, through left recursion and memoized rules.
func TypedInsertionCases() []Case {
	return []Case{
		{Name: "missing values in fields", Src: `type Pair = { A node[string]; Semi node[string] | missing; B *(node[string] | missing); E *(node[string] | error); L [](node[string] | missing); M node[string] | missing; R *(node[string] | error); H missing; G error }
@start
rule main Pair = a:"x" s:semi b:item e:bad l:list m:only r:rec h:hole g:gap $$ => Pair{A: $a, Semi: $s, B: $b, E: $e, L: $l.Value, M: $m, R: $r, H: $h, G: $g}
rule semi = ";" #missing
rule item = m:"z" #missing => $m
rule bad = "q" #recover(skip=[^!]*) / x:"!" #missing => $x
rule list = ("," #missing)*
rule only = k:"k" #missing => $k
rule rec = "w" #recover(skip=[^v]*)
rule hole missing = m:"]" #missing => $m.(missing)
rule gap missing = m:")" #missing => $m.(missing)`,
			Inputs: []string{"x;z!,,kw", "x", "xz", "x;zq!", "xq", "x;z!,k", "xwv", "x]", "x]]", "y", "x;z!,kw", "x!w", "x;z!,,kwv"}},
		{Name: "missing values in joins", Src: `type Num = { V node[string] }
type Tok ~ node[string] | missing
type Val ~ Num | missing
type Out = { T Tok; V Val; Vs []Val; E Tok }
@start
rule main Out = t:tok v:val vs:val* e:expr $$ => Out{T: $t, V: $v, Vs: $vs.Value, E: $e}
rule tok Tok = x:";" #missing => $x
rule val Val = num / hole
rule num Num = n:[0-9] => Num{V: $n}
rule hole missing = m:"]" #missing => $m.(missing)
rule expr Tok = sum / atom
rule sum Tok = l:expr "+" r:atom => $r
rule atom Tok = x:"(" #missing => $x`,
			Inputs: []string{";12(", "", "1", ";1+", "(+(+", "+", ";]]", "12+("}},
		{Name: "the fields of a missing value", Src: `type M = { E string; M string; S int; N int }
@start
rule main []M = xs:(x:("," #missing) => M{E: $x.(missing).expected, M: $x.(missing).message, S: $x.Start, N: $x.End})* "a" $$ => $xs.Value`,
			Inputs: []string{"a", "b"}},
	}
}

// TypedInsertionCorpus holds the cases of the typed parsers that insert missing tokens: the typed
// insertion cases, the insertion cases and the shared insertion corpus.
func TypedInsertionCorpus() []Case {
	return append(append(TypedInsertionCases(), InsertionCases()...), InsertionCorpus()...)
}

// MinimalEdit returns the edit, in bytes or code points, that replaces the characters
// between the common prefix and the common suffix of from and to. Every invalid UTF-8 byte is a
// character.
func MinimalEdit(from, to string, bytes bool) (start, end int, text string) {
	chars := func(s string) []string {
		var out []string
		for s != "" {
			_, n := utf8.DecodeRuneInString(s)
			out, s = append(out, s[:n]), s[n:]
		}
		return out
	}
	a, b := chars(from), chars(to)
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	pos := func(chars []string) int {
		if bytes {
			return len(strings.Join(chars, ""))
		}
		return len(chars)
	}
	return pos(a[:p]), pos(a[:len(a)-s]), strings.Join(b[p:len(b)-s], "")
}
