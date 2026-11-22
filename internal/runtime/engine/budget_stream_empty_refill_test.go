//go:build !sxir_budget_controls_reference

package engine

import (
	"errors"
	"io"
	"testing"

	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// Reading the input is no event, so the refills of a reader that returns no data count no compute
// unit and a limit does not stop them; the reader of the stream does, after the empty reads it allows
// (io.ErrNoProgress).
func TestBudgetStreamEmptyRefillsCountNoUnits(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = row* #stream $$

rule row = .+ #atomic`)
	budgetStreamVariants(t, func(t *testing.T, o ParseOptions) {
		reader := &statetest.CancelledRefillReader{Cancel: func() {}}
		o.Limits = runtime.Limits{Compute: 64}
		emits := 0
		err := prog.ParseStreamWith("main", reader, func(*value.Node) error { emits++; return nil }, o)
		if !errors.Is(err, io.ErrNoProgress) || errors.Is(err, runtime.ErrLimit) || reader.Calls != 100 || emits != 0 {
			t.Fatalf("error=%v calls=%d emits=%d", err, reader.Calls, emits)
		}
	})
}
