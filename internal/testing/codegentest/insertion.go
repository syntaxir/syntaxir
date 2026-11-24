package codegentest

import "strings"

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
