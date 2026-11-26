//go:build !sxir_budget_controls_reference

package state_test

import (
	"errors"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// leastLimit returns the least limit, as set by with, under which parse ends without reaching a
// limit: the units that the parse uses, since a parse stops at the first charge beyond its limit.
// Every parse that ends must end with the same error as the parse under the largest limit.
func leastLimit(t *testing.T, with func(n uint64) runtime.Limits, parseFn func(runtime.Limits) error) uint64 {
	t.Helper()
	lo, hi := uint64(1), uint64(1)<<24
	end := parseFn(with(hi))
	var syntax value.SyntaxErrors
	if end != nil && !errors.As(end, &syntax) {
		t.Fatalf("the parse under a limit of %d fails with %v, not a syntax error", hi, end)
	}
	for lo < hi {
		mid := lo + (hi-lo)/2
		err := parseFn(with(mid))
		if errors.Is(err, runtime.ErrLimit) {
			lo = mid + 1
			continue
		}
		if (err == nil) != (end == nil) || err != nil && err.Error() != end.Error() {
			t.Fatalf("the parse ends with %v under a limit of %d and with %v under %d", err, mid, end, uint64(1)<<24)
		}
		hi = mid
	}
	return hi
}

// TestBudgetUnrecordedContextCalls pins the compute and memory of a parse whose left-recursive
// calls run in a context that records no dependencies, as for rules that contain #missing with
// missing tokens, or without the context memo: their results go to no table, so such a call
// allocates no entry and copies no recovered errors. It covers missing tokens in the parse on every
// backend and in recognition, and the parse without the context memo on every backend.
func TestBudgetUnrecordedContextCalls(t *testing.T) {
	t.Parallel()
	const grammar = `rule e = f "+" "a" / f "-" "a" / "a" g
rule f = e ("x" / "y" / "z")?
rule g = ";" / "x" #recover(skip=[b]+)`
	// In missingProg, g may call h, so every rule of the cycle contains #missing.
	missingProg, prog := enginetest.MustCompile(t, grammar+" / h\nrule h = \"q\" #missing"), enginetest.MustCompile(t, grammar)
	type run struct {
		backend   engine.Backend
		recognize bool
		missing   int
		ctxOff    bool
	}
	type usage struct{ compute, memory uint64 }
	want := map[run]usage{
		{engine.Closure, false, 10, false}:           {43, 80532},
		{engine.Closure, true, 10, false}:            {43, 45676},
		{engine.Closure, false, 0, true}:             {43, 82076},
		{engine.Bytecode, false, 0, true}:            {43, 106652},
		{engine.BytecodeIterative, false, 0, true}:   {43, 108300},
		{engine.Bytecode, false, 10, false}:          {43, 105108},
		{engine.BytecodeIterative, false, 10, false}: {43, 106156},
		{engine.Bytecode, true, 10, false}:           {43, 70252},
	}
	for r, w := range want {
		p := prog
		if r.missing != 0 {
			p = missingProg
		}
		parseFn := func(l runtime.Limits) error {
			_, err := p.ParseWith("e", "abb+a-a+a", engine.ParseOptions{Backend: r.backend, Recognize: r.recognize, MissingTokens: r.missing, ContextMemoOff: r.ctxOff, Limits: l})
			return err
		}
		got := usage{
			compute: leastLimit(t, func(n uint64) runtime.Limits { return runtime.Limits{Compute: n} }, parseFn),
			memory:  leastLimit(t, func(n uint64) runtime.Limits { return runtime.Limits{Memory: n} }, parseFn),
		}
		if got != w {
			t.Errorf("%+v: used %+v, want %+v", r, got, w)
		}
	}
}

// TestBudgetCallEndChargesErrorsAfterExpectations pins where a limit interrupts the end of a
// memoized call: the call keeps its expectations (Keep) before it copies its recovered errors into
// its entry, so a limit that the copy reaches leaves the expectations kept and no entry stored.
// The total does not depend on the order; the point of the interruption does. The call counts its
// three entries at once, so every limit below three interrupts it there.
func TestBudgetCallEndChargesErrorsAfterExpectations(t *testing.T) {
	t.Parallel()
	end := func(limit uint64) (err error, kept, stored bool) {
		p := &state.Parser{Memo: state.NewMemoTable(), MemoAll: true, Expectations: true}
		r := &state.Rule{}
		r.ID, r.Memo = 1, true
		st := &state.CallState{}
		p.CallBegin(r, 0, st)
		p.Expect(0, 7)
		p.Recovered = append(p.Recovered, state.ErrEntry(&value.SyntaxError{}), state.ErrEntry(&value.SyntaxError{}), state.ErrEntry(&value.SyntaxError{}))
		p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: limit})
		err = statetest.CatchFatal(t, func() { p.CallEnd(r, st, nil, true) })
		_, stored = p.Memo.Get(state.MemoKey{Rule: r.ID})
		return err, len(p.LastKept) == 1, stored
	}
	keptOnly := 0
	for limit := uint64(1); ; limit++ {
		err, kept, stored := end(limit)
		if err == nil {
			if !kept || !stored {
				t.Fatalf("the call ended under a limit of %d without keeping or storing its result", limit)
			}
			break
		}
		if !errors.Is(err, runtime.ErrLimit) {
			t.Fatalf("limit %d: %v", limit, err)
		}
		if kept && !stored {
			keptOnly++
		}
		if stored && !kept {
			t.Fatalf("limit %d: the entry was stored before the expectations were kept", limit)
		}
	}
	// The three entries that the call takes are counted after Keep and before the store, so each
	// limit below three, 1 and 2, interrupts the call between them.
	if keptOnly != 2 {
		t.Fatalf("%d limits interrupt the call after it keeps its expectations and before it stores its entry, want 2", keptOnly)
	}
}
