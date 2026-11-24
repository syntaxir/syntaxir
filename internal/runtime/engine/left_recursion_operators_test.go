package engine

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// TestOperatorsNullableOperandLeftCalls pins the left calls of an Operators Rule whose operand can match
// nothing (Rule Facts in spec/core-ir/attributes.md): its infix and postfix parts are tried where the rule
// started, so a rule they call there is on a left-recursion cycle with the Operators Rule and grows as
// left recursion does. Without that clause each parse recursed without end, whatever the input. Every
// backend returns the pinned result, in both units, with the same compute units; the parses with
// insertion find the cycle through an operand that only an insertion makes empty, and return a tree
// with the errors of their insertions.
func TestOperatorsNullableOperandLeftCalls(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, src string
		missing   int
		want      map[string]string
	}{
		{"postfix", "rule main = e $$\nrule e = #operators {\n    operand \"\"\n    level a { postfix x }\n}\nrule x = e \"+\"", 0, map[string]string{
			"":   `(""@e)@main`,
			"+":  `((lhs:""@e op:(""@e "+")@x)@e)@main`,
			"++": `((lhs:""@e op:((lhs:""@e op:(""@e "+")@x)@e "+")@x)@e)@main`,
			"x":  "error",
		}},
		{"infix", "rule main = e $$\nrule e = #operators {\n    operand \"n\" / \"\"\n    level { infix left y }\n    level { postfix \"!\" }\n}\nrule y = e \"*\"", 0, map[string]string{
			"*n": "", "**": "", "*n!": "", "n*n": "error", "": "",
		}},
		{"postfix after an inserted operand", "rule main = e $$\nrule e = #operators {\n    operand \"a\" #missing\n    level { postfix x }\n}\nrule x = e \"+\"", 16, map[string]string{
			"+": "", "++": "", "": "",
			// The growth of e after "a" takes the "+", which then leaves none for x.
			"a+": "error",
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := mustCompile(t, c.src)
			for input, want := range c.want {
				var first string
				var firstUnits uint64
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
						var units uint64
						n, err := p.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, MissingTokens: c.missing, ComputeReport: &units})
						got := codegentest.ResultJSON(n, err)
						at := fmt.Sprintf("%q on %s in %s", input, backend, unit)
						if first == "" {
							first, firstUnits = got, units
						} else if got != first || units != firstUnits {
							t.Errorf("%s: %s with %d units, the closure backend in code points %s with %d", at, got, units, first, firstUnits)
						}
						switch {
						case want == "error" && err == nil:
							t.Errorf("%s: %v, want a syntax error", at, n)
						case want != "error" && (n == nil || c.missing == 0 && err != nil):
							// With insertion a tree comes with the syntax errors of its insertions.
							t.Errorf("%s: %v, %v", at, n, err)
						case want != "error" && want != "" && n.String() != want:
							t.Errorf("%s: %v, want %s", at, n, want)
						}
					}
				}
			}
		})
	}
}
