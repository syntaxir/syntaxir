//go:build !sxir_budget_controls_reference

package state

import (
	"context"
	"errors"
	"testing"

	"syntaxir.dev/sxir/pkg/runtime"
)

// TestChargeCountsSizedEvent pins Charge(n), the count of an event of n units, from every distance
// to a poll boundary and to the limit: the n units are counted at once and then checked, so Used is
// the count with them whether or not they fit, and the parse stops if the count exceeds the limit.
// The check observes the context when BudgetCheckInterval units have been counted since the last
// observation, or when the count exceeds the limit, and a context error wins over the limit. Its
// time does not grow with n.
func TestChargeCountsSizedEvent(t *testing.T) {
	type outcome struct {
		used     uint64
		polled   bool
		limited  bool
		canceled bool
	}
	for _, pre := range []uint64{0, 1, 100, 254, 255, 300, 511} {
		for _, n := range []uint64{1, 2, 255, 256, 257, 600, 1000} {
			for _, limit := range []uint64{0, pre + n - 1, pre + n, pre + n + 5, pre + 3} {
				if limit != 0 && limit < pre {
					continue
				}
				for _, cancel := range []bool{false, true} {
					ctx := newPollingContext(0)
					c := NewBudgetControl(ctx, runtime.Limits{Compute: limit})
					for range pre {
						c.Charge(1)
					}
					since, checks := c.SinceCheck, ctx.checks
					if cancel {
						ctx.at = checks + 1
					}
					err := catchFatal(t, func() { c.Charge(n) })
					got := outcome{c.Used, ctx.checks > checks, errors.Is(err, runtime.ErrLimit), errors.Is(err, context.Canceled)}
					over := limit != 0 && pre+n > limit
					// The context is observed at a poll only, not at a check that finds the limit
					// exceeded (spec/runtime/parsing.md#checks-and-interruptions).
					polled := since+n >= BudgetCheckInterval
					want := outcome{pre + n, polled, over && !(cancel && polled), cancel && polled}
					if got != want {
						t.Errorf("pre %d, n %d, limit %d, cancel %v: %+v, want %+v (%v)", pre, n, limit, cancel, got, want, err)
					}
				}
			}
		}
	}
	c := NewBudgetControl(newPollingContext(0), runtime.Limits{})
	c.Charge(1 << 32)
	if c.Used != 1<<32 {
		t.Fatalf("used %d after an event of 1<<32 units", c.Used)
	}
}
