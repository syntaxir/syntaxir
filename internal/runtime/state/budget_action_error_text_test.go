//go:build !sxir_budget_controls_reference

package state_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

func TestBudgetActionErrorTextReservationsAndOwnership(t *testing.T) {
	t.Parallel()
	for _, parts := range [][]string{{""}, {"", "", ""}, {"a", "", "λ\xff"}, {strings.Repeat("n", 8192)}} {
		// The text of an error is no event: it counts nothing, so it is made under a limit of one unit.
		want := strings.Join(parts, "")
		p := diagnosticParser(runtime.Limits{Compute: 1})
		retained := p.ActionError(parts...)
		if retained.Error() != want || p.Control.Used != 0 {
			t.Fatalf("builder parity/units: %q %d", retained, p.Control.Used)
		}
		p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 100})
		if got := p.ActionError("retry").Error(); got != "retry" || retained.Error() != want {
			t.Fatal("retry changed owned text")
		}
	}
	// A long text counts nothing either, and makes no check: a canceled context does not stop it.
	p := diagnosticParser(runtime.Limits{Compute: 1})
	if err := statetest.CatchFatal(t, func() { _ = p.ActionError(strings.Repeat("x", 512)) }); err != nil || p.Control.Used != 0 {
		t.Fatalf("long text: err=%v units=%d", err, p.Control.Used)
	}
	for _, parts := range [][]string{make([]string, 320), {strings.Repeat("x", 1024)}} {
		p.Control = state.NewBudgetControl(statetest.NewPollingContext(1), runtime.Limits{})
		if err := statetest.CatchFatal(t, func() { _ = p.ActionError(parts...) }); err != nil {
			t.Fatalf("text under a canceled context: %v", err)
		}
	}
}

func TestBudgetActionErrorFinalPrefixIsFlatAndBounded(t *testing.T) {
	t.Parallel()
	cause := state.ActionTextError("division by zero")
	where := strings.Repeat("rule", 512)
	want := "action in " + where + ": " + cause.Error()
	// The wrapped text counts nothing: the action error ends the parse under a limit of one unit, and
	// under a canceled context, which the wrapping of owned text does not check.
	p := diagnosticParser(runtime.Limits{Compute: 1})
	err := statetest.CatchFatal(t, func() { p.FailAction(where, cause) })
	if err.Error() != want || errors.Unwrap(err) != nil || errors.Is(err, cause) || p.Control.Used != 0 {
		t.Fatalf("final error contract: %v", err)
	}
	p.Control = state.NewBudgetControl(statetest.NewPollingContext(1), runtime.Limits{})
	if err := statetest.CatchFatal(t, func() { p.FailAction(where, cause) }); err.Error() != want {
		t.Fatalf("wrapper under a canceled context: %v", err)
	}
}

type actionFormattingError struct {
	cancel context.CancelFunc
	panics bool
}

func (e actionFormattingError) Error() string { return "Error method" }
func (e actionFormattingError) Format(s fmt.State, verb rune) {
	if e.cancel != nil {
		e.cancel()
	}
	if e.panics {
		panic("formatter panic")
	}
	_, _ = io.WriteString(s, "Formatter method")
}

func TestBudgetActionErrorOpaqueFormattingPreservesNative(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{errors.New("ordinary"), actionFormattingError{}, actionFormattingError{panics: true}} {
		want := fmt.Sprintf("action in r: %v", cause)
		p := diagnosticParser(runtime.Limits{Compute: 1 << 50})
		if got := statetest.CatchFatal(t, func() { p.FailAction("r", cause) }); got.Error() != want || errors.Unwrap(got) != nil {
			t.Fatalf("opaque native formatting: %q, want %q", got, want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := statetest.ActionBudgetParser(ctx, runtime.Limits{}, input.Bytes)
	if err := statetest.CatchFatal(t, func() { p.FailAction("r", actionFormattingError{cancel: cancel}) }); err != context.Canceled {
		t.Fatalf("opaque boundary cancellation: %v", err)
	}
}

func TestBudgetActionErrorProducersAreEager(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("T", 1024)
	prog := enginetest.MustCompile(t, "type A = {\n    N int\n}\n\nrule main = \"a\"\n")
	node := nodes.Ptr(nodes.Data{Kind: &valueimpl.Kind{Type: long}})
	producers := []struct {
		name string
		make func(*state.Parser) error
	}{
		{"invalid member", func(p *state.Parser) error { _, err := (&state.EvalCtx{P: p}).Member(1, long); return err }},
		{"missing field", func(p *state.Parser) error {
			_, err := (&state.EvalCtx{P: p}).Member(nodes.Ptr(nodes.Data{Kind: prog.TypeKind("A")}), long)
			return err
		}},
		{"unary type", func(p *state.Parser) error { _, err := p.ValueUnary("-", node); return err }},
		{"binary type", func(p *state.Parser) error { _, err := p.ValueBinary("+", node, 1); return err }},
		{"list name", func(p *state.Parser) error { _, err := p.ValueListItems(long, 1); return err }},
		{"element name", func(p *state.Parser) error {
			return (&state.EvalCtx{P: p}).Push(long, struct{}{})
		}},
	}
	for _, tc := range producers {
		t.Run(tc.name, func(t *testing.T) {
			p := diagnosticParser(runtime.Limits{Compute: 1 << 50})
			p.Prog = prog.Shared()
			owned := tc.make(p)
			if owned == nil {
				t.Fatal("expected expression error")
			}
			if _, ok := owned.(state.ActionTextError); !ok {
				t.Fatalf("not owned text: %T", owned)
			}
			p.Control = nil
			native := tc.make(p)
			if native.Error() != owned.Error() {
				t.Fatalf("native %q differs from controlled %q", native, owned)
			}
			// The text is made eagerly and counts nothing, so it is made under a limit of one unit.
			p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1})
			var again error
			if err := statetest.CatchFatal(t, func() { again = tc.make(p) }); err != nil || again == nil || again.Error() != owned.Error() || p.Control.Used != 0 {
				t.Fatalf("leaf under a limit of one unit: fatal=%v text=%v", err, again)
			}
		})
	}
}
