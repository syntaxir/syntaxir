//go:build !sxir_budget_controls_reference

package state_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// TestStreamCommitFreezesDeliveredElements holds the delivery to its contract: an element is final
// once the callback sees it, so no later step of the parse may extend it in place.
func TestStreamCommitFreezesDeliveredElements(t *testing.T) {
	t.Parallel()
	p := enginetest.MustCompile(t, `rule main = ("a" "b")* #stream $$`)
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				elements := 0
				err := p.ParseStreamWith("main", strings.NewReader("ababab"), func(n *value.Node) error {
					elements++
					if n == nil || nodes.D(n).Kind != valueimpl.SeqKind || nodes.D(n).Fresh {
						t.Errorf("element %d is %+v", elements, n)
					}
					return nil
				}, engine.ParseOptions{Backend: backend, Unit: unit})
				if err != nil || elements != 3 {
					t.Fatalf("elements=%d err=%v", elements, err)
				}
			})
		}
	}
}

// TestStreamCommitForgetsTheDiagnosedCount keeps every recovered error of an element that recovers
// more often than the one before it: a commit that settles the errors must also forget how many of
// them were reported, or the next report starts inside the new element's errors.
func TestStreamCommitForgetsTheDiagnosedCount(t *testing.T) {
	t.Parallel()
	p := enginetest.MustCompile(t, `rule main = item+ #stream $$
rule item = ("a" #recover(skip="x")) ("b" #recover(skip="y"))`)
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				var events []string
				err := p.ParseStreamWith("main", strings.NewReader("ayxy"), func(*value.Node) error {
					events = append(events, "element")
					return nil
				}, engine.ParseOptions{Backend: backend, Unit: unit, DiagnosticLimit: 8, DiagnosticLimitSet: true, DiagnosticCallback: func(*value.SyntaxError) error {
					events = append(events, "diagnostic")
					return nil
				}})
				want := "diagnostic element diagnostic diagnostic element"
				if got := strings.Join(events, " "); got != want {
					t.Fatalf("events %q, want %q", got, want)
				}
				var errs value.SyntaxErrors
				if !errors.As(err, &errs) || len(errs) != 3 {
					t.Fatalf("err=%v", err)
				}
			})
		}
	}
}

// TestStreamCommitBoundsTheErrorScratch pins the size at which a commit stops keeping the scratch of
// the recovered errors: the largest one it reuses, and the next.
func TestStreamCommitBoundsTheErrorScratch(t *testing.T) {
	t.Parallel()
	for capacity, reused := range map[int]bool{16: true, 17: false} {
		p := &state.Parser{Memo: state.NewMemoTable(), Recovered: make([]state.RecEntry, 1, capacity)}
		p.DiagnosticTail.Limit = 1
		p.Commit(1)
		if (p.Recovered != nil) != reused || reused && cap(p.Recovered) != capacity {
			t.Errorf("capacity %d: recovered=%v cap=%d", capacity, p.Recovered, cap(p.Recovered))
		}
	}
}

// TestStreamCommitDiscardsAndReleasesUnderAnyLimit holds the first two steps of a commit to the
// compute units: the discard and the release are no events, so a commit counts no unit and a limit
// that the parse has reached does not stop it before it has discarded the input and released the
// frames.
func TestStreamCommitDiscardsAndReleasesUnderAnyLimit(t *testing.T) {
	t.Parallel()
	p := &state.Parser{Input: state.Input{Unit: sxirinput.Bytes, Eof: true, Bs: []byte(strings.Repeat("a", 256)), BaseLine: 1, BaseCol: 1, Lines: []int{0},
		Control: state.NewBudgetControl(nil, runtime.Limits{Compute: 1})}, Memo: state.NewMemoTable(), FrameSlab: make([]state.Frame, 3)}
	p.Frame = &p.FrameSlab[1]
	got := statetest.CatchPanic(func() { p.Commit(130) })
	// The frame in use is the second of the scratch, so the release keeps it and the one before it. The
	// reference build keeps no frame scratch, which the release leaves alone.
	if got != nil || p.Base != 129 || state.StreamFrameScratch && len(p.FrameSlab) != 2 || p.Control.Used != 0 {
		t.Fatalf("interruption=%v base=%d frames=%d used=%d", got, p.Base, len(p.FrameSlab), p.Control.Used)
	}
}

