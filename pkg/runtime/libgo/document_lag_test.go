package libgo

import (
	"fmt"
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

// The walk that skips the lag test while no entry can lag (mayLag) leaves the chain and the returned link as
// the walk that tests every entry does, at the edge of the edit limit and on both sides of it.
func TestUnlinkLaggedSkipsOnlyWhenNothingLags(t *testing.T) {
	for _, gen := range []uint32{0, pendingEditLimit, pendingEditLimit + 1, 3 * pendingEditLimit} {
		for _, limit := range []int{0, 3, memoScan, -1} {
			build := func() *Parser {
				p := &Parser{gen: gen}
				for i := 0; i < 10; i++ {
					e := p.memo.alloc()
					e.rule, e.vgen = int32(i), uint32(i)*gen/9
					p.memo.put(i, 7, 0, nil, e, nil)
				}
				return p
			}
			rules := func(p *Parser, link **memoEntry) (out []int32, at int) {
				for e := p.memo.slots[p.memo.slot(7)]; e != nil; e = e.next {
					out = append(out, e.rule)
				}
				for e, n := p.memo.slots[p.memo.slot(7)], 0; ; e, n = e.next, n+1 {
					if &p.memo.slots[p.memo.slot(7)] == link {
						return out, 0
					}
					if e == nil {
						return out, -1
					}
					if link == &e.next {
						return out, n + 1
					}
				}
			}
			got, want := build(), build()
			i := got.memo.slot(7)
			gl := got.unlinkLagged(i, &got.memo.slots[i], limit)
			// The reference: the walk that tests every entry.
			wl := &want.memo.slots[i]
			for kept := 0; *wl != nil && kept != limit; {
				e := *wl
				if want.gen-e.vgen <= pendingEditLimit {
					wl, kept = &e.next, kept+1
					continue
				}
				*wl = e.next
			}
			g, ga := rules(got, gl)
			w, wa := rules(want, wl)
			if len(g) != len(w) || ga != wa {
				t.Errorf("gen %d limit %d: chain %v link at %d, want %v at %d", gen, limit, g, ga, w, wa)
			}
			for k := range g {
				if k < len(w) && g[k] != w[k] {
					t.Errorf("gen %d limit %d: chain %v, want %v", gen, limit, g, w)
					break
				}
			}
		}
	}
}

// TestTypedUnlinkLaggedSkipsOnlyWhenNothingLags is the test of the typed memo (TParser.mayLag): the walk
// that skips the lag test leaves the chain and the returned link as the walk that tests every entry does.
func TestTypedUnlinkLaggedSkipsOnlyWhenNothingLags(t *testing.T) {
	chain := func(p *TParser) (out []int32) {
		for e := p.memoSlots[7]; e != nil; e = e.next {
			out = append(out, e.rule)
		}
		return out
	}
	for _, gen := range []uint32{0, pendingEditLimit, pendingEditLimit + 1, 3 * pendingEditLimit} {
		for _, limit := range []int{0, 3, memoScan, -1} {
			build := func() *TParser {
				p := &TParser{Parser: &Parser{gen: gen}, memoSlots: make([]*tmemoEntry, 8)}
				for i := 9; i >= 0; i-- {
					e := &tmemoEntry{rule: int32(i), vgen: uint32(i) * gen / 9}
					e.next, p.memoSlots[7] = p.memoSlots[7], e
				}
				return p
			}
			got, want := build(), build()
			gl := got.unlinkLagged(7, &got.memoSlots[7], limit)
			// The reference: the walk that tests every entry.
			wl := &want.memoSlots[7]
			for kept := 0; *wl != nil && kept != limit; {
				e := *wl
				if want.gen-e.vgen <= pendingEditLimit {
					wl, kept = &e.next, kept+1
					continue
				}
				*wl = e.next
			}
			g, w := chain(got), chain(want)
			if fmt.Sprint(g) != fmt.Sprint(w) {
				t.Errorf("gen %d limit %d: chain %v, want %v", gen, limit, g, w)
			}
			// The links agree when they point at the same position of the chain.
			if (*gl == nil) != (*wl == nil) || (*gl != nil && (*gl).rule != (*wl).rule) {
				t.Errorf("gen %d limit %d: link before rule %v, want %v", gen, limit, *gl, *wl)
			}
			dropped := build()
			dropped.dropLagged(7, &dropped.memoSlots[7])
			ref := build()
			for l := &ref.memoSlots[7]; *l != nil; {
				if ref.gen-(*l).vgen > pendingEditLimit {
					*l = (*l).next
					continue
				}
				l = &(*l).next
			}
			if fmt.Sprint(chain(dropped)) != fmt.Sprint(chain(ref)) {
				t.Errorf("gen %d: dropLagged left %v, want %v", gen, chain(dropped), chain(ref))
			}
		}
	}
}
