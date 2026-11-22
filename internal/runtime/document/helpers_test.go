package document

import (
	"context"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/runtime"
)

// catchFatal runs f and returns the error of the Fatal panic it raises, or nil.
func catchFatal(t *testing.T, f func()) error {
	t.Helper()
	var got error
	func() {
		defer func() {
			if v := recover(); v != nil {
				fatal, ok := v.(state.Fatal)
				if !ok {
					t.Fatalf("panic = %T(%v), want fatal", v, v)
				}
				got = fatal.Err
			}
		}()
		f()
	}()
	return got
}

// cancelAfterUsed is a context that is canceled once the control that polls it has counted more than
// after units.
type cancelAfterUsed struct {
	context.Context
	done  chan struct{}
	c     *state.BudgetControl
	after uint64
}

func (c *cancelAfterUsed) Done() <-chan struct{} { return c.done }
func (c *cancelAfterUsed) Err() error {
	if c.c.Used > c.after {
		return context.Canceled
	}
	return nil
}

// cancelAfter returns a parser without limits whose context is canceled once it has counted more than
// after units, so that only a poll after that many units interrupts it.
func cancelAfter(after uint64) *state.Parser {
	ctx := &cancelAfterUsed{Context: context.Background(), done: make(chan struct{}), after: after}
	ctx.c = state.NewBudgetControl(ctx, runtime.Limits{})
	return &state.Parser{Control: ctx.c}
}
