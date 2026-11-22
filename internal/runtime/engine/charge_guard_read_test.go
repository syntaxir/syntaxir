//go:build !sxir_budget_controls_reference

package engine

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestKnownGuardReadDivergence pins the units of the divergence knownGuardReadDivergence accepts on
// its smallest grammars, a choice of three literals that all fail and a choice whose first literal
// fails after its guard: the bytecode VM charges a read for each guard it tests, the closure backend
// one for the guards of the choice. The change that makes every backend charge a guard nothing
// (#310) makes the units agree and removes this test with the entry.
func TestKnownGuardReadDivergence(t *testing.T) {
	codegentest.NeedsBudgetControls(t)
	for _, c := range []struct {
		grammar, input string
		closure, vm    uint64
	}{
		{`rule main = "a" / "b" / "c"`, "x", 7, 9},
		{`rule main = "ab" / "b"`, "ax", 12, 13},
	} {
		prog := mustCompile(t, c.grammar)
		_, _, closure := parseChargedWith(t, prog, c.input, Closure)
		for _, b := range vmBackends {
			_, _, vm := parseChargedWith(t, prog, c.input, b)
			if closure != c.closure || vm != c.vm {
				t.Errorf("%s on %q, %s: closure %d units, VM %d, want %d and %d", c.grammar, c.input, b, closure, vm, c.closure, c.vm)
			}
			if !knownGuardReadDivergence(prog.Module(), closure, vm) {
				t.Errorf("%s on %q, %s: the divergence is not accepted", c.grammar, c.input, b)
			}
		}
	}
	// One guard alone diverges in nothing.
	prog := mustCompile(t, `rule main = "a" "b"`)
	if knownGuardReadDivergence(prog.Module(), 1, 2) {
		t.Error("a grammar without a choice of guards is accepted")
	}
}
