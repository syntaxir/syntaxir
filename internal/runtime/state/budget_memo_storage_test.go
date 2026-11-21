//go:build !sxir_budget_controls_reference

package state

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/runtime"
)

func memoStorageParser(t *MemoTable, limits runtime.Limits) *Parser {
	return &Parser{Input: Input{Control: NewBudgetControl(nil, limits)}, Memo: t}
}

func slotPointers(slots []*MemoEntry) []*MemoEntry {
	return append([]*MemoEntry(nil), slots...)
}

func assertSlotPointers(t *testing.T, got, want []*MemoEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slot length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot %d pointer changed: got %p, want %p", i, got[i], want[i])
		}
	}
}

// Growing the slot array of the memo is storage, not cost: a store that grows it counts what a
// store into an existing empty slot counts, keeps the entries stored before and publishes the new one.
func TestBudgetMemoPutSlotGrowthChargesNothing(t *testing.T) {
	table := &MemoTable{Slots: make([]*MemoEntry, 511)}
	for i := range table.Slots {
		table.Slots[i] = &MemoEntry{Pos: i, Rule: int32(i)}
	}
	table.Slots[100] = nil // an empty slot, so the store below walks no chain, as one at 511 does
	oldPointers := slotPointers(table.Slots)
	p := memoStorageParser(table, runtime.Limits{Compute: 1 << 40})
	p.MemoPut(MemoKey{Rule: 7, Pos: 100}, &MemoEntry{})
	store := p.Control.Used
	oldPointers[100] = table.Slots[100]

	newEntry := &MemoEntry{Node: nodes.Ptr(nodes.Data{Text: "stored"})}
	// A store charges nothing (memo.store), so the limit is the smallest one, which admits no unit.
	limit := max(store, 1)
	p.Control = NewBudgetControl(nil, runtime.Limits{Compute: limit})
	if err := catchFatal(t, func() { p.MemoPut(MemoKey{Rule: 7, Pos: 511}, newEntry) }); err != nil {
		t.Fatalf("a store that grows the slots under a limit of %d units: %v", limit, err)
	}
	if used := p.Control.Used; used != store {
		t.Fatalf("a store that grows the slots charged %d units, a store into an empty slot %d", used, store)
	}
	if len(table.Slots) != 512 {
		t.Fatalf("slot length = %d, want 512", len(table.Slots))
	}
	if table.Slots[511] != newEntry {
		t.Fatalf("slot 511 = %p, want the stored entry", table.Slots[511])
	}
	assertSlotPointers(t, table.Slots[:511], oldPointers)
}

func seedSeenTable(words, capacity int) *MemoTable {
	seen := make([]uint64, words, capacity)
	for i := range seen {
		seen[i] = uint64(i+1) * 0x9e3779b97f4a7c15
	}
	return &MemoTable{firstCalls: firstCalls{stride: 1, seen: seen}}
}

// Growing the record of first calls by an allocation charges nothing, as the record does not
// (seen.first), and keeps the words recorded before.
func TestBudgetMemoSeenGrowthChargesNothing(t *testing.T) {
	table := seedSeenTable(512, 512)
	old := append([]uint64(nil), table.seen...)
	p := memoStorageParser(table, runtime.Limits{Compute: 1})
	var deferred bool
	if err := catchFatal(t, func() { deferred = p.Memo.first(512*64, 0) }); err != nil {
		t.Fatalf("a first call that grows the record under a limit of one unit: %v", err)
	}
	if !deferred {
		t.Fatal("first call after seen growth was not deferred")
	}
	if used := p.Control.Used; used != 0 {
		t.Fatalf("a first call that grows the record charged %d units, want 0", used)
	}
	if len(table.seen) <= 512 || table.seen[512]&(1<<0) == 0 {
		t.Fatalf("growth did not publish the requested bit: len=%d word=%#x", len(table.seen), table.seen[512])
	}
	if !reflect.DeepEqual(table.seen[:512], old) {
		t.Fatal("growth changed the words recorded before")
	}
}

