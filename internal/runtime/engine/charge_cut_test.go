//go:build !sxir_budget_controls_reference

package engine

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// cutVariants are grammars that differ only in cuts. On their inputs no expression fails after
// passing a cut, so the cuts change no evaluation, and the cost (Cost in spec/runtime/parsing.md)
// of each input is the same for every grammar of a variant.
var cutVariants = []struct {
	name     string
	grammars []string
	inputs   []string
}{
	{"choice in a repetition", []string{
		`rule main = ("a" "b" / "c")* $$`,
		`rule main = ("a" #cut "b" / "c")* $$`,
	}, []string{"abcab", "", "ccc"}},
	{"option", []string{
		`rule main = ("a" ("b" "c")?)* $$`,
		`rule main = ("a" ("b" #cut "c")?)* $$`,
	}, []string{"abcaabc", "a"}},
	{"rule body", []string{
		"rule main = item* $$\nrule item = \"x\" \"y\" / \"z\"",
		"rule main = item* $$\nrule item = \"x\" #cut \"y\" / \"z\"",
		"rule main = item* $$\nrule item = (\"x\" #cut \"y\" / \"z\") #cut",
	}, []string{"xyzxy", "zz"}},
}

// TestCutChargesNothing checks that each backend of the engine charges the same units for grammars
// that differ only in cuts which change no evaluation: a cut is not an item of the cost.
func TestCutChargesNothing(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	for _, c := range cutVariants {
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			t.Run(fmt.Sprintf("%s/%v", c.name, unit), func(t *testing.T) {
				t.Parallel()
				first := engineChargeBackends(t, c.grammars[0], unit)
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
