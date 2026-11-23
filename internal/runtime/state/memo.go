package state

import "slices"

// MemoTable is the memo table ((rule, position, binding level) → result). It keeps a linked
// list of entries per position. Positions are offsets from the start of the input; entries at
// positions before base have been discarded (stream parsing). Compared with a Go map, it avoids
// the cost of hashing entries and growing the table, and it can be pruned in position order.
type MemoTable struct {
	Base  int
	Slots []*MemoEntry // list of entries at position base+i (memoGap.slot)
	slab  []MemoEntry  // area for allocating entries in bulk
	// Chunks are the areas slab came from, kept by reset for later parses; used counts those in use.
	Chunks [][]MemoEntry
	used   int
	// Free links retired entries through next. Retiring clears their result
	// references; reuse bounds slab growth in streams and edited Documents.
	Free *MemoEntry
	// Document edits splice positions in and out at the gap (in Document tables, where base is 0):
	// moving it costs only the distance to the next edit.
	memoGap
	// firstCalls records the calls whose memoization a whole-input parse defers (seen.first).
	firstCalls
	// MemBytes counts the bytes of the slots the table has taken into use (budget_memory.go,
	// ChargedBytes). Reset clears it.
	MemBytes uint64
	// Index holds the hash index of each position whose chain has more entries
	// than a lookup walks (memoScan), by position. Splice drops all of them.
	Index map[int]memoIndex[MemoEntry]
	// IndexBytes counts the bytes of the indexes in Index, for the memory limit (ChargedBytes).
	IndexBytes uint64
	// assignments counts the assignments of string variables that the Document parses of the table
	// made (assign); Reset, which drops every entry, clears it.
	assignments uint64
}

// assign returns the stamp of an assignment of a string variable in a Document parse: one that no other
// assignment that an entry of the table may hold in its key has.
func (t *MemoTable) assign() uint64 {
	t.assignments++
	return t.assignments
}

// sparseMemo enables the sparse first-call record (firstCalls.sparse); the reference build
// sxir_reference_sparse_memo disables it.
const sparseMemo = SparseMemo

func NewMemoTable() *MemoTable { return &MemoTable{} }

// Get returns the entry for key k.
func (t *MemoTable) Get(k MemoKey) (*MemoEntry, bool) {
	i := t.slot(k.Pos - t.Base)
	if i < 0 || i >= len(t.Slots) {
		return nil, false
	}
	e, n := t.Slots[i], 0
	for ; e != nil && n < memoScan; e, n = e.Next, n+1 {
		if e.Rule == int32(k.Rule) && e.Min == int32(k.Min) && e.missingSilent == k.MissingSilent && slices.Equal(e.Env, k.Env) {
			return e, true
		}
	}
	if e == nil {
		return nil, false
	}
	ix := t.indexAt(k.Pos, t.Slots[i])
	h, _ := memoKeyHash(int32(k.Rule), int32(k.Min), k.MissingSilent, k.Env)
	_, x := ix.probe(h, func(x *MemoEntry) bool {
		return x.Rule == int32(k.Rule) && x.Min == int32(k.Min) && x.missingSilent == k.MissingSilent && slices.Equal(x.Env, k.Env)
	})
	return x, x != nil
}

// joinStored returns the entry that a store of e keeps under the key of x, the entry stored there:
// the join of the two, which answers every call that either answers (CallBegin). A store never
// narrows the calls that a current entry answers (see "Evaluations of Rules" in
// spec/runtime/parsing.md). x stays if it was computed outside a lookahead and e inside one;
// otherwise e replaces it and takes its present set, to which the caller adds that of e's context
// (CallEnd). Both read no growing result, so each is the result of the call outside every context:
// neither calls a leader of either present set at the position, and the result holds in every
// context whose leaders are in their union. An entry that a lookup would not return, one that lags
// behind an edit it cannot be advanced past (AdvanceEntry), is not current: e replaces it as before.
func (p *Parser) joinStored(x, e *MemoEntry) *MemoEntry {
	if x == e || x.Vgen != p.Gen && !AdvanceEntry(x, p.Edits[:p.Gen]) {
		return e
	}
	if !x.silent && e.silent {
		return x
	}
	e.Present = x.Present
	return e
}

