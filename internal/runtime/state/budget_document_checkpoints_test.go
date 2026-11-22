//go:build !sxir_budget_controls_reference

package state_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// interruptAt makes the k-th reservation of c the one that observes a canceled context, so that the
// units that the work of a Document reserves can still be told apart by where an interruption
// leaves it, although no Compute limit counts them: the work keeps advancing the poll counter.
func interruptAt(c *state.BudgetControl, k uint64) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Ctx = ctx
	c.SinceCheck = state.BudgetCheckInterval - k
}

func TestBudgetDocumentMemoReplayAtomicReservation(t *testing.T) {
	t.Parallel()
	edits := []state.DocEdit{{Start: 0, End: 1, Delta: 2}, {Start: 0, End: 1, Delta: 3}, {Start: 0, End: 1, Delta: 4}}
	before := state.MemoEntry{Pos: 10, End: 12, From: 10, Examined: 13, Far: 12}
	e := before
	p := statetest.NewBudgetParser(runtime.Limits{Compute: 1 << 40})
	interruptAt(p.Control, 3) // a poll within three units would observe the cancellation
	// Replaying edits on an entry is no event: it counts nothing and reaches no poll.
	if err := statetest.CatchFatal(t, func() { p.AdvanceEntry(&e, edits) }); err != nil {
		t.Fatalf("replay under a canceled context = %v", err)
	}
	if p.Control.Used != 0 {
		t.Fatalf("edit replay counted %d units", p.Control.Used)
	}
	e = before
	p = statetest.NewBudgetParser(runtime.Limits{Compute: 3})
	want := before
	if !state.AdvanceEntry(&want, edits) || !p.AdvanceEntry(&e, edits) || !reflect.DeepEqual(e, want) {
		t.Fatal("completed replay differs from ordinary edit replay")
	}
}

// The search for the old element after the edit and the scan of the elements after it are work of
// the Document, which charges nothing: a limit of one unit does not interrupt them, and the search finds
// the element that starts at the moved position.
func TestBudgetDocumentResyncSearchAndTailCountNothing(t *testing.T) {
	t.Parallel()
	elems := make([]state.RunElem, 513)
	for i := range elems {
		elems[i] = state.RunElem{Start: i * 2, From: 100}
	}
	for _, at := range []int{-1, 0, 1, 6, 510, 1024, 1025} {
		p := statetest.NewBudgetParser(runtime.Limits{Compute: 1})
		r := state.RunState{Old: elems, K: 3, J: -1, Max: -1, Shiftable: true, EditStart: -1, EditEnd: -1}
		p.Pos = at
		got := p.RunResync(&r)
		if want := at >= 6 && at <= 1024 && at%2 == 0; got != want || got && r.J != at/2 {
			t.Fatalf("resync at %d = %v (j %d), want %v", at, got, r.J, want)
		}
		if p.Control.Used != 0 {
			t.Fatalf("resync at %d charged %d", at, p.Control.Used)
		}
	}
	// An element after the found one that examined input before the end of the edit stops the resync.
	p := statetest.NewBudgetParser(runtime.Limits{Compute: 1})
	r := state.RunState{Old: elems, J: -1, Max: -1, Shiftable: true, EditStart: 50, EditEnd: 101}
	p.Pos = 400
	if p.RunResync(&r) || p.Control.Used != 0 {
		t.Fatalf("resynchronized over elements that examined the edit, charging %d", p.Control.Used)
	}
}

// Recording a run is work of the Document: it charges nothing, so no limit interrupts it.
func TestBudgetDocumentRunPublicationCountsNothing(t *testing.T) {
	t.Parallel()
	old := make([]state.RunElem, 128)
	mid := make([]state.RunElem, 1)
	for i := range old {
		old[i].Start = i
	}
	mid[0].Start = 900
	r := state.RunState{Key: state.RunKey{Site: 1}, Old: old, K: 128, J: -1, Mid: mid, Count: 129}
	p := statetest.NewBudgetParser(runtime.Limits{Compute: 1})
	p.Runs = map[state.RunKey]*state.RunRecord{r.Key: {Elems: old}}
	p.RunFinish(&r)
	if p.Control.Used != 0 || len(p.Runs[r.Key].Elems) != 129 || p.Runs[r.Key].Elems[128].Start != 900 {
		t.Fatalf("recording charged %d or differs", p.Control.Used)
	}
}

