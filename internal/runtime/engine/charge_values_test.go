//go:build !zp_budget_controls_reference

package engine

import (
	"fmt"
	"testing"

	"zeroparse.dev/zp/internal/testing/codegentest"
	zpruntime "zeroparse.dev/zp/pkg/runtime"
	"zeroparse.dev/zp/pkg/runtime/input"
)

// runtimeValueVariants are grammars whose cost (Cost in spec/runtime/parsing.md) is the same on
// each input and that differ only in the values the parse builds: terminals, sequences, lists of
// repetitions, captures and the nodes that wrap them. The inputs match: the syntax error of a failed
// parse depends on the expectations, which the grammars do not share.
var runtimeValueVariants = []struct {
	name     string
	grammars []string
	inputs   []string
}{
	{"sequence", []string{
		`rule main = "abc"`,
		`rule main = "a" "b" "c"`,
		`rule main = x:"a" "b" y:"c"`,
	}, []string{"abc"}},
	{"repetition", []string{
		`rule main = [a-z]*`,
		`rule main = ([a-z])*`,
		`rule main = (c:[a-z])*`,
	}, []string{"abcd", ""}},
	{"captures", []string{
		"rule main = key \"=\" val $$\nrule key = [a-z]+\nrule val = [0-9]+ / key",
		"rule main = k:key \"=\" v:val $$\nrule key = [a-z]+\nrule val = [0-9]+ / key",
	}, []string{"a=1", "abc=def"}},
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
	old, flows, n := flowProgramsOf(t, src, func(string) bool { return true }, nil)
	on := func(prog *Program, b Backend) chargeRun {
		return func(in string, limit uint64) string {
			_, err := prog.ParseWith("", in, ParseOptions{Backend: b, Unit: unit, Expectations: true, Recognize: true, Limits: zpruntime.Limits{Compute: limit}})
			return limitReportOf(err)
		}
	}
	out := []chargeBackend{{"closure", on(old, Closure)}, {"bytecode", on(old, Bytecode)}, {"bytecode-iterative", on(old, BytecodeIterative)}}
	if n > 0 {
		out = append(out, chargeBackend{"flow", on(flows, Bytecode)}, chargeBackend{"flow-iterative", on(flows, BytecodeIterative)})
	}
	return out
}
