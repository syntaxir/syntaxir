package libgo

import (
	"math/rand"
	"reflect"
	"slices"
	"testing"

	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// typedRecord is what a test compares of an entry that a lookup would return: its rule as an id, its
// position, and the record that the edits moved.
type typedRecord struct {
	rule, pos, from, examined, end, shift int
}

// eagerSplice is a copying splice of every entry at once, written from the contract of memo.splice:
// an entry that examined only input before the edit stays, one whose examined input starts at or after
// the edit's end moves by its delta unless it is pinned or holds recovered errors, and any other is
// dropped. It applies ed to the records of recs.
func eagerSplice(recs []typedRecord, pinned, errs map[int]bool, ed documentEdit) []typedRecord {
	var out []typedRecord
	for _, r := range recs {
		switch {
		case r.examined <= ed.start:
		case r.from >= ed.end && !pinned[r.rule] && !errs[r.rule]:
			r.pos += ed.delta
			r.from += ed.delta
			r.examined += ed.delta
			r.end += ed.delta
			r.shift += ed.delta
		default:
			continue
		}
		out = append(out, r)
	}
	return out
}

// lazyRecords reads the entries of d that a lookup of the next parse would return after the edits of
// log (advanceTEntry), with their positions.
func lazyRecords(d *typedDocumentState, log []documentEdit) []typedRecord {
	var out []typedRecord
	for i, head := range d.slots {
		pos := i
		if i >= d.gap {
			pos -= d.gapLen
		}
		for e := head; e != nil; e = e.next {
			if advanceTEntry(e, log) {
				out = append(out, typedRecord{int(e.rule), pos, e.from, e.examined, e.end, int(e.shift)})
			}
		}
	}
	return out
}

func sortRecords(recs []typedRecord) {
	slices.SortFunc(recs, func(a, b typedRecord) int { return a.rule - b.rule })
}

// TestTypedSpliceMatchesEagerReference pins the typed Document memo, which applies an edit at once only
// to the entries at its positions and leaves the others to the lookup, to a splice that applies every
// edit to every entry at once: after any sequence of edits, a lookup returns the same entries at the
// same positions with the same record.
func TestTypedSpliceMatchesEagerReference(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for round := 0; round < 500; round++ {
		n := 1 + rng.Intn(12)
		d := &typedDocumentState{slots: make([]*tmemoEntry, n)}
		var want []typedRecord
		pinned, errs := map[int]bool{}, map[int]bool{}
		id := 0
		for pos := range n {
			for k := rng.Intn(4); k > 0; k-- {
				// An entry that examined no input records the empty range at its position.
				r := typedRecord{rule: id, pos: pos, from: pos, examined: pos + rng.Intn(7), end: pos + rng.Intn(3)}
				if pos > 0 && rng.Intn(3) == 0 {
					r.from--
				}
				e := &tmemoEntry{rule: int32(id), from: r.from, examined: r.examined, end: r.end, pinned: rng.Intn(6) == 0}
				if rng.Intn(8) == 0 {
					e.setRecs(recEntry{err: &sxirvalue.SyntaxError{}})
				}
				pinned[id], errs[id] = e.pinned, !e.recs().none()
				e.next, d.slots[pos] = d.slots[pos], e
				want = append(want, r)
				id++
			}
		}
		var log []documentEdit
		for k := 1 + rng.Intn(4); k > 0; k-- {
			start := rng.Intn(n + 3)
			end := start + rng.Intn(4)
			ed := documentEdit{start, end, start - end + rng.Intn(4)}
			log = append(log, ed)
			d.splice(log)
			want = eagerSplice(want, pinned, errs, ed)
		}
		got := lazyRecords(d, log)
		sortRecords(got)
		sortRecords(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round %d edits %v:\n got  %v\n want %v", round, log, got, want)
		}
	}
}
