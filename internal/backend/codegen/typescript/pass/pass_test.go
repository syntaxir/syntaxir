package pass

import (
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
)

// TwoResults names the primitives of the Exec stage that the passes and the printer read, not the
// Flow operations they were lowered from.
func TestTwoResultsNamesPrimitives(t *testing.T) {
	for _, name := range []string{prim.OpMatchLit, prim.OpCallRule, prim.OpCallOutline, prim.OpResumeNext} {
		if !TwoResults(name) {
			t.Errorf("TwoResults(%s) = false, want true", name)
		}
	}
	for _, name := range []string{flow.OpMatchLit, flow.OpCallRule, prim.OpStep, prim.OpUnisolate} {
		if TwoResults(name) {
			t.Errorf("TwoResults(%s) = true, want false", name)
		}
	}
}