func TestBudgetDocumentMovementChargesEdges(t *testing.T) {
	t.Parallel()
	child := nodes.Ptr(nodes.Data{Start: 2, End: 3})
	field := nodes.Ptr(nodes.Data{Start: 4, End: 5})
	root := nodes.Ptr(nodes.Data{Start: 1, End: 6, Children: []*value.Node{child}, Fields: valueimpl.Fields{{Name: "field", Value: field}}})
	p := statetest.NewBudgetParser(runtime.Limits{Compute: 3})
	p.Gen = 1
	if err := statetest.CatchFatal(t, func() { p.MoveTree(root, 10) }); !errors.Is(err, runtime.ErrLimit) {
		t.Fatalf("movement interruption = %v", err)
	}
	// Legacy movement is non-transactional, but later unvisited fields remain.
	if nodes.D(root).Start != 11 || nodes.D(child).Start != 12 || nodes.D(field).Start != 4 {
		t.Fatal("movement checkpoints do not precede individual edges")
	}
}

func TestBudgetDocumentRunAssemblyMatchesOrdinarySplice(t *testing.T) {
	for _, controlled := range []bool{false, true} {
		old := make([]state.RunElem, 8)
		for i := range old {
			old[i].Start = i
		}
		r := state.RunState{Key: state.RunKey{Site: 1}, Old: old, K: 2, J: 4, T: 3, Mid: []state.RunElem{{Start: 900}}, Post: []state.RunElem{{Start: 901}, {Start: 902}}, Count: 8}
		p := &state.Parser{Runs: map[state.RunKey]*state.RunRecord{}}
		p.SetTuning(state.Tuning{MinRec: 1}) // Keep the splice fixture small, independent of the recording heuristic.
		if controlled {
			p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 8})
		}
		p.RunFinish(&r)
		var got []int
		for _, el := range p.Runs[r.Key].Elems {
			got = append(got, el.Start)
		}
		if !reflect.DeepEqual(got, []int{0, 1, 900, 4, 5, 6, 901, 902}) {
			t.Fatalf("controlled=%v: %v", controlled, got)
		}
	}
}

func TestBudgetDocumentGrowthCopyReservation(t *testing.T) {
	t.Parallel()
	values := make([]*value.Node, 129)
	for i := range values {
		values[i] = nodes.Ptr(nodes.Data{Start: int32(i)})
	}
	before := append([]*value.Node(nil), values...)
	// Growing storage is no event: it counts nothing under a limit below the values it copies.
	p := statetest.NewBudgetParser(runtime.Limits{Compute: 128})
	if err := statetest.CatchFatal(t, func() { state.GrowSpend(p, values, 1) }); err != nil || p.Control.Used != 0 {
		t.Fatalf("growth = %v, counted %d units", err, p.Control.Used)
	}
	if !reflect.DeepEqual(values, before) {
		t.Fatal("growth changed source references")
	}
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 129})
	got := state.GrowSpend(p, values, 1)
	if len(got) != 129 || cap(got) < 130 || !reflect.DeepEqual(got, values) {
		t.Fatal("growth did not preserve length and values")
	}
}

func TestBudgetDocumentMovementMapCleanupIsCounted(t *testing.T) {
	t.Parallel()
	p := statetest.NewBudgetParser(runtime.Limits{Compute: 1})
	a, b := nodes.Ptr(nodes.Data{Text: "a"}), nodes.Ptr(nodes.Data{Text: "b"})
	p.Moved = map[*value.Node]*value.Node{a: a, b: b}
	p.Shifts = map[uint32]int{1: 3, 2: 4}
	if err := statetest.CatchFatal(t, func() { p.ClearMovementSpend() }); !errors.Is(err, runtime.ErrLimit) {
		t.Fatalf("cleanup interruption = %v", err)
	}
	if len(p.Moved) != 1 || len(p.Shifts) != 2 || nodes.D(a).Text != "a" || nodes.D(b).Text != "b" {
		t.Fatal("cleanup changed pointees or skipped reservations")
	}
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 3})
	p.ClearMovementSpend()
	if len(p.Moved) != 0 || len(p.Shifts) != 0 || p.Control.Used != 3 {
		t.Fatal("cleanup did not finish remaining entries")
	}
}

