package libgo

import (
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// TestStreamMemoHeldAcrossCommit pins the error count of the memory limit across the memo prune of
// a stream: a commit's release leaves the lists of recovered errors counted, since the memo entries
// and the record still hold them, and a prune replaces the count with the bytes that the remaining
// entries and the record reach, each shared list once and a discarded entry's list not at all. With
// a diagnostic limit the errors themselves are counted as well, as the settling that follows a
// commit moves them from the count of errors built.
func TestStreamMemoHeldAcrossCommit(t *testing.T) {
	for _, limit := range []int{0, 4} {
		p := &Parser{Stream: &streamInput{baseLine: 1, baseCol: 1}}
		p.Stream.tail.limit = limit
		p.setControl(newBudgetControl(sxirruntime.Limits{Memory: 1 << 40}))
		built := func(pos ...int) []*sxirvalue.SyntaxError {
			var errs []*sxirvalue.SyntaxError
			for _, n := range pos {
				e := &sxirvalue.SyntaxError{Pos: n}
				p.chargeError(e)
				p.PushRecovered(e)
				errs = append(errs, e)
			}
			return errs
		}
		// put stores a memo entry whose key holds n values, charged as memoEnv charges a key.
		put := func(pos int, e recEntry, n int) {
			for len(p.memo.slots) <= pos {
				p.memo.slots = append(p.memo.slots, nil)
			}
			p.chargeMemoHeld(uint64(n) * anyBytes)
			p.memo.slots[pos] = &memoEntry{errs: e, env: make([]any, n)}
		}
		listBytes := func(l recEntry) uint64 { return errListBytes + uint64(cap(l.list.entries))*recEntryBytes }
		commit := func(pos int, prune bool) {
			p.streamRelease()
			if prune {
				p.pruneStreamMemo(pos)
			}
			if limit > 0 {
				p.streamSettleErrors()
			}
		}

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
		put(1600, recEntry{err: e56[0]}, 1)
		// skipOnly is held only by a skip record.
		e78 := built(7, 8)
		skipOnly := p.recSince(2)
		p.Recovered = p.Recovered[:2]
		s := &skipSlots{}
		p.skipTables = []*skipSlots{s}
		s.recs = map[skipKey]skipRecord{{pos: 1700}: {state: skipRecorded, errs: &skipOnly}}
		keys := 3 * anyBytes // the keys of the entries at 1500 and 1600
		lists := listBytes(kept) + listBytes(recordOnly) + listBytes(skipOnly)
		all := lists + listBytes(gone) + keys + 3*anyBytes
		if p.Control.memoHeld != all {
			t.Fatalf("limit %d: the lists built are counted as %d bytes, want %d", limit, p.Control.memoHeld, all)
		}

		commit(500, false)
		if p.Control.memoHeld < all {
			t.Errorf("limit %d: a commit that does not prune leaves %d of %d list bytes counted", limit, p.Control.memoHeld, all)
		}

		commit(1100, true)
		if limit == 0 {
			if p.Control.memoHeld != lists+keys {
				t.Errorf("the prune leaves %d bytes counted, want the %d of the keys and lists the memo, the skip records and the record hold", p.Control.memoHeld, lists+keys)
			}
			continue
		}
		// The settling at 500 emptied the record: only the memo and the skip records hold the lists
		// kept and skipOnly and the errors that remain, and the errors are counted too.
		want := keys + listBytes(kept) + listBytes(skipOnly) + syntaxErrorSize(e12[0]) + syntaxErrorSize(e12[1]) +
			syntaxErrorSize(e56[0]) + syntaxErrorSize(e78[0]) + syntaxErrorSize(e78[1])
		if p.Control.memoHeld != want {
			t.Errorf("limit %d: the prune leaves %d bytes counted, want %d", limit, p.Control.memoHeld, want)
		}
	}
}

// TestStreamKeepsChunksMemoReferences pins when the memory count returns what a commit lets go of.
// A memo entry keeps the nodes, text and strings of the element it was built in until the memo
// prune discards it, so a commit leaves their bytes counted, and a prune returns them only if no
// entry that remains can reference them: an entry stored before the commit at a position the prune
// keeps (a lookahead past the end of the element) holds them for another window.
func TestStreamKeepsChunksMemoReferences(t *testing.T) {
	p := &Parser{Stream: &streamInput{baseLine: 1, baseCol: 1}}
	p.setControl(newBudgetControl(sxirruntime.Limits{Memory: 1 << 40}))
	c := p.Control
	// An entry stored at 1500, by a lookahead of the element that ends at 500, before its commit.
	c.noteStored(1500)
	p.memo.slots = make([]*memoEntry, 1501)
	p.memo.slots[1500] = &memoEntry{}
	c.values = 640 // what the element built: copied text and strings
	p.streamRelease()
	if c.values != 0 || c.pending != 640 {
		t.Fatalf("a commit without a prune leaves values %d and pending %d, want 0 and 640", c.values, c.pending)
	}
	// The prune at 1100 discards the entries before it, but the entry at 1500 remains and may
	// reference the element.
	p.pruneStreamMemo(1100)
	if c.pending != 640 {
		t.Errorf("a prune that keeps an entry stored before the commit leaves pending %d, want 640", c.pending)
	}
	// By the prune at 2200 the entry is gone, and no entry stored before the commit remains.
	p.memo.slots = p.memo.slots[:0]
	p.pruneStreamMemo(2200)
	if c.pending != 0 {
		t.Errorf("a prune that leaves no entry stored before the commit leaves pending %d, want 0", c.pending)
	}
}
