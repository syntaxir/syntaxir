package state

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// TestStreamCommitWindow pins when a commit prunes and what the pruning leaves: it waits for a whole
// window past the last pruning, and everything from the committed position on stays.
func TestStreamCommitWindow(t *testing.T) {
	p := NewControlledStreamParser(&SharedProgram{}, strings.NewReader(strings.Repeat("a", 4096)), input.CodePoints, NewBudgetControl(nil, runtime.Limits{Compute: 1 << 40}))
	p.Fill(4096)
	at := func(pos int) bool {
		_, ok := p.MemoGet(MemoKey{Rule: 1, Pos: pos})
		return ok
	}
	for _, pos := range []int{1023, 1024, 2047, 2048} {
		p.MemoPut(MemoKey{Rule: 1, Pos: pos}, p.Memo.Alloc())
	}
	for _, step := range []struct {
		pos, floor int
		gone, kept []int
	}{
		{1023, 0, nil, []int{1023, 1024}},
		{1024, 1024, []int{1023}, []int{1024, 2047}},
		{2047, 1024, nil, []int{1024, 2047}},
		{2048, 2048, []int{1024, 2047}, []int{2048}},
	} {
		p.Commit(step.pos)
		if p.Memo.Base != step.floor {
			t.Errorf("after a commit at %d the floor is %d, not %d", step.pos, p.Memo.Base, step.floor)
		}
		for _, pos := range step.gone {
			if at(pos) {
				t.Errorf("after a commit at %d the memo holds position %d", step.pos, pos)
			}
		}
		for _, pos := range step.kept {
			if !at(pos) {
				t.Errorf("after a commit at %d the memo lost position %d", step.pos, pos)
			}
		}
	}
}

// TestStreamCommitDropsRecordsBeforeItsPosition pins the position of the records a pruning commit
// drops: a repetition may still resume from a record at the committed position.
func TestStreamCommitDropsRecordsBeforeItsPosition(t *testing.T) {
	p := NewControlledStreamParser(&SharedProgram{}, strings.NewReader(strings.Repeat("a", 2048)), input.CodePoints, nil)
	p.Fill(2048)
	s := &skipSlots{}
	p.skipTables = []*skipSlots{s}
	s.put(1023, nil, skipRecord{state: skipRecorded})
	s.put(1024, nil, skipRecord{state: skipRecorded})
	p.Commit(1024)
	if got := s.get(1023, nil).state; got != skipFresh {
		t.Errorf("the record before the commit has the state %d", got)
	}
	if got := s.get(1024, nil).state; got != skipRecorded {
		t.Errorf("the record at the commit has the state %d", got)
	}
}

// TestSkipRecordOverwriteCountsTheNewRecord pins the count of a record stored over another of the
// same key: the slots count what the new record holds and forget what the old one held.
func TestSkipRecordOverwriteCountsTheNewRecord(t *testing.T) {
	s := &skipSlots{}
	s.put(5, nil, skipRecord{state: skipRecorded, exp: []ExpID{1, 2, 3}})
	s.put(5, nil, skipRecord{state: skipRecorded, exp: []ExpID{4}})
	if r := s.recs[skipKey{pos: 5}]; len(s.recs) != 1 || s.expBytes != r.countedBytes() || len(r.exp) != 1 {
		t.Errorf("the slots hold %d records counting %d bytes; the record stored last holds %d", len(s.recs), s.expBytes, r.countedBytes())
	}
}

