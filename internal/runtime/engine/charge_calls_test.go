//go:build !sxir_budget_controls_reference

package engine

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// callVariant is a grammar with rule calls and a reference grammar that, on one input, examines the
// same input, makes the same iterations and builds values that charge nothing, so that their cost
// (Cost in spec/runtime/parsing.md) differs only by the calls the grammar makes more: one unit for
// each call, one for each evaluation of a body, and for each call of a rule that a parse from scratch
// memoizes, one unit for each variable the rule reads and one for each byte of the string values
// among them. Deferring the memoization of a first call and storing a result charge nothing.
type callVariant struct {
	name, grammar, reference, input string
	calls                           uint64
}

var callVariants = []callVariant{
	// item is called once and memoized nowhere: a call and a body.
	{"plain call", "rule main = item \"!\"\nrule item = \"a\" \"b\"", `rule main = "a" "b" "!"`, "ab!", 2},
	{"failing plain call", "rule main = !item \"c\"\nrule item = \"a\" \"b\"", `rule main = !("a" "b") "c"`, "c", 2},
	{"call of a rule with captures", "rule main = item \"!\"\nrule item = x:\"a\" \"b\"", `rule main = x:"a" "b" "!"`, "ab!", 2},
	// item is memoized, and its first call at a position is not: a call and a body all the same.
	{"deferred call", "rule main = item \"!\" / \"?\" item \"?\"\nrule item = a \"b\"\nrule a = \"a\"",
		"rule main = a \"b\" \"!\" / \"?\" item \"?\"\nrule item = a \"b\"\nrule a = \"a\"", "ab!", 2},
	// The key of item holds s, which is three bytes long.
	{"deferred call with a string key", "var s string = \"xyz\"\nrule main = item \"!\" / \"?\" item \"?\"\nrule item = a { s == \"xyz\" } \"b\"\nrule a = \"a\"",
		"var s string = \"xyz\"\nrule main = a { s == \"xyz\" } \"b\" \"!\" / \"?\" item \"?\"\nrule item = a { s == \"xyz\" } \"b\"\nrule a = \"a\"", "ab!", 6},
	// The third call reuses the result that the second stored in the same lookahead context: a call,
	// and the lookup of its key.
	{"reused result", "rule main = &item &item &item \"ab!\"\nrule item = a \"b\"\nrule a = \"a\"",
		"rule main = &item &item \"ab!\"\nrule item = a \"b\"\nrule a = \"a\"", "ab!", 1},
	{"reused result with an int key", "var n int = 7\nrule main = &item &item &item \"ab!\"\nrule item = a { n == 7 } \"b\"\nrule a = \"a\"",
		"var n int = 7\nrule main = &item &item \"ab!\"\nrule item = a { n == 7 } \"b\"\nrule a = \"a\"", "ab!", 2},
	{"reused result with a string key", "var s string = \"xyz\"\nrule main = &item &item &item \"ab!\"\nrule item = a { s == \"xyz\" } \"b\"\nrule a = \"a\"",
		"var s string = \"xyz\"\nrule main = &item &item \"ab!\"\nrule item = a { s == \"xyz\" } \"b\"\nrule a = \"a\"", "ab!", 5},
	{"reused result with a bound string key", "var s string\nrule main = { s := \"uvwxy\" } &item &item &item \"ab!\"\nrule item = a { s == \"uvwxy\" } \"b\"\nrule a = \"a\"",
		"var s string\nrule main = { s := \"uvwxy\" } &item &item \"ab!\"\nrule item = a { s == \"uvwxy\" } \"b\"\nrule a = \"a\"", "ab!", 7},
}

// TestCallsChargeTheirUnits checks that each backend of the engine charges for a call what the cost
// counts for it and nothing else: the units of a grammar exceed those of its reference by its calls.
func TestCallsChargeTheirUnits(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	for _, c := range callVariants {
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			t.Run(fmt.Sprintf("%s/%v", c.name, unit), func(t *testing.T) {
				t.Parallel()
				reference := engineChargeBackends(t, c.reference, unit)
				for i, b := range engineChargeBackends(t, c.grammar, unit) {
					if i >= len(reference) {
						break // (the reference grammar may have no Flow body)
					}
					got, base := chargeTotal(b.run, c.input), chargeTotal(reference[i].run, c.input)
					if got != base+c.calls {
						t.Errorf("%s: %q: %d units, %d more than the reference, want %d more for its calls\n%s\nreference:\n%s", b.name, c.input, got, int64(got)-int64(base), c.calls, c.grammar, c.reference)
					}
				}
			})
		}
	}
}

// TestTracedCallsChargeAsUntraced checks that tracing a parse changes no unit: the traced path of a
// call, which makes the call again, charges it once.
func TestTracedCallsChargeAsUntraced(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	for _, c := range callVariants {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			untraced := engineChargeBackends(t, c.grammar, input.CodePoints)
			for i, b := range engineChargeBackendsTraced(t, c.grammar, input.CodePoints, true) {
				if got, want := chargeTotal(b.run, c.input), chargeTotal(untraced[i].run, c.input); got != want {
					t.Errorf("%s: %q: %d units traced, %d untraced\n%s", b.name, c.input, got, want, c.grammar)
				}
			}
		})
	}
}
