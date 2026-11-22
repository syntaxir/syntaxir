package libgo

import (
	"errors"
	"math"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

// The operations of a region of Document work declare a cost of zero: they open and close the region
// and reserve no unit. The units reserved inside are the work they cover, and a default limit does
// not count them, whatever their number and whether the limit is the default or explicit.
func TestFreeRegionChargesExactly(t *testing.T) {
	const limit = 1000
	for _, work := range []uint64{0, 1, 999, limit, limit + 1, 1 << 40, math.MaxUint64 / 4} {
		c := newBudgetControl(nil, sxirruntime.Limits{Compute: limit})
		mark := c.FreeBegin(true)
		if c.budgetUsed() != 0 || c.doc != 0 {
			t.Fatalf("work %d: FreeBegin reserved %d units", work, c.budgetUsed())
		}
		c.Charge(work)
		c.FreeEnd(mark)
		if c.budgetUsed() != work || c.doc != work {
			t.Fatalf("work %d: used %d, Document units %d", work, c.budgetUsed(), c.doc)
		}
		if c.limits.Compute != limit+work {
			t.Fatalf("work %d: the limit is %d after the region, want %d", work, c.limits.Compute, limit+work)
		}
		c.Charge(limit)
		var le *sxirruntime.LimitError
		func() {
			defer func() {
				if f, ok := recover().(fatal); ok {
					errors.As(f.err, &le)
				}
			}()
			c.Charge(1)
		}()
		if le == nil || le.Limit != limit || le.Used != limit {
			t.Fatalf("work %d: counted work stopped with %+v, want limit and used %d", work, le, limit)
		}
	}
}

// ChargeDoc reserves exactly the units it is given, as Charge does, and counts them as Document work
// under every limit, so that none counts them against it.
func TestChargeDocChargesExactly(t *testing.T) {
	c := newBudgetControl(nil, sxirruntime.Limits{Compute: 100})
	c.ChargeDoc(70)
	c.ChargeDoc(70)
	c.Charge(100)
	if c.budgetUsed() != 240 || c.doc != 140 {
		t.Fatalf("used %d (want 240), Document units %d (want 140)", c.budgetUsed(), c.doc)
	}
	var le *sxirruntime.LimitError
	func() {
		defer func() {
			if f, ok := recover().(fatal); ok {
				errors.As(f.err, &le)
			}
		}()
		c.Charge(1)
	}()
	if le == nil || le.Limit != 100 || le.Used != 100 {
		t.Fatalf("counted work stopped with %+v, want limit and used 100", le)
	}
	// Under no limit the units still count as Document work, and no limit appears.
	u := newBudgetControl(nil, sxirruntime.Limits{Memory: 1 << 40})
	u.ChargeDoc(5)
	mark := u.FreeBegin(true)
	u.Charge(7)
	u.FreeEnd(mark)
	if u.budgetUsed() != 12 || u.doc != 12 || u.limits.Compute != 0 {
		t.Fatalf("no limit: used %d, Document units %d, limit %d", u.budgetUsed(), u.doc, u.limits.Compute)
	}
}

// A region opens at most once, and none opens with on false or on no control.
func TestFreeRegionDoesNotNest(t *testing.T) {
	c := newBudgetControl(nil, sxirruntime.Limits{Compute: 10})
	outer := c.FreeBegin(true)
	inner := c.FreeBegin(true)
	if inner != noFree {
		t.Fatal("a region opened inside a region")
	}
	c.Charge(5)
	c.FreeEnd(inner)
	c.Charge(5)
	c.FreeEnd(outer)
	if c.budgetUsed() != 10 || c.doc != 10 || c.limits.Compute != 20 {
		t.Fatalf("used %d, Document units %d, limit %d", c.budgetUsed(), c.doc, c.limits.Compute)
	}
	if c.FreeBegin(false) != noFree {
		t.Fatal("a region opened with on false")
	}
	var none *BudgetControl
	none.FreeEnd(none.FreeBegin(true))
}
