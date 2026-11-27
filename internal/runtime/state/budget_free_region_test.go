//go:build !sxir_budget_controls_reference

package state

import (
	"errors"
	"math"
	"testing"

	"syntaxir.dev/sxir/pkg/runtime"
)

// The operations of a region of Document work declare a cost of zero: they open and close the region
// and reserve no unit. The units reserved inside are the work they cover, and no Compute limit counts
// them, whatever their number and whether the limit is the default or explicit.
func TestFreeRegionChargesExactly(t *testing.T) {
	const limit = 1000
	for _, work := range []uint64{0, 1, 999, limit, limit + 1, 1 << 40, math.MaxUint64 / 4} {
		c := &BudgetControl{Limits: runtime.Limits{Compute: limit}}
		mark := c.FreeBegin()
		if c.Used != 0 || c.Doc != 0 {
			t.Fatalf("work %d: FreeBegin reserved %d units", work, c.Used)
		}
		c.Charge(work)
		c.FreeEnd(mark)
		if c.Used != work || c.Doc != work {
			t.Fatalf("work %d: used %d, Document units %d", work, c.Used, c.Doc)
		}
		if c.Limits.Compute != limit+work {
			t.Fatalf("work %d: the limit is %d after the region, want %d", work, c.Limits.Compute, limit+work)
		}
		// Counted work has the room of the whole limit left: limit units pass, one more does not.
		c.Charge(limit)
		var le *runtime.LimitError
		func() {
			defer func() {
				if f, ok := recover().(Fatal); ok {
					errors.As(f.Err, &le)
				}
			}()
			c.Charge(1)
		}()
		// The unit past the limit is counted before the check: Used is one past it.
		if le == nil || le.Limit != limit || le.Used != limit+1 {
			t.Fatalf("work %d: counted work stopped with %+v, want limit %d and used one more", work, le, limit)
		}
	}
}

// ChargeDoc reserves exactly the units it is given, as Charge does, and counts them as Document work
// under every limit, so that none counts them against it.
func TestChargeDocChargesExactly(t *testing.T) {
	c := &BudgetControl{Limits: runtime.Limits{Compute: 100}}
	c.ChargeDoc(70)
	c.ChargeDoc(70)
	c.Charge(30)
	if c.Used != 170 || c.Doc != 140 {
		t.Fatalf("default limit: used %d (want 170), Document units %d (want 140)", c.Used, c.Doc)
	}
	// An explicit limit leaves them out too: 70 + 70 units pass a limit of 100, and the counted work
	// has the whole limit.
	c = &BudgetControl{Limits: runtime.Limits{Compute: 100}}
	c.ChargeDoc(70)
	c.ChargeDoc(70)
	c.Charge(100)
	if c.Used != 240 || c.Doc != 140 {
		t.Fatalf("explicit limit: used %d (want 240), Document units %d (want 140)", c.Used, c.Doc)
	}
	var le *runtime.LimitError
	func() {
		defer func() {
			if f, ok := recover().(Fatal); ok {
				errors.As(f.Err, &le)
			}
		}()
		c.Charge(1)
	}()
	if le == nil || le.Limit != 100 || le.Used != 101 {
		t.Fatalf("counted work stopped with %+v, want limit 100 and used 101", le)
	}
	// Under no limit the units still count as Document work, and no limit appears.
	u := &BudgetControl{}
	u.ChargeDoc(5)
	mark := u.FreeBegin()
	u.Charge(7)
	u.FreeEnd(mark)
	if u.Used != 12 || u.Doc != 12 || u.Limits.Compute != 0 {
		t.Fatalf("no limit: used %d, Document units %d, limit %d", u.Used, u.Doc, u.Limits.Compute)
	}
}

// A region opens at most once, and none opens on no control.
func TestFreeRegionDoesNotNest(t *testing.T) {
	c := &BudgetControl{Limits: runtime.Limits{Compute: 10}}
	outer := c.FreeBegin()
	inner := c.FreeBegin()
	if inner != noFree {
		t.Fatal("a region opened inside a region")
	}
	c.Charge(5)
	c.FreeEnd(inner)
	c.Charge(5)
	c.FreeEnd(outer)
	if c.Used != 10 || c.Doc != 10 || c.Limits.Compute != 20 {
		t.Fatalf("used %d, Document units %d, limit %d", c.Used, c.Doc, c.Limits.Compute)
	}
	var none *BudgetControl
	none.FreeEnd(none.FreeBegin())
}

// A rule is memoized only by a Document parse when it is transient and memoized, no leader, not a
// value-less lexical rule, and not one that the missing-token mode evaluates again: the memo protocol
// of such a rule is Document work.
func TestDocOnlyMemo(t *testing.T) {
	missing := &MissingTokenState{}
	for _, c := range []struct {
		name    string
		memoAll bool
		missing *MissingTokenState
		rule    Rule
		want    bool
	}{
		{"transient memoized rule in a Document parse", true, nil, Rule{Memo: true, Transient: true}, true},
		{"the same rule in a parse from scratch", false, nil, Rule{Memo: true, Transient: true}, false},
		{"a rule that a parse from scratch memoizes", true, nil, Rule{Memo: true}, false},
		{"a rule that is not memoized", true, nil, Rule{Transient: true}, false},
		{"a leader", true, nil, Rule{Memo: true, Transient: true, Leader: true}, false},
		{"a missing-token rule without the mode", true, nil, Rule{Memo: true, Transient: true, Missing: true}, true},
		{"a missing-token rule in the mode", true, missing, Rule{Memo: true, Transient: true, Missing: true}, false},
		{"another rule in the mode", true, missing, Rule{Memo: true, Transient: true}, true},
		{"a value-less lexical rule", true, nil, Rule{Memo: true, Transient: true, Lexical: true, Novalue: true}, false},
		{"a lexical rule that builds its value", true, nil, Rule{Memo: true, Transient: true, Lexical: true}, true},
		{"a value-free twin that is not lexical", true, nil, Rule{Memo: true, Transient: true, Novalue: true}, true},
	} {
		p := &Parser{MemoAll: c.memoAll, Missing: c.missing}
		if got := p.docOnlyMemo(&c.rule); got != c.want {
			t.Errorf("%s: docOnlyMemo is %v, want %v", c.name, got, c.want)
		}
	}
}
