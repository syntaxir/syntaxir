//go:build !sxir_budget_controls_reference

package engine

import (
	"context"
	"errors"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

func TestBudgetStreamRawReaderCancellation(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = row* #stream $$

rule row = .+ #atomic`)
	budgetStreamVariants(t, func(t *testing.T, o ParseOptions) {
		for _, shape := range []string{"empty", "partial-rune"} {
			t.Run(shape, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				reader := &statetest.CancelledRefillReader{Cancel: cancel}
				if shape == "partial-rune" {
					reader.First = []byte{0xc3}
				}
				o.Context = ctx
				emits := 0
				err := prog.ParseStreamWith("main", reader, func(*value.Node) error { emits++; return nil }, o)
				if !errors.Is(err, context.Canceled) || reader.Calls != 1 || emits != 0 {
					t.Fatalf("error=%v readercalls=%d emits=%d", err, reader.Calls, emits)
				}
			})
		}
	})
}

func TestBudgetStreamRawReaderAdmission(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	caller := &statetest.CancelledRefillReader{Cancel: func() {}}
	var returned error
	observer := &state.StreamReaderObserver{Reader: caller, Result: &returned, Control: state.NewBudgetControl(ctx, runtime.Limits{})}
	err := statetest.CatchFatal(t, func() { _, _ = observer.Read(make([]byte, 4)) })
	if !errors.Is(err, context.Canceled) || caller.Calls != 0 || returned != nil {
		t.Fatalf("error=%v calls=%d returned=%v", err, caller.Calls, returned)
	}
}

type budgetRawReaderPanicContext struct {
	context.Context
	reader *statetest.CancelledRefillReader
	token  *statetest.ContextPanicToken
}

func (c *budgetRawReaderPanicContext) Err() error {
	if c.reader.Calls != 0 {
		panic(c.token)
	}
	return c.Context.Err()
}

func TestBudgetStreamRawReaderContextPanicIdentity(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = row* #stream $$

rule row = .+ #atomic`)
	budgetStreamVariants(t, func(t *testing.T, o ParseOptions) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		reader := &statetest.CancelledRefillReader{Cancel: cancel, First: []byte{0xc3}}
		token := &statetest.ContextPanicToken{Phase: "raw reader postcheck"}
		o.Context = &budgetRawReaderPanicContext{Context: ctx, reader: reader, token: token}
		var saved TraceEvent
		o.Trace = func(e TraceEvent) {
			if state.TraceParser(saved) == nil {
				saved = e
			}
		}
		var err error
		got := statetest.CatchPanic(func() {
			err = prog.ParseStreamWith("main", reader, func(*value.Node) error { t.Fatal("unexpected emit"); return nil }, o)
		})
		if got != token || err != nil || reader.Calls != 1 {
			t.Fatalf("panic=%#v error=%v calls=%d", got, err, reader.Calls)
		}
		statetest.AssertTraceParserDetached(t, state.TraceParser(saved), false)
	})
}
