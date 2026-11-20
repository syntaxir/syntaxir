//go:build !zp_budget_controls_reference

package state

import (
	"fmt"
	"testing"

	"zeroparse.dev/zp/pkg/runtime"
)

// TestChargeUnitsIsOneUnitReservations pins that ChargeUnits(n) reserves what n calls of Charge(1)
// do, from every distance to a poll boundary and to the limit: the same units, the same distance to
// the next poll, the same failure (the unit that does not fit, after the units that do), the same
// polls, and a cancellation that the first poll observes. Its time grows with the polls, not with n.
func TestChargeUnitsIsOneUnitReservations(t *testing.T) {
	type outcome struct {
		used, since uint64
		polls       int
		err         string
	}
	run := func(pre, n, limit uint64, cancel, units bool) (o outcome) {
		ctx := newPollingContext(0)
		c := NewBudgetControl(ctx, runtime.Limits{Compute: limit})
		for range pre {
			c.Charge(1)
		}
		checks := ctx.checks
		if cancel {
			ctx.at = checks + 1
		}
		err := catchFatal(t, func() {
			if units {
				c.ChargeUnits(n)
				return
			}
			for range n {
				c.Charge(1)
			}
		})
		if err != nil {
			o.err = err.Error()
		}
		return outcome{c.Used, c.SinceCheck, ctx.checks - checks, o.err}
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
						t.Errorf("%s: ChargeUnits %+v, one-unit charges %+v", name, got, want)
					}
				}
			}
		}
	}
	c := NewBudgetControl(newPollingContext(0), runtime.Limits{})
	c.ChargeUnits(1 << 32)
	if c.Used != 1<<32 {
		t.Fatalf("used %d after a run of 1<<32 units", c.Used)
	}
}
