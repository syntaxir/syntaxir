package libgo

import (
	"context"
	"errors"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

// pollCounter is a context that can be canceled and counts its observations.
type pollCounter struct {
	context.Context
	done     chan struct{}
	polls    int
	cancelAt int
}

func (c *pollCounter) Done() <-chan struct{} { return c.done }
func (c *pollCounter) Err() error {
	c.polls++
	if c.cancelAt > 0 && c.polls >= c.cancelAt {
		return context.Canceled
	}
	return nil
}

// A compute limit or a cancelable context makes the parse controlled, but only a memory limit gives
// the control memory accounting; the compute limit and the context are checked by the counter of
// the parse. A cancelable context without limits is observed by the checks of the counter at the
// interval: a parse that counts units sees the cancellation within an interval of its observation,
// though no limit is ever reached.
func TestUnitsObserveContextWithoutLimits(t *testing.T) {
	if c := newBudgetControl(sxirruntime.Limits{}); c != nil {
		t.Fatal("no limit made a control")
	}
	if c := newBudgetControl(sxirruntime.Limits{Compute: 1 << 20}); c == nil || c.memoryControl() != nil {
		t.Fatal("a compute limit made no control, or one that counts memory")
	}
	ctx := &pollCounter{Context: context.Background(), done: make(chan struct{}), cancelAt: 3}
	o := prepareOptions(parseOptions{controls: true, context: ctx, limits: sxirruntime.Limits{Compute: sxirruntime.Unlimited}}, 0, false)
	if o.control == nil || o.control.memoryControl() != nil {
		t.Fatal("a cancelable context made no control, or one that counts memory")
	}
	p := &Parser{}
	p.startUnits(o)
	if !p.units.observes() {
		t.Fatal("the counter does not observe a cancelable context")
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
		for range 100 * budgetCheckInterval {
			p.unitsSized(1)
		}
	}()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not observed without limits: %v after %d polls", err, ctx.polls)
	}
	if p.UnitsNow() > 3*budgetCheckInterval {
		t.Fatalf("cancellation observed late: %d units counted", p.UnitsNow())
	}
}

// A counter with neither a context nor a memory limit observes nothing: its first check is at the
// compute limit, where it stops the parse with the units counted.
func TestUnitsWithoutContextDoNotPoll(t *testing.T) {
	o := prepareOptions(parseOptions{controls: true, limits: sxirruntime.Limits{Compute: 1 << 20}}, 0, false)
	p := &Parser{}
	p.startUnits(o)
	if p.units.observes() || p.UnitsCheck != 1<<20 {
		t.Fatalf("a counter without context polls: check at %d", p.UnitsCheck)
	}
	var err error
	func() {
		defer func() { err = recover().(fatal).err }()
		for range 1<<20 + 1 {
			p.unitsSized(1)
		}
	}()
	var le *sxirruntime.LimitError
	if !errors.As(err, &le) || le.Kind != sxirruntime.LimitCompute || le.Used != 1<<20+1 || le.Requested != 0 {
		t.Fatalf("limit: %v", err)
	}
}