// TestStreamCommitPrunesAndSettlesUnderAnyLimit holds the last two steps of a commit to the compute
// units: the pruning and the settling of the recovered errors are no events, so a limit that the
// parse has reached does not stop the commit before it has settled the errors.
func TestStreamCommitPrunesAndSettlesUnderAnyLimit(t *testing.T) {
	t.Parallel()
	first := &value.SyntaxError{Pos: 1}
	memo := state.NewMemoTable()
	memo.Slots = make([]*state.MemoEntry, 2)
	p := &state.Parser{Input: state.Input{Base: 1023, Control: state.NewBudgetControl(nil, runtime.Limits{Compute: 1})}, Memo: memo, Recovered: []state.RecEntry{state.ErrEntry(first)}}
	p.DiagnosticTail.Limit = 1
	p.SetDiagnosed(1)
	p.Control.Used = 1
	got := statetest.CatchPanic(func() { p.Commit(1024) })
	if got != nil || len(p.Recovered) != 0 || p.Diagnosed() != 0 || memo.Base == 0 || p.Control.Used != 1 {
		t.Fatalf("interruption=%v recovered=%v diagnosed=%d floor=%d used=%d", got, p.Recovered, p.Diagnosed(), memo.Base, p.Control.Used)
	}
}

// A diagnostic limit needs no callback: each commit still moves the committed errors into the tail
// before it settles them, so that the terminal result counts them all.
func TestStreamCommitFeedsATailWithoutCallback(t *testing.T) {
	t.Parallel()
	p := enginetest.MustCompile(t, `rule main = item+ #stream $$
rule item = "ab" #recover(skip=[^a]+)`)
	const count = 100
	input := strings.Repeat("abX!", count)
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		err := p.ParseStreamWith("main", strings.NewReader(input), func(*value.Node) error { return nil },
			engine.ParseOptions{Backend: backend, DiagnosticLimit: 2})
		var tail *value.TruncatedSyntaxErrors
		if !errors.As(err, &tail) || tail.Total != count || tail.Dropped != count-2 || len(tail.Errors) != 2 {
			t.Fatalf("%v: tail=%+v err=%v", backend, tail, err)
		}
		if got, want := tail.Errors[1].Pos, (count-1)*4+2; got != want {
			t.Fatalf("%v: last error at %d, want %d", backend, got, want)
		}
	}
}

// TestStreamCommitCountsNoUnits holds the operations of a commit whose work depends on more than the
// size of their argument to the compute units: none of them is an event, so each counts nothing, and
// each still does its work.
func TestStreamCommitCountsNoUnits(t *testing.T) {
	t.Parallel()
	budget := func() *state.BudgetControl { return state.NewBudgetControl(nil, runtime.Limits{Compute: 1 << 20}) }
	t.Run("the frames are scanned and cleared", func(t *testing.T) {
		// The frame in use lies outside the scratch, so the scan finds nothing and the clear visits
		// every frame.
		slab := make([]state.Frame, 4)
		for i := range slab {
			slab[i].Vals = make([]*value.Node, 1)
		}
		p := &state.Parser{Input: state.Input{Control: budget()}, Memo: state.NewMemoTable(), FrameSlab: slab, Frame: &state.Frame{}}
		p.Commit(1)
		if p.Control.Used != 0 {
			t.Fatalf("used=%d, want 0", p.Control.Used)
		}
		if state.StreamFrameScratch {
			for i := range slab {
				if slab[i].Vals != nil {
					t.Fatalf("frame %d of the scratch was not cleared", i)
				}
			}
		}
	})
	t.Run("the whole input is scanned for lines", func(t *testing.T) {
		// The input is complete from its first byte, so the line table covers all 3000 bytes, past the
		// 1501 that go; the compaction copies the 1499 that stay.
		p := &state.Parser{Input: state.Input{Unit: sxirinput.Bytes, Eof: true, Bs: []byte(strings.Repeat("a", 3000)), BaseLine: 1, BaseCol: 1, Control: budget()}, Memo: state.NewMemoTable()}
		p.Commit(1502)
		if p.Base != 1501 || p.Control.Used != 0 {
			t.Fatalf("base=%d used=%d", p.Base, p.Control.Used)
		}
	})
	t.Run("the scratch of the errors is cleared", func(t *testing.T) {
		p := &state.Parser{Input: state.Input{Control: budget()}, Memo: state.NewMemoTable(), Recovered: make([]state.RecEntry, 1, 16)}
		p.Recovered[0] = state.ErrEntry(&value.SyntaxError{Pos: 1})
		p.DiagnosticTail.Limit = 1
		p.Commit(1)
		if p.Control.Used != 0 || len(p.Recovered) != 0 || p.Recovered[:1][0] != (state.RecEntry{}) {
			t.Fatalf("used=%d recovered=%v", p.Control.Used, p.Recovered[:1])
		}
	})
}

