package libgo

import (
	"errors"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

// The fuel counter holds the count of units and the comparison with the check count in one field
// (Parser.Fuel): a charge takes from it, UnitsNow returns the count, and a check is due exactly when
// the count passes UnitsCheck.
func TestFuelHoldsTheCountAndTheCheck(t *testing.T) {
	const limit = 1000
	o := prepareOptions(parseOptions{controls: true, limits: sxirruntime.Limits{Compute: limit}}, 0, false)
	p := &Parser{}
	p.startUnits(o)
	if p.UnitsNow() != 0 || p.Fuel != limit {
		t.Fatalf("a started counter holds %d units and %d fuel, want 0 and %d", p.UnitsNow(), p.Fuel, limit)
	}
	for i := uint64(1); i <= limit; i++ {
		p.Fuel--
		if got := p.UnitsNow(); got != i {
			t.Fatalf("after %d charges the count is %d", i, got)
		}
		if p.Fuel < 0 {
			t.Fatalf("a check is due at %d units, which do not pass the check count %d", i, p.UnitsCheck)
		}
	}
	p.Fuel-- // the unit that passes the limit
	if p.Fuel >= 0 {
		t.Fatalf("no check is due at %d units, which pass the check count %d", p.UnitsNow(), p.UnitsCheck)
	}
	var err error
	func() {
		defer func() {
			if x := recover(); x != nil {
				f, ok := x.(fatal)
				if !ok {
					panic(x)
				}
				err = f.err
			}
		}()
		p.PollUnits()
	}()
	var le *sxirruntime.LimitError
	if !errors.As(err, &le) || le.Used != limit+1 || le.Limit != limit {
		t.Fatalf("the check stops with %v, want a compute limit error at %d units", err, limit+1)
	}
}

// A poll below the limit (an observation of the context or the memory count) keeps the count and moves
// the check count: the fuel is set again from the units counted, whatever it was.
func TestFuelKeepsTheCountAcrossChecks(t *testing.T) {
	o := prepareOptions(parseOptions{controls: true, limits: sxirruntime.Limits{Compute: 1 << 20}}, 0, false)
	p := &Parser{}
	p.startUnits(o)
	p.Fuel -= 777
	for _, check := range []uint64{900, 5000, 1 << 40} {
		p.setFuel(p.UnitsNow(), check)
		if got := p.UnitsNow(); got != 777 {
			t.Fatalf("a check count of %d changed the count to %d", check, got)
		}
		if p.Fuel != int64(check-777) {
			t.Fatalf("a check count of %d leaves %d fuel, want %d", check, p.Fuel, int64(check-777))
		}
	}
	// A count past the check count leaves negative fuel: a check is due.
	p.setFuel(p.UnitsNow(), 700)
	if p.Fuel >= 0 || p.UnitsNow() != 777 {
		t.Fatalf("a count of %d past the check count 700 leaves %d fuel", p.UnitsNow(), p.Fuel)
	}
	// A parse that checks nothing keeps counting without a due check, and the clamp keeps its fuel positive.
	p.setFuel(p.UnitsNow(), noUnitsCheck)
	if p.Fuel <= 0 || p.UnitsNow() != 777 {
		t.Fatalf("a parse without checks has %d fuel at %d units", p.Fuel, p.UnitsNow())
	}
	// A check count above the largest fuel is clamped, not wrapped into negative fuel.
	p.setFuel(p.UnitsNow(), ^uint64(0))
	if p.UnitsCheck != noUnitsCheck || p.Fuel <= 0 || p.UnitsNow() != 777 {
		t.Fatalf("a check count past the range gives check %d, fuel %d, units %d", p.UnitsCheck, p.Fuel, p.UnitsNow())
	}
}
