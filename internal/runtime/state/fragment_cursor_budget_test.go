//go:build !sxir_budget_controls_reference

package state

import (
	"context"
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

func TestFragmentCursorFallbackInterruption(t *testing.T) {
	source := strings.Repeat("😀\xff", 1000) + "tail"
	in := NewTextInput(source, input.CodePoints, false)
	// Endpoint conversion is no event: it counts nothing and reaches no poll, so neither a limit
	// below its length nor a canceled context interrupts it, and it retains no index.
	in.Control = NewBudgetControl(nil, runtime.Limits{Compute: 8})
	if err := catchFatal(t, func() { in.PrefixByteOffset(source, 2000) }); err != nil || in.Control.Used != 0 {
		t.Fatalf("conversion under a compute limit: %v, counted %d units", err, in.Control.Used)
	}
	ctx, cancel := context.WithCancel(context.Background())
	in.Control = NewBudgetControl(ctx, runtime.Limits{})
	cancel()
	if err := catchFatal(t, func() { in.PrefixByteOffset(source, 2000) }); err != nil {
		t.Fatalf("conversion under a canceled context: %v", err)
	}
	if in.Src != source || in.Offs != nil {
		t.Fatal("conversion changed source or retained an index")
	}
	in.Control = nil
	if got := in.PrefixByteOffset(source, 2000); got != 5000 {
		t.Fatalf("retry endpoint=%d, want 5000", got)
	}
}
