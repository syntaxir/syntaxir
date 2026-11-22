//go:build !sxir_budget_controls_reference

package state

import (
	"testing"

	"syntaxir.dev/sxir/pkg/runtime"
)

func TestDiagnosticRenameAccountsForLargeExpectationSets(t *testing.T) {
	ids := make([]ExpID, 512)
	for i := range ids {
		ids[i] = ExpID(i + 1)
	}
	// Renaming an expectation set is no event: it counts nothing and reaches no poll, so neither a
	// limit below its length nor a canceled context interrupts it.
	p := &Parser{Input: Input{Control: NewBudgetControl(nil, runtime.Limits{Compute: 64})}}
	if err := catchFatal(t, func() { RenamedExpected(p, ids, 999) }); err != nil {
		t.Fatalf("rename under a compute limit = %v", err)
	}
	if got := p.Control.Used; got != 0 {
		t.Fatalf("rename steps = %d, want 0", got)
	}

	c := newPollingContext(1)
	p.Control = NewBudgetControl(c, runtime.Limits{})
	if err := catchFatal(t, func() { RenamedExpected(p, ids, 999) }); err != nil || c.checks != 0 {
		t.Fatalf("rename under a canceled context = %v after %d polls, want no poll", err, c.checks)
	}
}
