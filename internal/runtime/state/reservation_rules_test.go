package state

import (
	"context"
	"errors"
	"testing"

	"syntaxir.dev/sxir/pkg/runtime"
)

// reserve runs c.Charge(n) and returns the error of the interruption it raises, or nil.
func reserve(c *BudgetControl, n uint64) (err error) {
	defer func() {
		if x := recover(); x != nil {
			f, ok := x.(Fatal)
			if !ok {
				panic(x)
			}
			err = f.Err
		}
	}()
	c.Charge(n)
	return nil
}

// TestReservationRules pins the rules of "Checks and Interruptions" in spec/runtime/parsing.md on
// the engine's check: the units of an event are counted before the check after it and are never
// taken back; a cancelled context is observed only once 256 units have been counted since the last
// observation; and there its error wins over the exceeded limit.
func TestReservationRules(t *testing.T) {
	if BudgetCheckInterval != 256 {
		t.Fatalf("polling interval %d, the specification states 256", BudgetCheckInterval)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := NewBudgetControl(ctx, runtime.Limits{Compute: 1000})
	if err := reserve(c, 200); err != nil || c.Used != 200 || c.SinceCheck != 200 {
		t.Fatalf("count below the interval: %v, used %d, since %d", err, c.Used, c.SinceCheck)
	}
	cancel()
	// Neither polls nor exceeds: the cancellation is not observed yet.
	if err := reserve(c, 55); err != nil || c.Used != 255 {
		t.Fatalf("count before the poll: %v, used %d", err, c.Used)
	}
	// Reaches the interval: the context is observed, and the units stay counted.
	if err := reserve(c, 1); !errors.Is(err, context.Canceled) || c.Used != 256 {
		t.Fatalf("polling count: %v, used %d, since %d", err, c.Used, c.SinceCheck)
	}
	// Exceeds as well as polls: the context error wins over the limit.
	if err := reserve(c, 5000); !errors.Is(err, context.Canceled) || c.Used != 5256 {
		t.Fatalf("exceeding count with a cancelled context: %v, used %d", err, c.Used)
	}
	// Exceeds without polling: the context is not observed, and the limit stops the parse.
	f := NewBudgetControl(ctx, runtime.Limits{Compute: 10})
	if err := reserve(f, 11); !errors.Is(err, runtime.ErrLimit) {
		t.Fatalf("exceeding count before a poll with a cancelled context: %v", err)
	}
	// Without a context, exceeding interrupts with a limit error whose Used holds the units.
	d := NewBudgetControl(nil, runtime.Limits{Compute: 10})
	if err := reserve(d, 4); err != nil {
		t.Fatal(err)
	}
	var le *runtime.LimitError
	if err := reserve(d, 7); !errors.As(err, &le) || le.Limit != 10 || le.Used != 11 || le.Requested != 0 || d.Used != 11 {
		t.Fatalf("exceeding count: %v, used %d, since %d", err, d.Used, d.SinceCheck)
	}
	// An observation that finds no error starts the interval again.
	e := NewBudgetControl(context.Background(), runtime.Limits{Compute: 1 << 20})
	if err := reserve(e, 300); err != nil || e.Used != 300 || e.SinceCheck != 0 {
		t.Fatalf("polling count without an error: %v, used %d, since %d", err, e.Used, e.SinceCheck)
	}
}
