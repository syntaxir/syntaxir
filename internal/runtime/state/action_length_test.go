package state

import (
	"context"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// TestEngineValueLengthUnits runs the length table (conform.RunLengths) on the composition actionLength of
// the engine under a compute limit: len of a string counts its bytes in either unit, as in the generated
// libraries. The reference build counts no units, so the table does not apply to it.
func TestEngineValueLengthUnits(t *testing.T) {
	if !BudgetControlsEnabled {
		t.Skip("the reference build counts no compute units")
	}
	if err := conform.RunLengths("engine", func(s string, codePoints bool) (int, uint64) {
		p := &Parser{}
		p.Input.Unit = input.Bytes
		if codePoints {
			p.Input.Unit = input.CodePoints
		}
		p.Input.Control = NewBudgetControl(context.Background(), runtime.Limits{Compute: 1 << 62})
		v, err := p.actionLength(s)
		if err != nil {
			t.Fatalf("len(%q) fails: %v", s, err)
		}
		return v.(int), p.Control.Used
	}); err != nil {
		t.Error(err)
	}
}
