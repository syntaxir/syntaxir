package state_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
)

func TestNewBudgetControlDisabled(t *testing.T) {
	t.Parallel()
	var absent *state.BudgetControl
	absent.Check()
	absent.Charge(1)

	for name, ctx := range map[string]context.Context{
		"nil":        nil,
		"background": context.Background(),
		"no done":    context.WithoutCancel(context.WithValue(context.Background(), struct{}{}, "v")),
	} {
		t.Run(name, func(t *testing.T) {
			if got := state.NewBudgetControl(ctx, runtime.Limits{}); got != nil {
				t.Fatalf("newBudgetControl = %#v, want nil", got)
			}
		})
	}
	if got := state.NewBudgetControl(nil, runtime.Limits{Compute: 1}); got == nil {
		t.Fatal("finite compute limit returned nil control")
	}
}

func TestBudgetControlExactLimitsAndOverflow(t *testing.T) {
	t.Parallel()
	c := state.NewBudgetControl(nil, runtime.Limits{Compute: 8})
	c.Charge(8)
	if c.Used != 8 {
		t.Fatalf("counter = %d, want 8", c.Used)
	}
	var limitErr *runtime.LimitError
	err := statetest.CatchFatal(t, func() { c.Charge(1) })
	if !errors.Is(err, runtime.ErrLimit) || !errors.As(err, &limitErr) {
		t.Fatalf("limit+1 error = %v, want a LimitError matching ErrLimit", err)
	}
	// The units of a charge are counted before the check: Used is the counter past the limit, and
	// nothing is requested (spec/runtime/parsing.md#checks-and-interruptions).
	if limitErr.Kind != runtime.LimitCompute || limitErr.Limit != 8 || limitErr.Used != 9 || limitErr.Requested != 0 {
		t.Fatalf("error = %#v", limitErr)
	}

	// A count that would overflow saturates, which still exceeds every limit below the maximum.
	c = state.NewBudgetControl(nil, runtime.Limits{Compute: math.MaxUint64 - 1})
	c.Charge(math.MaxUint64 - 2)
	if err := statetest.CatchFatal(t, func() { c.Charge(2) }); err == nil {
		t.Fatal("overflowing charge unexpectedly succeeded")
	}
	if c.Used != math.MaxUint64 {
		t.Fatalf("overflowing charge left the counter at %d, want it saturated", c.Used)
	}
}

// TestBudgetControlExceedingChargeKeepsItsUnits checks that a charge past the limit is counted, never
// taken back: Used is the counter with the charge, and Requested is zero.
func TestBudgetControlExceedingChargeKeepsItsUnits(t *testing.T) {
	t.Parallel()
	c := state.NewBudgetControl(nil, runtime.Limits{Compute: 6})
	c.Charge(2)
	var got *runtime.LimitError
	if !errors.As(statetest.CatchFatal(t, func() { c.Charge(5) }), &got) {
		t.Fatal("charge over the limit did not fail with LimitError")
	}
	if got.Kind != runtime.LimitCompute || got.Limit != 6 || got.Used != 7 || got.Requested != 0 {
		t.Fatalf("error = %#v", got)
	}
	if c.Used != 7 {
		t.Fatalf("the charge left the counter at %d, want 7", c.Used)
	}
}

func TestBudgetControlUnlimitedCounterSaturates(t *testing.T) {
	t.Parallel()
	c := state.NewBudgetControl(statetest.NewPollingContext(0), runtime.Limits{})
	c.Charge(math.MaxUint64 - 2)
	c.Charge(10)
	if c.Used != math.MaxUint64 {
		t.Fatalf("saturated counter = %d, want MaxUint64", c.Used)
	}
	if c.SinceCheck != 10 {
		t.Fatalf("poll counter after following small reservation = %d, want 10", c.SinceCheck)
	}
}

func TestBudgetControlCancellationAndCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("caller cause")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	c := state.NewBudgetControl(ctx, runtime.Limits{Compute: 1})
	// A check observes the context only once BudgetCheckInterval units have been counted since the
	// last observation; one that observes it and finds the limit exceeded too reports the context.
	if got := statetest.CatchFatal(t, func() { state.NewBudgetControl(ctx, runtime.Limits{Compute: 1}).Charge(2) }); !errors.Is(got, runtime.ErrLimit) {
		t.Fatalf("error before an observation = %v, want the compute limit", got)
	}
	got := statetest.CatchFatal(t, func() { c.Charge(state.BudgetCheckInterval) })
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", got)
	}
	if errors.Is(got, cause) {
		t.Fatalf("error = %v, unexpectedly replaced context.Err with cause", got)
	}
	if context.Cause(ctx) != cause {
		t.Fatalf("context cause = %v, want caller cause", context.Cause(ctx))
	}

	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	deadline := state.NewBudgetControl(deadlineCtx, runtime.Limits{Compute: 1})
	if err := statetest.CatchFatal(t, deadline.Check); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v, want context.DeadlineExceeded", err)
	}
}

func TestBudgetControlPollInterval(t *testing.T) {
	t.Parallel()
	ctx := statetest.NewPollingContext(1)
	c := state.NewBudgetControl(ctx, runtime.Limits{})
	c.Charge(state.BudgetCheckInterval - 1)
	if ctx.Checks != 0 {
		t.Fatalf("checks before interval = %d, want 0", ctx.Checks)
	}
	if err := statetest.CatchFatal(t, func() { c.Charge(1) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("interval poll error = %v, want context.Canceled", err)
	}
	if ctx.Checks != 1 {
		t.Fatalf("checks at interval = %d, want 1", ctx.Checks)
	}
}

func TestBudgetControlExplicitCheck(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	c := state.NewBudgetControl(ctx, runtime.Limits{})
	c.Charge(1)
	cancel()
	if err := statetest.CatchFatal(t, c.Check); !errors.Is(err, context.Canceled) {
		t.Fatalf("check error = %v, want context.Canceled", err)
	}
}
