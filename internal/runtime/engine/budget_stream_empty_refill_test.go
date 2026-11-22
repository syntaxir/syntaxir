//go:build !sxir_budget_controls_reference

package engine

import (
	"errors"
	"testing"

	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

func TestBudgetStreamEmptyRefillBudget(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = row* #stream $$

rule row = .+ #atomic`)
	budgetStreamVariants(t, func(t *testing.T, o ParseOptions) {
		reader := &statetest.CancelledRefillReader{Cancel: func() {}}
		o.Limits = runtime.Limits{Compute: 64}
		emits := 0
		err := prog.ParseStreamWith("main", reader, func(*value.Node) error { emits++; return nil }, o)
		var limit *runtime.LimitError
		if !errors.As(err, &limit) || limit.Kind != runtime.LimitCompute || limit.Used != 64 || limit.Requested != 1 || reader.Calls == 0 || reader.Calls > 64 || emits != 0 {
			t.Fatalf("error=%v calls=%d emits=%d", err, reader.Calls, emits)
		}
	})
}