// TestStreamCommitCountsWhatMemoHolds pins the error count of the memory limit across a commit:
// a commit that does not prune leaves the lists of recovered errors counted, since the memo entries
// and the record still hold them, and a commit that prunes replaces the count with the bytes
// that the remaining entries and the record reach, each shared list once and a discarded entry's
// list not at all. With a diagnostic limit the errors themselves are counted as well, as the
// commit moves them from the count of errors built.
func TestStreamCommitCountsWhatMemoHolds(t *testing.T) {
	if !BudgetControlsEnabled {
		t.Skip("counts memory with the budget controls, which the reference build leaves out")
	}
	for _, limit := range []int{0, 4} {
		p := NewControlledStreamParser(&SharedProgram{}, strings.NewReader(strings.Repeat("a", 4096)), input.CodePoints,
			NewBudgetControl(nil, runtime.Limits{Compute: 1 << 40, Memory: 1 << 40}))
		p.DiagnosticTail.Limit = limit
		p.Fill(4096)
		built := func(pos ...int) []*value.SyntaxError {
			var errs []*value.SyntaxError
			for _, n := range pos {
				e := &value.SyntaxError{Pos: n}
				p.chargeError(e)
				p.PushRecovered(e)
				errs = append(errs, e)
			}
			return errs
		}
		// put stores a memo entry whose key holds n values, charged as memoEnv charges a key.
		put := func(pos int, e RecEntry, n int) {
			x := p.Memo.Alloc()
			x.Errs = e
			p.chargeMemoHeld(uint64(n) * anyBytes)
			p.MemoPut(MemoKey{Rule: 1, Pos: pos, Env: make([]any, n)}, x)
		}
		listBytes := func(l RecEntry) uint64 { return errListBytes + uint64(cap(l.list.entries))*recEntryBytes }

		// kept is held by a memo entry that survives the prune and by the record; gone by an entry
		// that the prune discards, the record having dropped it as a backtrack does; only the record
		// holds recordOnly.
		e12 := built(1, 2)
		kept := p.recSince(0)
		put(1500, kept, 2)
		built(3, 4)
		gone := p.recSince(1)
		p.Recovered = p.Recovered[:1]
		put(100, gone, 3)
		e56 := built(5, 6)
		recordOnly := p.recSince(1)
		single := RecEntry{}
		single.err = e56[0]
		put(1600, single, 1)
		// skipOnly is held only by a skip record.
		e78 := built(7, 8)
		skipOnly := p.recSince(2)
		p.Recovered = p.Recovered[:2]
		s := &skipSlots{}
		p.skipTables = []*skipSlots{s}
		s.put(1700, nil, skipRecord{state: skipRecorded, errs: &skipOnly})
		keys := 3 * anyBytes // the keys of the entries at 1500 and 1600
		lists := listBytes(kept) + listBytes(recordOnly) + listBytes(skipOnly)
		all := lists + listBytes(gone) + keys + 3*anyBytes
		if p.Control.memoHeld != all {
			t.Fatalf("limit %d: the lists built are counted as %d bytes, want %d", limit, p.Control.memoHeld, all)
		}

		p.Commit(500)
		if p.Control.memoHeld < all {
			t.Errorf("limit %d: a commit that does not prune leaves %d of %d list bytes counted", limit, p.Control.memoHeld, all)
		}

		p.Commit(1100)
		if limit == 0 {
			if p.Control.memoHeld != lists+keys {
				t.Errorf("the prune leaves %d bytes counted, want the %d of the keys and lists the memo, the skip records and the record hold", p.Control.memoHeld, lists+keys)
			}
			continue
		}
		// The commit at 500 settled the errors, which emptied the record: only the memo and the skip
		// records hold the lists kept and skipOnly and the errors that remain, and the errors are
		// counted too.
		want := keys + listBytes(kept) + listBytes(skipOnly) + syntaxErrorSize(e12[0]) + syntaxErrorSize(e12[1]) +
			syntaxErrorSize(e56[0]) + syntaxErrorSize(e78[0]) + syntaxErrorSize(e78[1])
		if p.Control.memoHeld != want {
			t.Errorf("limit %d: the prune leaves %d bytes counted, want %d", limit, p.Control.memoHeld, want)
		}
	}
}

// TestStreamCommitKeepsChunksMemoReferences pins when the memory count returns what a commit lets go
// of. A memo entry keeps the nodes, text and strings of the element it was built in until the memo
// prune discards it, so a commit leaves their bytes counted, and a prune returns them only if no
// entry that remains can reference them: an entry stored before the commit at a position the prune
// keeps (a lookahead past the end of the element) holds them for another window.
func TestStreamCommitKeepsChunksMemoReferences(t *testing.T) {
	if !BudgetControlsEnabled {
		t.Skip("counts memory with the budget controls, which the reference build leaves out")
	}
	p := NewControlledStreamParser(&SharedProgram{}, strings.NewReader(strings.Repeat("a", 4096)), input.CodePoints,
		NewBudgetControl(nil, runtime.Limits{Compute: 1 << 40, Memory: 1 << 40}))
	p.Fill(4096)
	c := p.Control
	// An entry stored at 1500, by a lookahead of the element that ends at 500, before its commit.
	p.MemoPut(MemoKey{Rule: 1, Pos: 1500}, p.Memo.Alloc())
	c.values = 640 // what the element built: copied text and strings
	p.Commit(500)
	if c.values != 0 || c.pending != 640 {
		t.Fatalf("a commit without a prune leaves values %d and pending %d, want 0 and 640", c.values, c.pending)
	}
	// The prune at 1100 discards the entries before it, but the entry at 1500 remains and may
	// reference the element.
	p.Commit(1100)
	if c.pending != 640 {
		t.Errorf("a prune that keeps an entry stored before the commit leaves pending %d, want 640", c.pending)
	}
	// By the prune at 2200 the entry is gone, and no entry stored before the commit remains.
	p.Commit(2200)
	if c.pending != 0 {
		t.Errorf("a prune that leaves no entry stored before the commit leaves pending %d, want 0", c.pending)
	}
}