// Put sets the entry for key k to e, replacing any existing entry for the same key.
func (t *MemoTable) Put(k MemoKey, e *MemoEntry) {
	t.put(k, e, nil)
}

// put sets the entry for key k to e, replacing any existing entry for the same key unless p, if
// not nil, keeps that entry (Parser.joinStored), and returns the entry it keeps.
func (t *MemoTable) put(k MemoKey, e *MemoEntry, p *Parser) *MemoEntry {
	i := k.Pos - t.Base
	if i < 0 {
		return e // discarded position
	}
	i = t.slot(i)
	if i >= len(t.Slots) {
		// The slot array is charged by its length, not its capacity, so the count does not
		// depend on the capacity an earlier parse left.
		t.MemBytes += uint64(i+1-len(t.Slots)) * memoSlotBytes
		if i < cap(t.Slots) {
			t.Slots = t.Slots[:i+1]
		} else {
			grown := make([]*MemoEntry, i+1, max(2*cap(t.Slots), i+1, 64))
			copy(grown, t.Slots)
			t.Slots = grown
		}
	}
	link := &t.Slots[i]
	same := func(x *MemoEntry) bool {
		return x.Rule == int32(k.Rule) && x.Min == int32(k.Min) && x.missingSilent == k.MissingSilent && slices.Equal(x.Env, k.Env)
	}
	for n := 0; *link != nil && n < memoScan; link, n = &(*link).Next, n+1 {
		if x := *link; same(x) {
			if p != nil && p.joinStored(x, e) == x {
				return x
			}
			setKey(e, k)
			t.reindex(k.Pos, x, e)
			e.Next = x.Next
			*link = e
			if x != e {
				// A replaced growth seed is no longer GrowState.best: the
				// caller installs its new best before replacing the key.
				t.Retire(x)
			}
			return e
		}
	}
	if *link != nil {
		// A long chain: the index finds the entry to replace, and a walk finds its link.
		ix := t.indexAt(k.Pos, t.Slots[i])
		h, _ := memoKeyHash(int32(k.Rule), int32(k.Min), k.MissingSilent, k.Env)
		slot, x := ix.probe(h, same)
		if x != nil && p != nil && p.joinStored(x, e) == x {
			return x
		}
		setKey(e, k)
		ix[slot] = e
		if x == nil {
			t.IndexBytes += memoIndexEntryBytes
		}
		if x != nil {
			link = &t.Slots[i]
			for *link != x {
				link = &(*link).Next
			}
			e.Next = x.Next
			*link = e
			if x != e {
				t.Retire(x)
			}
			return e
		}
	}
	setKey(e, k)
	e.Next = t.Slots[i]
	t.Slots[i] = e
	return e
}

// setKey records the key k in the entry e.
func setKey(e *MemoEntry, k MemoKey) {
	e.Rule, e.Min, e.Pos, e.Env = int32(k.Rule), int32(k.Min), k.Pos, k.Env
	e.missingSilent = k.MissingSilent
}

// Len returns the number of entries.
func (t *MemoTable) Len() int {
	n := 0
	t.Each(func(*MemoEntry) { n++ })
	return n
}

// Alloc returns a new entry.
func (t *MemoTable) Alloc() *MemoEntry {
	if e := t.Free; e != nil {
		t.Free = e.Next
		e.Next = nil
		return e
	}
	if len(t.slab) == 0 {
		if t.used < len(t.Chunks) {
			t.slab = t.Chunks[t.used]
		} else {
			t.slab = make([]MemoEntry, 256)
			t.Chunks = append(t.Chunks, t.slab)
		}
		t.used++
	}
	e := &t.slab[0]
	t.slab = t.slab[1:]
	return e
}

