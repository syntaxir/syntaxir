package complexity

import (
	"fmt"
	"strings"
	"testing"
)

// TestClassifyLoosestLevelCall checks that a call that names the loosest level of an Operators
// Rule classifies as the call that names no level: both apply every operator (core.Call.Level 0),
// so the classifier tells them apart nowhere, as a left call of a cycle, as a call that passes
// before a left call, or as a call in a repetition.
func TestClassifyLoosestLevelCall(t *testing.T) {
	const ops = "\nrule e = #operators {\n    operand s / \"a\"\n    level sum { infix left \"+\" }\n    level prod { infix left \"*\" }\n}\n"
	for _, tc := range []struct{ name, src string }{
		{"left call of a cycle", "rule s = CALL \"!\" [a-z]* \"?\" / CALL \"!\" [a-z]* \";\" / \"b\"" + ops},
		{"call before a left call", "rule s = (CALL)? s [a-z]* \"!\" / s [a-z]* \";\" / \"b\"" + ops},
		{"call in a repetition", "rule main = (CALL [a-z]* \"!\" / [a-z]* \";\")* $$\nrule s = \"b\"" + ops},
	} {
		t.Run(tc.name, func(t *testing.T) {
			named := classifySource(t, strings.ReplaceAll(tc.src, "CALL", "level(e, sum)"))
			plain := classifySource(t, strings.ReplaceAll(tc.src, "CALL", "e"))
			if got, want := classSummary(named), classSummary(plain); got != want {
				t.Fatalf("named level: %s\nno level: %s", got, want)
			}
		})
	}
}

// classSummary describes the class, the degree and the kinds and degrees of the causes and
// reasons of c, which do not quote the grammar.
func classSummary(c *Complexity) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%v degree %d;", c.Class, classDegree(c))
	for _, x := range c.Causes {
		fmt.Fprintf(&b, " cause %v/%v/%d", x.Kind, x.Class, x.Degree)
	}
	fmt.Fprintf(&b, "; %d reasons", len(c.Reasons))
	return b.String()
}
