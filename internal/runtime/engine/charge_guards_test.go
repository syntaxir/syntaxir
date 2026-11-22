//go:build !sxir_budget_controls_reference

package engine

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGuardsCountNoUnit pins the units of parses whose choices test first-character guards, on a
// choice of three literals that all fail, a choice whose first literal fails after its guard and a
// sequence without a choice. A guard examines the next character without advancing, which is no
// event (spec/runtime/parsing.md#compute-units), so it counts nothing on every backend: the closure
// backend, which reads the character once for the guards of a choice (state.ChoiceRead), and the
// bytecode VMs, which test each guard of a Flow body in turn, count the same units: the call of main
// and the syntax error built, and the input that a matching literal advances over.
func TestGuardsCountNoUnit(t *testing.T) {
	codegentest.NeedsBudgetControls(t)
	for _, c := range []struct {
		grammar, input string
		units          uint64
	}{
		{`rule main = "a" / "b" / "c"`, "x", 2},
		{`rule main = "ab" / "b"`, "ax", 2},
		{`rule main = "a" "b"`, "ax", 3},
	} {
		prog := mustCompile(t, c.grammar)
		_, _, closure := parseChargedWith(t, prog, c.input, Closure)
		if closure != c.units {
			t.Errorf("%s on %q: closure %d units, want %d", c.grammar, c.input, closure, c.units)
		}
		for _, b := range vmBackends {
			if _, _, vm := parseChargedWith(t, prog, c.input, b); vm != closure {
				t.Errorf("%s on %q, %s: %d units, the closure backend %d", c.grammar, c.input, b, vm, closure)
			}
		}
	}
}
