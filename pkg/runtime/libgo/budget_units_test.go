package libgo

import (
	"context"
	"fmt"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

// unitsContext counts its polls and reports cancellation from the at-th on (never when at is 0);
// Done never fires, so only polling sees it.
type unitsContext struct {
	context.Context
	done       chan struct{}
	checks, at int
}

func (c *unitsContext) Done() <-chan struct{} { return c.done }
func (c *unitsContext) Err() error {
	c.checks++
	if c.at > 0 && c.checks >= c.at {
		return context.Canceled
	}
	return nil
}

// TestTakeIsOneUnitReservations pins that Take(n) reserves what n one-unit reservations do, from
// every distance to a poll boundary and to the limit: the same units, the same distance to the next
// poll, the same failure (the unit that does not fit, after the units that do), the same polls, and a
// cancellation that the first poll observes. Its time grows with the polls, not with n.
func TestTakeIsOneUnitReservations(t *testing.T) {
	type outcome struct {
		used, since uint64
		polls       int
		err         string
	}
	run := func(pre, n, limit uint64, cancel, units bool) (o outcome) {
		ctx := &unitsContext{Context: context.Background(), done: make(chan struct{})}
		c := newBudgetControl(ctx, sxirruntime.Limits{Compute: limit})
		for range pre {
			c.Charge(1)
		}
		checks := ctx.checks
		if cancel {
			ctx.at = checks + 1
		}
		func() {
			defer func() {
				if f, ok := recover().(fatal); ok {
					o.err = f.err.Error()
				}
			}()
			if units {
				c.Take(n)
				return
			}
			for range n {
				c.Charge(1)
			}
		}()
		c.settle()
		return outcome{c.base, c.sinceCheck, ctx.checks - checks, o.err}
	}
	for _, pre := range []uint64{0, 1, 100, 254, 255, 300, 511} {
		for _, n := range []uint64{1, 2, 255, 256, 257, 600, 1000} {
			for _, limit := range []uint64{0, pre + n - 1, pre + n, pre + n + 5, pre + 3} {
				if limit != 0 && limit < pre {
					continue
				}
				for _, cancel := range []bool{false, true} {
					name := fmt.Sprintf("pre %d, n %d, limit %d, cancel %v", pre, n, limit, cancel)
					if got, want := run(pre, n, limit, cancel, true), run(pre, n, limit, cancel, false); got != want {
						t.Errorf("%s: Take %+v, one-unit reservations %+v", name, got, want)
					}
				}
			}
		}
	}
	c := newBudgetControl(&unitsContext{Context: context.Background(), done: make(chan struct{})}, sxirruntime.Limits{})
	c.Take(1 << 32)
	if c.budgetUsed() != 1<<32 {
		t.Fatalf("used %d after a run of 1<<32 units", c.budgetUsed())
	}
}