func TestBudgetDocumentShiftReplayAndSharedEmptyGraph(t *testing.T) {
	t.Parallel()
	p := statetest.NewBudgetParser(runtime.Limits{Compute: 2})
	p.Gen = 4
	p.Edits = []state.DocEdit{{Start: 0, End: 1, Delta: 1}, {Start: 0, End: 1, Delta: 1}, {Start: 0, End: 1, Delta: 1}, {Start: 0, End: 1, Delta: 1}}
	n := nodes.Ptr(nodes.Data{Start: 10, End: 11, Mark: 1})
	if err := statetest.CatchFatal(t, func() { p.ShiftOf(n) }); !errors.Is(err, runtime.ErrLimit) {
		t.Fatalf("shift interruption = %v", err)
	}
	if p.Shifts != nil || nodes.D(n).Start != 10 {
		t.Fatal("interrupted shift replay published state")
	}
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 3})
	if got := p.ShiftOf(n); got != 3 || p.Shifts[1] != 3 {
		t.Fatalf("shift = %d, cache=%v", got, p.Shifts)
	}
	empty := nodes.Ptr(nodes.Data{Start: 0, End: 0})
	leaf := nodes.Ptr(nodes.Data{Start: 3, End: 4})
	root := nodes.Ptr(nodes.Data{Start: 0, End: 0, Children: []*value.Node{empty, leaf}, Fields: valueimpl.Fields{{Name: "empty", Value: empty}, {Name: "leaf", Value: leaf}}})
	clone := root.Clone()
	history := codegentest.ResultJSON(clone, nil)
	p = statetest.NewBudgetParser(runtime.Limits{Compute: 32})
	p.Gen = 1
	p.Moved = map[*value.Node]*value.Node{}
	got := p.MoveEmpty(root, 2)
	if got == root || nodes.D(root).Start != 0 || nodes.D(empty).Start != 0 || nodes.D(got).Children[0] != nodes.D(got).Fields[0].Value.(*value.Node) {
		t.Fatal("empty shared nodes were mutated or duplicated")
	}
	if nodes.D(leaf).Start != 5 || nodes.D(got).Children[1] != nodes.D(got).Fields[1].Value.(*value.Node) {
		t.Fatal("shared non-empty descendant was moved more than once")
	}
	if codegentest.ResultJSON(clone, nil) != history {
		t.Fatal("movement changed historical Clone")
	}
}

func TestBudgetDocumentStaleMemoCallReplayIsBounded(t *testing.T) {
	t.Parallel()
	p := &state.Parser{Memo: state.NewMemoTable(), Pos: 19, Gen: 3, Edits: []state.DocEdit{{Start: 0, End: 1, Delta: 2}, {Start: 0, End: 1, Delta: 3}, {Start: 0, End: 1, Delta: 4}}}
	r := &state.Rule{ID: 1}
	e := &state.MemoEntry{Ok: true, End: 12, From: 10, Examined: 13, Far: 12}
	p.Memo.Put(state.MemoKey{Rule: 1, Pos: 19}, e)
	e.Pos = 10 // A shifted slot can retain older value coordinates until replay.
	before := *e
	// The replay of the three edits is no event, and the call composition counts the call before
	// CallBegin (CallCharge), so the hit counts nothing here and reaches no poll.
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1 << 40})
	interruptAt(p.Control, 3)
	var ok, hit bool
	if err := statetest.CatchFatal(t, func() { _, ok, hit = p.CallBegin(r, 0, &state.CallState{}) }); err != nil || p.Control.Used != 0 {
		t.Fatalf("stale hit under a canceled context = %v, counted %d units", err, p.Control.Used)
	}
	if !ok || !hit || p.Pos != 21 || e.Vgen != 3 || e.Gen != 3 {
		t.Fatalf("stale hit: ok=%v hit=%v pos=%d entry=%+v", ok, hit, p.Pos, e)
	}
	*e, p.Pos = before, 19
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1})
	_, ok, hit = p.CallBegin(r, 0, &state.CallState{})
	if !ok || !hit || p.Pos != 21 || e.Vgen != 3 || e.Gen != 3 {
		t.Fatalf("completed stale hit: ok=%v hit=%v pos=%d entry=%+v", ok, hit, p.Pos, e)
	}
}

func TestBudgetDocumentGrowthRejectsNegativeCapacity(t *testing.T) {
	t.Parallel()
	panicOf := func(run func()) (got any) { defer func() { got = recover() }(); run(); return nil }
	values := []int{1}
	want := panicOf(func() { _ = slices.Grow(values, -1) })
	for _, p := range []*state.Parser{{}, statetest.NewBudgetParser(runtime.Limits{Compute: 1})} {
		got := panicOf(func() { state.GrowSpend(p, values, -1) })
		if got == nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("negative capacity panic=%v, want=%v", got, want)
		}
		if p.Control != nil && p.Control.Used != 0 {
			t.Fatal("invalid growth consumed spend")
		}
	}
}