// Growing the record into spare capacity that an earlier parse left clears it, and charges
// nothing, whatever the capacity.
func TestBudgetMemoSeenSpareCapacityIsCleared(t *testing.T) {
	table := seedSeenTable(512, 1024)
	backing := table.seen[:cap(table.seen)]
	for i := 512; i < len(backing); i++ {
		backing[i] = ^uint64(0)
	}
	old := append([]uint64(nil), table.seen...)
	p := memoStorageParser(table, runtime.Limits{Compute: 1})
	var deferred bool
	if err := catchFatal(t, func() { deferred = p.Memo.first(512*64, 0) }); err != nil {
		t.Fatalf("a first call that grows into spare capacity under a limit of one unit: %v", err)
	}
	if !deferred {
		t.Fatal("first call after the growth into spare capacity was not deferred")
	}
	if used := p.Control.Used; used != 0 {
		t.Fatalf("a first call that grows into spare capacity charged %d units, want 0", used)
	}
	if table.seen[512] != 1 {
		t.Fatalf("grown seen bit word = %#x, want 1", table.seen[512])
	}
	for i := 513; i < len(table.seen); i++ {
		if table.seen[i] != 0 {
			t.Fatalf("growth left word %d of the spare capacity uncleared: %#x", i, table.seen[i])
		}
	}
	if !reflect.DeepEqual(table.seen[:512], old) {
		t.Fatal("growth changed the words recorded before")
	}
}

// The record of a first call charges no unit, so it reaches no poll: a cancellation that the next
// unit would observe leaves the call to finish and grow the record, and the next unit observes it.
func TestBudgetMemoSeenGrowthReachesNoPoll(t *testing.T) {
	table := seedSeenTable(512, 512)
	old := append([]uint64(nil), table.seen...)
	ctx := newPollingContext(1)
	p := memoStorageParser(table, runtime.Limits{})
	p.Control = NewBudgetControl(ctx, runtime.Limits{})
	// The next unit reaches the next poll.
	p.Control.SinceCheck = BudgetCheckInterval - 1
	if err := catchFatal(t, func() { p.Memo.first(512*64, 0) }); err != nil {
		t.Fatalf("a first call that grows the record before a poll: %v", err)
	}
	if len(table.seen) <= 512 || table.seen[512]&(1<<0) == 0 || !reflect.DeepEqual(table.seen[:512], old) {
		t.Fatal("the first call did not grow the record")
	}
	if err := catchFatal(t, func() { p.Spend() }); !errors.Is(err, context.Canceled) {
		t.Fatalf("the next unit after the first call: error = %v, want context.Canceled", err)
	}
}

func cloneSeenTable(t *MemoTable) *MemoTable {
	return &MemoTable{firstCalls: firstCalls{
		seen:   append([]uint64(nil), t.seen...),
		stride: t.stride,
		calls:  append([]seenCalls(nil), t.calls...),
	}}
}

func compareFirstCallParity(t *testing.T, seed *MemoTable, calls [][2]int) {
	t.Helper()
	want := cloneSeenTable(seed)
	got := cloneSeenTable(seed)
	p := memoStorageParser(got, runtime.Limits{Compute: 100_000})
	for i, call := range calls {
		expected := want.first(call[0], call[1])
		actual := p.Memo.first(call[0], call[1])
		if actual != expected || !reflect.DeepEqual(got.seen, want.seen) || !reflect.DeepEqual(got.calls, want.calls) {
			t.Fatalf("call %d (%d,%d): controlled=%t state=%#v/%#v, raw=%t state=%#v/%#v", i, call[0], call[1], actual, got.calls, got.seen, expected, want.calls, want.seen)
		}
	}
}

func TestBudgetMemoFirstCallMatchesAdaptiveEstimator(t *testing.T) {
	seed := &MemoTable{firstCalls: firstCalls{stride: 4}}
	var calls [][2]int
	for i := 0; i < 48; i++ {
		calls = append(calls, [2]int{0, 1})
	}
	for i := 0; i < 80; i++ {
		calls = append(calls, [2]int{i % 7, i % 4})
	}
	compareFirstCallParity(t, seed, calls)

	// Seed a repeated key just below the signed counter ceiling. The next increment
	// crosses the high bit, so the raw estimator must discount both counters.
	nearCeiling := &MemoTable{firstCalls: firstCalls{
		stride: 4,
		calls:  []seenCalls{{}, {calls: int32(1<<31 - 1)}},
	}}
	nearCeiling.markSeen(3, 1)
	compareFirstCallParity(t, nearCeiling, [][2]int{{3, 1}, {3, 1}})
}
