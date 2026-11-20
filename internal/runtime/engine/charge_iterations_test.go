//go:build !zp_budget_controls_reference

package engine

import (
	"fmt"
	"testing"

	"zeroparse.dev/zp/internal/testing/codegentest"
	"zeroparse.dev/zp/pkg/runtime/input"
)

// iterationVariant is a grammar with a repetition and the same grammar with the repetition written
// out for one input: the elements it evaluates there in sequence, the last one as a negative
// lookahead when it fails. On that input both examine the same input, call the same rules and build
// values that charge nothing, so their cost (Cost in spec/runtime/parsing.md) differs only by the
// iterations of the repetition: one unit for each evaluation of its element, the last failing one
// included, and none for an option.
type iterationVariant struct {
	name, grammar, unrolled, input string
	iterations                     uint64
}

var iterationVariants = []iterationVariant{
	{"value-free class", `rule main int = [a-z]* "!" => 1`, `rule main int = [a-z] [a-z] [a-z] ![a-z] "!" => 1`, "abc!", 4},
	{"value-free class at a maximum", `rule main int = [a-z]#repeat(0,2) "c!" => 1`, `rule main int = [a-z] [a-z] "c!" => 1`, "abc!", 2},
	{"value-free any", `rule main int = .#repeat(0,3) "!" => 1`, `rule main int = . . . "!" => 1`, "ab!!", 3},
	{"class list", `rule main = [a-z]* "!"`, `rule main = [a-z] [a-z] [a-z] ![a-z] "!"`, "abc!", 4},
	{"sequence", `rule main = ("a" "b")* "!"`, `rule main = "a" "b" "a" "b" !("a" "b") "!"`, "abab!", 3},
	{"element failing inside", `rule main = ("a" "b")* "a!"`, `rule main = "a" "b" "a" "b" !("a" "b") "a!"`, "ababa!", 3},
	{"one or more", `rule main = ("a" "b")+ "!"`, `rule main = "a" "b" "a" "b" !("a" "b") "!"`, "abab!", 3},
	{"at a maximum", `rule main = ("a" "b")#repeat(0,2) "ab!"`, `rule main = "a" "b" "a" "b" "ab!"`, "ababab!", 2},
	{"below a maximum", `rule main = ("a" "b")#repeat(1,3) "!"`, `rule main = "a" "b" "a" "b" !("a" "b") "!"`, "abab!", 3},
	{"captures in the element", `rule main = (x:"a" "b")* "!"`, `rule main = x:"a" "b" y:"a" "b" !("a" "b") "!"`, "abab!", 3},
	{"rule element", "rule main = item* \"!\"\nrule item = \"a\" \"b\"", "rule main = item item !item \"!\"\nrule item = \"a\" \"b\"", "abab!", 3},
	{"matched option", `rule main = ("a" "b")? "!"`, `rule main = "a" "b" "!"`, "ab!", 0},
	{"unmatched option", `rule main = ("a" "b")? "!"`, `rule main = !("a" "b") "!"`, "a!", 0},
}

// TestIterationsChargeOneUnitEach checks that each backend of the engine charges one unit for each
// evaluation of the element of a repetition and nothing else for it: the units of a grammar with a
// repetition exceed those of the repetition written out by its iterations.
func TestIterationsChargeOneUnitEach(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	for _, c := range iterationVariants {
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			t.Run(fmt.Sprintf("%s/%v", c.name, unit), func(t *testing.T) {
				t.Parallel()
				unrolled := engineChargeBackends(t, c.unrolled, unit)
				for i, b := range engineChargeBackends(t, c.grammar, unit) {
					if i >= len(unrolled) {
						break // (the written-out grammar may have no Flow body)
					}
					got, base := chargeTotal(b.run, c.input), chargeTotal(unrolled[i].run, c.input)
					if got != base+c.iterations {
						t.Errorf("%s: %q: %d units for\n%s\nand %d for\n%s\nwant %d more for its iterations", b.name, c.input, got, c.grammar, base, c.unrolled, c.iterations)
					}
				}
			})
		}
	}
}
