package libgo

import (
	"testing"
)

// A lookup treats an entry that lags behind more than pendingEditLimit edits as a miss (advanceEntry), and
// a walk of its chain drops it (unlinkLagged): the entries of earlier texts that a Document retains stay
// bounded by that many edits, in both libraries.
func TestEntriesBeyondTheEditLimitAreMisses(t *testing.T) {
	const epoch = 200
	seen := []uint32{epoch - pendingEditLimit - 1, epoch - pendingEditLimit, epoch}
	want := []bool{false, true, true}

	log := make([]documentEdit, epoch)
	for i := range log {
		log[i] = documentEdit{start: 50, end: 50}
	}
	p := &Parser{edits: log, gen: epoch}
	for i, s := range seen {
		e := p.memo.alloc()
		e.from, e.examined, e.vgen = 3, 3, s
		p.memo.put(i, 3, 0, nil, e, nil)
	}
	for i, w := range want {
		e := p.memoGetControlled(i, 3, 0, nil, false)
		if got := e != nil && (e.vgen == p.gen || p.advanceEntry(e, p.edits)); got != w {
			t.Errorf("entry at %d of %d edits: found %v, want %v", seen[i], epoch, got, w)
		}
	}
	n := 0
	for e := p.memo.slots[p.memo.slot(3)]; e != nil; e = e.next {
		n++
	}
	if n != 2 {
		t.Errorf("%d entries left in the chain, want 2", n)
	}

	tp := &TParser{Parser: &Parser{edits: log, gen: epoch, Control: p.Control, memoryLimited: p.memoryLimited}}
	tp.memoSlots = make([]*tmemoEntry, 4)
	for i, s := range seen {
		e := &tmemoEntry{rule: int32(i), from: 3, examined: 3, vgen: s}
		e.next, tp.memoSlots[3] = tp.memoSlots[3], e
	}
	for i, w := range want {
		e := tp.memoGetControlled(i, 3, 0, nil, false)
		if got := e != nil && (e.vgen == tp.gen || tp.advanceEntry(e, tp.edits)); got != w {
			t.Errorf("typed entry at %d of %d edits: found %v, want %v", seen[i], epoch, got, w)
		}
	}
	n = 0
	for e := tp.memoSlots[3]; e != nil; e = e.next {
		n++
	}
	if n != 2 {
		t.Errorf("%d typed entries left in the chain, want 2", n)
	}
}
