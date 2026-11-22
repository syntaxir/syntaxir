package libgo

import (
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

// A lookup treats an entry that lags behind more than pendingEditLimit edits as a miss (advanceEntry), and
// a walk of its chain drops it (unlinkLagged): the entries of earlier texts that a Document retains stay
// bounded by that many edits. The typed memo drops such entries when an edit passes them (splice).
func TestEntriesBeyondTheEditLimitAreMisses(t *testing.T) {
	const epoch = 200
	seen := []uint32{epoch - pendingEditLimit - 1, epoch - pendingEditLimit, epoch}
	want := []bool{false, true, true}

	log := make([]documentEdit, epoch)
	for i := range log {
		log[i] = documentEdit{start: 50, end: 50}
	}
	p := &Parser{edits: log, gen: epoch, Control: newBudgetControl(nil, sxirruntime.Limits{Compute: sxirruntime.Unlimited})}
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

	d := &typedDocumentState{owner: &documentState{epoch: epoch}, meta: map[*tmemoEntry]typedDocumentMeta{}}
	d.slots = make([]*tmemoEntry, 4)
	for i, s := range seen {
		e := &tmemoEntry{rule: int32(i), seen: s}
		e.next, d.slots[3] = d.slots[3], e
		d.meta[e] = typedDocumentMeta{recorded: true}
	}
	d.splice(documentEdit{start: 50, end: 50})
	kept := map[int32]bool{}
	for e := d.slots[3]; e != nil; e = e.next {
		kept[e.rule] = true
	}
	for i, w := range want {
		if kept[int32(i)] != w {
			t.Errorf("typed entry seen at %d of %d edits: kept %v, want %v", seen[i], epoch, kept[int32(i)], w)
		}
	}
	if len(d.meta) != 2 {
		t.Errorf("%d metadata entries kept, want 2", len(d.meta))
	}
}