// ChargedBytes returns the memory count of the table: the chunks in use, whether kept from an
// earlier parse or new, so that the count does not depend on what an earlier parse left, the slots
// it has taken (MemBytes), the record of first calls, and the hash indexes of long chains
// (IndexBytes). The chunks are counted from the state that Alloc already keeps, so that it stays
// inlinable.
func (t *MemoTable) ChargedBytes() uint64 {
	return t.MemBytes + t.firstCalls.chargedBytes() + t.IndexBytes + uint64(t.used)*256*memoBytes
}

// Retire releases all result references and makes an unlinked entry reusable.
// It must not be called on a seed still held by an active GrowState.
func (t *MemoTable) Retire(e *MemoEntry) {
	*e = MemoEntry{Next: t.Free}
	t.Free = e
}

// Reset empties the table for another parse, keeping its memory, and reports whether that was
// worth keeping (it is not when its entries are very many). The record of first calls keeps the
// storage within MaxScratch words (firstCalls.reset).
func (t *MemoTable) Reset() bool {
	if cap(t.Slots) > MaxScratch || len(t.Chunks)*256 > MaxScratch {
		return false
	}
	clear(t.Slots)
	for _, c := range t.Chunks[:t.used] {
		clear(c)
	}
	t.firstCalls.reset(MaxScratch)
	*t = MemoTable{Slots: t.Slots[:0], Chunks: t.Chunks, firstCalls: t.firstCalls}
	return true
}

// Prune discards the entries at positions before pos.
func (t *MemoTable) Prune(pos int) {
	k := pos - t.Base
	if k <= 0 {
		return
	}
	for _, head := range t.Slots[:min(k, len(t.Slots))] {
		for e := head; e != nil; {
			next := e.Next
			// The start rule may still be growing across a commit. Its
			// stack-owned best must survive even after its slot is gone.
			if !e.Growing {
				t.Retire(e)
			}
			e = next
		}
	}
	t.MemBytes -= min(t.MemBytes, uint64(min(k, len(t.Slots)))*memoSlotBytes)
	if k >= len(t.Slots) {
		clear(t.Slots)
		t.Slots = t.Slots[:0]
	} else {
		// Compact the remaining part to the front so no references to discarded entries remain.
		n := copy(t.Slots, t.Slots[k:])
		clear(t.Slots[n:])
		t.Slots = t.Slots[:n]
	}
	for p := range t.Index {
		if p < pos {
			t.dropIndex(p)
		}
	}
	t.Base = pos
}

// Splice adjusts the table to a Document edit that replaced the positions [start, end) with
// delta more positions. Entries elsewhere move with their positions, and advance is applied to
// them when they are next looked up; the entries at the positions [start, end] are decided now:
// advance reports whether each is still valid (one at start kept as it was, or one at end shifted
// by delta). Only tables without pruning (base 0) are spliced.
func (t *MemoTable) Splice(start, end, delta int, advance func(e *MemoEntry) bool) {
	t.Index, t.IndexBytes = nil, 0 // entries move to other positions
	// Detach the entries at [start, end] and keep the valid ones.
	var stay *MemoEntry
	for pos := start; pos <= end; pos++ {
		i := t.slot(pos)
		if i >= len(t.Slots) {
			break
		}
		for e := t.Slots[i]; e != nil; {
			next := e.Next
			if advance(e) {
				e.Next, stay = stay, e
			} else {
				t.Retire(e)
			}
			e = next
		}
		t.Slots[i] = nil
	}
	var added int
	t.Slots, added = moveGap(&t.memoGap, t.Slots, start, end, delta)
	t.MemBytes += uint64(added) * memoSlotBytes
	for e := stay; e != nil; {
		next := e.Next
		i := t.slot(e.Pos)
		e.Next = t.Slots[i]
		t.Slots[i] = e
		e = next
	}
}

// Each passes every entry to f in position order.
func (t *MemoTable) Each(f func(e *MemoEntry)) {
	for _, head := range t.Slots {
		for e := head; e != nil; {
			next := e.Next
			f(e)
			e = next
		}
	}
}
