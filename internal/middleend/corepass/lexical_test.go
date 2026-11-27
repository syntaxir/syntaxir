package corepass_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// The lexical rules of a grammar (Rule.Lexical) are those that only match characters: every form
// that matches characters, and a capture, which only names a part of the value, makes a rule
// lexical, and an action, a cycle of calls or a call of a rule that is not lexical does not. The value-free twin of a transient lexical rule is a
// value-less lexical rule, which no parse memoizes; a twin of a rule that is not lexical is not.
func TestLexicalRules(t *testing.T) {
	const src = `
rule main = chars marks group atomic action calls loop capture -chars -calls
rule chars = "x" [a-z]* . "y"?
rule marks = ^ $ ^^ _ "m" $$
rule group = &"y" !"z" -"w" ("p" / "q")+
rule atomic = ("r" chars) #atomic
rule action = x:"d" => $x
rule calls = "e" action
rule loop = "f" loop?
rule capture = x:"g"
rule bottom = _|_
`
	prog := compiletest.Program(t, src, check.Options{})
	want := map[string]bool{
		"main": false, "chars": true, "marks": true, "group": true, "atomic": true,
		"action": false, "calls": false, "loop": false, "capture": true, "bottom": true,
	}
	rt := prog.Runtime()
	for name, lexical := range want {
		r := rt.ByName[name]
		if r == nil {
			t.Fatalf("no rule %s", name)
		}
		if r.Lexical != lexical {
			t.Errorf("rule %s: Lexical %v, want %v", name, r.Lexical, lexical)
		}
		if r.ValuelessLexical() {
			t.Errorf("rule %s, which builds a value, is a value-less lexical rule", name)
		}
	}
	twins := map[string]bool{}
	for _, r := range rt.Twins {
		twins[r.Name] = r.ValuelessLexical()
	}
	for name, valueless := range map[string]bool{"chars": true, "calls": false} {
		got, ok := twins[name]
		if !ok {
			t.Fatalf("rule %s has no value-free twin; twins %v", name, twins)
		}
		if got != valueless {
			t.Errorf("twin of %s: ValuelessLexical %v, want %v", name, got, valueless)
		}
	}
}
