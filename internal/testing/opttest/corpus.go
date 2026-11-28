// Package opttest holds the corpus of the backend-claims test of the optimization registry: the
// grammars on which a flag's witnesses (pass.Flag.Witnesses) show that it changes the parsers of the
// backends it lists, and on which the long tier shows that it changes no other backend's parser.
package opttest

import (
	"fmt"
	"strings"

	"syntaxir.dev/sxir/internal/bench/textcompare"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// Corpus returns the cases of the backend-claims test: the cases of the flag parity corpus, and cases of
// the shapes that the other flags act on. The inputs of a case are those that the transcripts of the
// reference controls (internal/testing/reftest) parse where it is a witness.
func Corpus() []codegentest.Case {
	cases := codegentest.FlagParityCases()
	kw, kwIn := codegentest.OutlineKeywords()
	cases = append(cases,
		codegentest.Case{Name: "outlined keywords", Src: kw, Inputs: kwIn},
		codegentest.Case{Name: "class tables", Src: "rule main = ([a-cf-hk-mp-rt-vx-z0-35-7]+ / [^a-cf-hk-mp-rt-vx-z0-35-7 ]+ / -\" \")* $$\n"},
		codegentest.Case{Name: "terminals", Src: "rule main = (-([aA] [bB] / [cC] [dD] / [eE] [fF] / [gG] [hH] / [iI] [jJ] / [kK] [lL] / [mM] [nN] / [oO] [pP]) / -\" \")* $$\n"},
		// The operand lines of the two Operators Rules call one rule, so their generated Go helpers are equal.
		codegentest.Case{Name: "equal bodies", Src: "rule main = a \",\" b $$\nrule a = #operators {\n    operand n\n    level { infix left \"+\" }\n}\nrule b = #operators {\n    operand n\n    level { infix left \"*\" }\n}\nrule n = [0-9]+\n"},
		codegentest.Case{Name: "typed text fields", Src: textcompare.Grammar, Inputs: textcompare.Inputs},
		codegentest.Case{Name: "many memoized rules", Src: manyMemoizedRules(70), Inputs: []string{"", "k0!", "k0? k69ab! k35?", "k1x!k2!", "k70!", "k3ab?  k3ab!", "k12"}},
		codegentest.Case{Name: "capture frames", Src: captureFrames, Inputs: []string{"", "a=1;", "a=1+2*3; b=(1+2)*c,d,e;", "x=1;", "a=1+;b=2;", "a=((1);c=d;", "a=b,c", "日=1;", "a=1;\xff", "𝄞=1; a=b,c;"}},
	)
	return cases
}

// captureFrames is a grammar whose rules keep captures where a parse backtracks over them: in
// repetitions, left recursion, an Operators Rule, a predicate and recovery.
const captureFrames = `rule main = (s:stmt #recover(skip=(![;] .)* ";") -" "*)* $$
rule stmt = n:name "=" e:expr ";" {text($n) != "x"}
rule expr = #operators {
    operand atom
    level { infix left "+" }
    level { infix left "*" }
}
rule atom = d:[0-9]+ #atomic / "(" e:expr ")" / l:list
rule list = l:list "," x:name / x:name
rule name = [a-z]+ #atomic
`

// manyMemoizedRules returns a grammar of n rules that the parse memoizes, each called at two places, so
// that the first-call table of its parser has more entries than a dense table holds.
func manyMemoizedRules(n int) string {
	var alts, rules []string
	for i := range n {
		alts = append(alts, fmt.Sprintf("r%d \"!\" / r%d \"?\"", i, i))
		rules = append(rules, fmt.Sprintf("rule r%d = \"k%d\" w\n", i, i))
	}
	return "rule main = (item / -\" \")* $$\nrule item = " + strings.Join(alts, " / ") + "\nrule w = [a-z]*\n" + strings.Join(rules, "")
}
