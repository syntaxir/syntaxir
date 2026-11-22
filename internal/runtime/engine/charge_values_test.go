//go:build !sxir_budget_controls_reference

package engine

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// runtimeValueVariants are grammars whose cost (Cost in spec/runtime/parsing.md) is the same on
// each input and that differ only in the values the parse builds: terminals, sequences, lists of
// repetitions, captures and the nodes that wrap them, and the captures that backtracking undoes. The
// inputs match: the syntax error of a failed parse depends on the expectations, which the grammars do
// not share. Recognizing the first grammar is a reference too, unless it has captures: a call of a
// rule with captures costs more than one without (to be settled with the call protocol).
var runtimeValueVariants = []struct {
	name     string
	grammars []string
	inputs   []string
	captured bool // the first grammar has captures, so recognizing it is no reference
}{
	{"sequence", []string{
		`rule main = "abc"`,
		`rule main = "a" "b" "c"`,
		`rule main = x:"a" "b" y:"c"`,
	}, []string{"abc"}, false},
	{"repetition", []string{
		`rule main = [a-z]*`,
		`rule main = ([a-z])*`,
		`rule main = (c:[a-z])*`,
	}, []string{"abcd", ""}, false},
	{"captures", []string{
		"rule main = key \"=\" val $$\nrule key = [a-z]+\nrule val = [0-9]+ / key",
		"rule main = k:key \"=\" v:val $$\nrule key = [a-z]+\nrule val = [0-9]+ / key",
	}, []string{"a=1", "abc=def"}, false},
	{"undone captures", []string{
		"rule main = item* $$\nrule item = (\"a\" \"b\" \"c\" / \"a\" \"b\" / \"a\") x:\"\"",
		"rule main = item* $$\nrule item = x:\"a\" \"b\" \"c\" / x:\"a\" \"b\" / x:\"a\"",
	}, []string{"aababc", "abcab"}, true},
	{"operator captures", []string{
		"rule main = expr $$\nrule expr = #operators {\n    operand [0-9]\n    level {\n        infix left \"+\"\n    }\n    level {\n        prefix \"-\"\n    }\n}",
		"rule main = expr $$\nrule expr = #operators {\n    operand d:[0-9]\n    level {\n        infix left \"+\"\n    }\n    level {\n        prefix \"-\"\n    }\n}",
	}, []string{"1+-2+3", "--1"}, false},
}

// TestRuntimeValuesChargeNothing checks that each backend of the engine charges the same units for
// grammars that differ only in the values the parse builds, and the same units as recognizing the
// first grammar, which builds no values: those values are not an item of the cost.
func TestRuntimeValuesChargeNothing(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	for _, c := range runtimeValueVariants {
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			t.Run(fmt.Sprintf("%s/%v", c.name, unit), func(t *testing.T) {
				t.Parallel()
				first := engineChargeBackends(t, c.grammars[0], unit)
				for i, b := range engineRecognizeBackends(t, c.grammars[0], unit) {
					if c.captured {
						break
					}
					for k, in := range c.inputs {
						if got, want := chargeTotal(first[i].run, in), chargeTotal(b.run, in); got != want {
							t.Errorf("%s: input %d %q: parsing charges %d units and recognizing %d for\n%s", b.name, k, in, got, want, c.grammars[0])
						}
					}
				}
				for _, g := range c.grammars[1:] {
					for i, b := range engineChargeBackends(t, g, unit) {
						for k, in := range c.inputs {
							if got, want := chargeTotal(b.run, in), chargeTotal(first[i].run, in); got != want {
								t.Errorf("%s: input %d %q: %d units for\n%s\nand %d for\n%s", b.name, k, in, got, g, want, c.grammars[0])
							}
						}
					}
				}
			})
		}
	}
}

// engineRecognizeBackends is engineChargeBackends for recognizing src, which builds no values.
func engineRecognizeBackends(t testing.TB, src string, unit input.PositionUnit) []chargeBackend {
	t.Helper()
	old := mustCompile(t, src)
	on := func(prog *Program, b Backend) chargeRun {
		return func(in string, limit uint64) string {
			_, err := prog.ParseWith("", in, ParseOptions{Backend: b, Unit: unit, Expectations: true, Recognize: true, Limits: sxirruntime.Limits{Compute: limit}})
			return limitReportOf(err)
		}
	}
	return []chargeBackend{{"closure", on(old, Closure)}, {"bytecode", on(old, Bytecode)}, {"bytecode-iterative", on(old, BytecodeIterative)}}
}