// TestStreamCancelledContextDeliversNoDiagnostic holds the emission to its first checkpoint: a
// context that is already cancelled stops it before the diagnostic callback runs, as well as before
// the element.
func TestStreamCancelledContextDeliversNoDiagnostic(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	diagnostics, elements := 0, 0
	p := &state.Parser{Input: state.Input{Control: state.NewBudgetControl(ctx, runtime.Limits{})}, Memo: state.NewMemoTable(), Recovered: []state.RecEntry{state.ErrEntry(&value.SyntaxError{Pos: 1})},
		Diagnostic: func(*value.SyntaxError) error { diagnostics++; return nil },
		Emit:       func(*value.Node) error { elements++; return nil }}
	got := statetest.CatchPanic(func() { p.EmitStream(nil, 0) })
	if f, ok := got.(state.Fatal); !ok || f.Err != context.Canceled || diagnostics != 0 || elements != 0 {
		t.Fatalf("panic=%v diagnostics=%d elements=%d", got, diagnostics, elements)
	}
}

// TestStreamDiagnosticCallbackPanicIsTheCallers keeps a panic of the diagnostic callback the caller's,
// as for the callback of the elements: the parse does not report it as a fault of its own.
func TestStreamDiagnosticCallbackPanicIsTheCallers(t *testing.T) {
	t.Parallel()
	p := enginetest.MustCompile(t, `rule main = item+ #stream $$
rule item = "ab" #recover(skip=[^a]+)`)
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, runtimeValue := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/runtime=%t", backend, runtimeValue), func(t *testing.T) {
				var token any = &struct{ phase string }{"diagnostic"}
				if runtimeValue {
					token = &statetest.StreamPanicToken{Phase: "diagnostic"}
				}
				var err error
				got := statetest.CatchPanic(func() {
					err = p.ParseStreamWith("main", strings.NewReader("abX!"), func(*value.Node) error { return nil },
						engine.ParseOptions{Backend: backend, DiagnosticCallback: func(*value.SyntaxError) error { panic(token) }})
				})
				if got != token || err != nil {
					t.Fatalf("panic=%#v err=%v", got, err)
				}
			})
		}
	}
}

// TestStreamDiagnosticCallbackEndsTheEmission holds what follows the diagnostic callback: a
// cancellation in its last call or an error it returns stops the emission before the element, and
// the parse returns it as an error, since the callback is over and the end of the parse is not the
// caller's panic.
func TestStreamDiagnosticCallbackEndsTheEmission(t *testing.T) {
	t.Parallel()
	p := enginetest.MustCompile(t, `rule main = item+ #stream $$
rule item = ("a" #recover(skip="x")) ("b" #recover(skip="y"))`)
	stop := errors.New("stop")
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		for _, c := range []struct {
			name  string
			calls int
			want  error
		}{{"cancel", 2, context.Canceled}, {"error", 1, stop}} {
			t.Run(backend.String()+"/"+c.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				diagnostics, elements := 0, 0
				var err error
				got := statetest.CatchPanic(func() {
					err = p.ParseStreamWith("main", strings.NewReader("xyab"), func(*value.Node) error { elements++; return nil },
						engine.ParseOptions{Backend: backend, Context: ctx, DiagnosticCallback: func(*value.SyntaxError) error {
							diagnostics++
							if c.want == stop {
								return stop
							}
							if diagnostics == c.calls {
								cancel()
							}
							return nil
						}})
				})
				if got != nil || !errors.Is(err, c.want) || diagnostics != c.calls || elements != 0 {
					t.Fatalf("panic=%v err=%v diagnostics=%d elements=%d", got, err, diagnostics, elements)
				}
			})
		}
	}
}

// TestStreamDiagnosticCallbackMarkEndsWithIt keeps the mark of a caller's callback to the callback:
// the interruptions that follow it reach the caller the same with the mark left on, so only a fault
// of the parser after the callback would show it, as the caller's panic.
func TestStreamDiagnosticCallbackMarkEndsWithIt(t *testing.T) {
	t.Parallel()
	for _, want := range []error{nil, errors.New("stop")} {
		p := &state.Parser{Recovered: []state.RecEntry{state.ErrEntry(&value.SyntaxError{Pos: 1})}}
		p.Diagnostic = func(*value.SyntaxError) error {
			if !p.EmitCall {
				t.Error("the callback runs without the mark")
			}
			return want
		}
		if err := p.ReportRecovered(); err != want || p.EmitCall {
			t.Fatalf("err=%v mark=%t, want err %v without the mark", err, p.EmitCall, want)
		}
	}
}
