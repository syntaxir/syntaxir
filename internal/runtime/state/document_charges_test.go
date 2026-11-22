//go:build !sxir_budget_controls_reference

package state

import (
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// The tests of this file measure the operations of Document reuse at the bounds that they declare in
// internal/ir/code/exec. The operations of runs and of moving values have no reference model, so the
// conformance suites cannot state their quantities; here each worst case is built by hand, and the
// quantities are worked out from the declaration. A driver of the operations (ResumeRepeat, the
// REPEAT instruction) charges its own steps between them.

func docParser() *Parser {
	return &Parser{Input: Input{Control: NewBudgetControl(nil, runtime.Limits{Compute: 1 << 30})}, Memo: NewMemoTable(),
		Runs: map[RunKey]*RunRecord{}}
}

// spent returns the units that f charges on p.
func spent(p *Parser, f func()) uint64 {
	used := p.Control.Used
	f()
	return p.Control.Used - used
}

// TestAdvanceEntryMatchesItsContract runs both printed forms on random entries and logs against the
// contract of memo.splice, written out here edit by edit. The engine reaches only part of these states
// through each form, since an edit decides the entries at its positions at once.
func TestAdvanceEntryMatchesItsContract(t *testing.T) {
	t.Parallel()
	valid, invalid := 0, 0
	for seed := range uint64(20000) {
		rng := rand.New(rand.NewPCG(seed, 66))
		log := make([]DocEdit, rng.IntN(5))
		for i := range log {
			start := rng.IntN(24)
			end := start + rng.IntN(4)
			log[i] = DocEdit{Start: start, End: end, Delta: rng.IntN(5) - 2}
		}
		pos := rng.IntN(24)
		before := MemoEntry{Pos: pos, From: pos - rng.IntN(2), Examined: pos + rng.IntN(6), End: pos + rng.IntN(3), Far: pos + rng.IntN(7),
			Growing: rng.IntN(16) == 0, Positional: rng.IntN(8) == 0, shift: int32(rng.IntN(3)), shifted: rng.IntN(4) == 0,
			Vgen: uint32(rng.IntN(len(log) + 1))}
		if rng.IntN(8) == 0 {
			before.Errs = ErrEntry(&value.SyntaxError{})
		}
		want, charged, ok := before, uint64(0), !before.Growing
		if ok {
			for _, ed := range log[before.Vgen:] {
				charged++
				if want.Examined <= ed.Start {
					continue
				}
				if want.From < ed.End || want.Positional || !want.Errs.None() {
					ok = false
					break
				}
				want.Pos += ed.Delta
				want.End += ed.Delta
				want.From += ed.Delta
				want.Examined += ed.Delta
				want.Far += ed.Delta
				want.shift += int32(ed.Delta)
				want.shifted = want.shifted || ed.Delta != 0
			}
		}
		if ok {
			want.Vgen = uint32(len(log))
			valid++
		} else {
			want = before
			invalid++
		}
		plain := before
		if got := AdvanceEntry(&plain, log); got != ok || !reflect.DeepEqual(plain, want) {
			t.Fatalf("seed %d: plain form: %v %+v, want %v %+v, log %v", seed, got, plain, ok, want, log)
		}
		p := docParser()
		budget := before
		if got := p.AdvanceEntry(&budget, log); got != ok || !reflect.DeepEqual(budget, want) {
			t.Fatalf("seed %d: budget form: %v %+v, want %v %+v, log %v", seed, got, budget, ok, want, log)
		}
		if p.Control.Used != charged {
			t.Fatalf("seed %d: charged %d, want %d", seed, p.Control.Used, charged)
		}
	}
	if valid == 0 || invalid == 0 {
		t.Fatalf("%d valid and %d invalid cases", valid, invalid)
	}
}

func TestMemoLookupChargesItsPendingEdits(t *testing.T) {
	t.Parallel()
	lookup := func(p *Parser, k MemoKey) bool {
		e, found := p.MemoGet(k)
		if found && e.Vgen != p.Gen && !p.AdvanceEntry(e, p.Edits[:p.Gen]) {
			found = false
		}
		return found
	}
	store := func(p *Parser, k MemoKey, from, examined int) {
		e := p.Memo.Alloc()
		*e = MemoEntry{Ok: true, End: k.Pos + 1, From: from, Examined: examined, Gen: p.Gen, Vgen: p.Gen}
		p.MemoPut(k, e)
	}
	edit := func(p *Parser, start, end, delta int) {
		p.Edits = EditMemo(p.Memo, p.Edits, DocEdit{Start: start, End: end, Delta: delta})
		p.Gen = uint32(len(p.Edits))
	}
	t.Run("an entry that stays valid applies each edit once", func(t *testing.T) {
		p := docParser()
		k := MemoKey{Rule: 1, Pos: 10}
		store(p, k, 10, 12)
		for range 3 {
			edit(p, 20, 21, 0)
		}
		// One unit for each pending edit; the call paid for the key (ChargeKey).
		if got := spent(p, func() {
			if !lookup(p, k) {
				t.Fatal("the entry examined only input before the edits")
			}
		}); got != 3 {
			t.Fatalf("first lookup charged %d", got)
		}
		if got := spent(p, func() { lookup(p, k) }); got != 0 {
			t.Fatalf("second lookup charged %d", got)
		}
	})
	t.Run("a held entry charges its pending edits on every lookup", func(t *testing.T) {
		p := docParser()
		k := MemoKey{Rule: 1, Pos: 10}
		store(p, k, 10, 12)
		// The last pending edit decides, so the lookup applies them all and the entry keeps them.
		edit(p, 20, 21, 0)
		edit(p, 20, 21, 0)
		edit(p, 11, 11, 0)
		for range 2 {
			if got := spent(p, func() {
				if lookup(p, k) {
					t.Fatal("the last edit lies in the examined input")
				}
			}); got != 3 {
				t.Fatalf("lookup charged %d", got)
			}
		}
	})
	t.Run("a scan charges nothing for the entries it passes", func(t *testing.T) {
		p := docParser()
		held, live := MemoKey{Rule: 1, Pos: 10}, MemoKey{Rule: 2, Pos: 10}
		store(p, held, 10, 12)
		store(p, live, 10, 11)
		edit(p, 11, 11, 0)
		// The newer entry heads the chain: the scan for the held entry passes it, and a lookup of a live
		// entry behind a held one passes the held one in the same way. Only the edits count.
		if got := spent(p, func() {
			if lookup(p, held) {
				t.Fatal("the edit lies in the examined input")
			}
		}); got != 1 {
			t.Fatalf("lookup of the held entry charged %d", got)
		}
		behind := MemoKey{Rule: 3, Pos: 10}
		store(p, behind, 10, 11)
		edit(p, 11, 11, 0)
		// The live entry is second now, and the held one third: only the two pending edits count.
		if got := spent(p, func() {
			if !lookup(p, live) {
				t.Fatal("the entry examined only input before the edits")
			}
		}); got != 2 {
			t.Fatalf("lookup of the live entry charged %d", got)
		}
	})
	t.Run("an edit charges nothing", func(t *testing.T) {
		p := docParser()
		k := MemoKey{Rule: 1, Pos: 10}
		store(p, k, 10, 12)
		if got := spent(p, func() { edit(p, 10, 12, 5) }); got != 0 {
			t.Fatalf("edit charged %d", got)
		}
	})
	t.Run("a lookup applies at most the limit of pending edits", func(t *testing.T) {
		// At the limit, the lookup applies every pending edit; one more and the entry lags: the lookup
		// removes it, compares nothing, and misses, all without a unit.
		// The specification fixes the limit at 128 edits.
		const limit = 128
		for _, lag := range []int{limit, limit + 1} {
			p := docParser()
			k := MemoKey{Rule: 1, Pos: 10, Env: []any{7}}
			store(p, k, 10, 12)
			for range lag {
				edit(p, 20, 21, 0)
			}
			var found bool
			got := spent(p, func() { found = lookup(p, k) })
			// One unit for each pending edit applied.
			want, wantFound := uint64(lag), true
			if lag > limit {
				want, wantFound = 0, false
			}
			if got != want || found != wantFound {
				t.Fatalf("%d pending edits: charged %d and found %v, want %d and %v", lag, got, found, want, wantFound)
			}
		}
	})
}

// TestMemoChainChargesAtItsBounds checks a lookup and a store on a chain longer than a lookup compares
// before it consults the index of the position: the walk compares 32 entries, the index is built
// once, and a store of a key held beyond the walk passes the chain up to it, all without a unit: the
// call pays for the lookup of its key (budget.chargeKey), however long the chain.
func TestMemoChainChargesAtItsBounds(t *testing.T) {
	t.Parallel()
	// The specification fixes the walk at 32 entries.
	const scan = 32
	const chain = scan + 8
	p := docParser()
	key := func(v int) MemoKey { return MemoKey{Rule: 1, Pos: 5, Env: []any{v}} }
	for v := range chain {
		// A store of a new key walks the chain, and links the entry; the first store that finds more
		// entries than the walk compares builds the index, and later ones probe it.
		got := spent(p, func() {
			e := p.Memo.Alloc()
			*e = MemoEntry{Ok: true, End: 6, From: 5, Examined: 6, Gen: p.Gen, Vgen: p.Gen}
			p.MemoPut(key(v), e)
		})
		if got != 0 {
			t.Fatalf("the store of key %d charged %d, want 0", v, got)
		}
		if built := p.Memo.Index[5] != nil; built != (v > scan) {
			t.Fatalf("after the store of key %d: index built %v, want %v", v, built, v > scan)
		}
	}
	absent := key(-1)
	for range 2 {
		got := spent(p, func() {
			if _, ok := p.MemoGet(absent); ok {
				t.Fatal("found an absent key")
			}
		})
		if got != 0 {
			t.Fatalf("a lookup with the index built charged %d, want 0", got)
		}
	}
	// The chain holds the newest entry first, so the key stored first is the last of the chain.
	got := spent(p, func() {
		e := p.Memo.Alloc()
		*e = MemoEntry{Ok: true, End: 6, From: 5, Examined: 6, Gen: p.Gen, Vgen: p.Gen}
		p.MemoPut(key(0), e)
	})
	if got != 0 {
		t.Fatalf("a store of the last key of the chain charged %d, want 0", got)
	}
	if e, ok := p.MemoGet(key(0)); !ok || e.End != 6 {
		t.Fatal("the store of the last key of the chain left no entry under it")
	}
}

// TestMemoStoreChargesNoSlotGrowth checks that a store charges nothing, also for the slots it adds,
// after edits that move and widen the gap: growing the table is storage, not cost.
func TestMemoStoreChargesNoSlotGrowth(t *testing.T) {
	t.Parallel()
	parseState := exec.Parser()
	store, splice := parseState.Op("memo.store"), parseState.Op("memo.splice")
	grown := 0
	for seed := range uint64(200) {
		rng := rand.New(rand.NewPCG(seed, 65))
		model := ref.New(ref.Config{LazyEdits: true})
		p := docParser()
		for range 60 {
			if rng.IntN(4) == 0 {
				start := rng.IntN(5000)
				end := start + rng.IntN(100)
				delta := rng.IntN(3000) - (end - start)
				model.Call(splice, []any{int64(start), int64(end), int64(delta)})
				p.Edits = EditMemo(p.Memo, p.Edits, DocEdit{Start: start, End: end, Delta: delta})
				p.Gen = uint32(len(p.Edits))
				continue
			}
			pos := rng.IntN(20000)
			args := []any{int64(0), int64(pos), int64(0), int64(0), true, int64(pos), false, nil, nil}
			slots := len(p.Memo.Slots)
			got := spent(p, func() {
				e := p.Memo.Alloc()
				*e = MemoEntry{Ok: true, End: pos, Gen: p.Gen, Vgen: p.Gen}
				p.MemoPut(MemoKey{Pos: pos}, e)
			})
			if got != 0 {
				t.Fatalf("seed %d: a store at %d charged %d, want 0", seed, pos, got)
			}
			if len(p.Memo.Slots) > slots {
				grown++
			}
			model.Call(store, args)
		}
	}
	if grown == 0 {
		t.Fatal("no store grew the slots")
	}
}

// TestMemoChargesMatchTheModel runs random stores, edits and lookups on the engine's lazy memo and on
// the model of a lazy memo, and compares what each lookup and store charges with the bound of its
// declared cost, which the model computes: nothing for a store, and the edits a lookup applies
// (exec.QPending) for a lookup. The
// declared cost is a bound, since a lookup stops at the entry it finds and an entry that fails an edit
// stops there; the charge is equal to it where the model is exact:
//
//   - a lookup of a key that no entry holds, live or dropped, and a store of such a key, pass every entry
//     of the position;
//   - a lookup of the entry at the end of its chain applies every pending edit when the entry stays valid,
//     and the one pending edit that drops it when it has one;
//
// except that the model keeps the edits an entry accounts for as of its last store or lookup, while a
// memo applies an edit at once to the entries at the positions the edit replaces and the one after
// them (memo.splice). For these the model counts the edits since, which the engine does not charge, so
// the declaration is a bound for them by design.
func TestMemoChargesMatchTheModel(t *testing.T) {
	t.Parallel()
	parseState := exec.Parser()
	lookupOp, storeOp, spliceOp, examineOp := parseState.Op("memo.lookup"), parseState.Op("memo.store"), parseState.Op("memo.splice"), parseState.Op("memo.examine")
	const positions, rule = 14, 1
	var absent, valid, held, overstated, stores int
	for seed := range uint64(300) {
		rng := rand.New(rand.NewPCG(seed, 66))
		model := ref.New(ref.Config{LazyEdits: true})
		p := docParser()
		// decided holds the entries that an edit decided at once: the model counts their edits from before.
		decided := map[*MemoEntry]bool{}
		keyOf := func(pos, v int) MemoKey { return MemoKey{Rule: rule, Pos: pos, Env: []any{int64(v)}} }
		argsOf := func(pos, v int) []any { return []any{int64(rule), int64(pos), int64(0), int64(v)} }
		// chain returns the entries of a position in the order a lookup passes them.
		chain := func(pos int) (es []*MemoEntry) {
			table := p.Memo
			i := table.slot(pos - table.Base)
			if i < 0 || i >= len(table.Slots) {
				return nil
			}
			for e := table.Slots[i]; e != nil; e = e.Next {
				es = append(es, e)
			}
			return es
		}
		for range 80 {
			pos, v := rng.IntN(positions), rng.IntN(2)
			args := argsOf(pos, v)
			switch op := rng.IntN(10); {
			case op < 3:
				start := rng.IntN(positions)
				end := start + rng.IntN(4)
				delta := rng.IntN(5) - (end - start)
				model.Call(spliceOp, []any{int64(start), int64(end), int64(delta)})
				p.Edits = EditMemo(p.Memo, p.Edits, DocEdit{Start: start, End: end, Delta: delta})
				p.Gen = uint32(len(p.Edits))
				p.Memo.Each(func(e *MemoEntry) {
					if e.Vgen == p.Gen {
						decided[e] = true
					}
				})
			case op < 6:
				before := pos > 0 && rng.IntN(3) == 0
				from := pos
				if before {
					from--
				}
				examined := pos + 1 + rng.IntN(6)
				present := slices.ContainsFunc(chain(pos), func(e *MemoEntry) bool { return e.Env[0] == any(int64(v)) })
				want := costBound(model, storeOp, args)
				got := spent(p, func() {
					e := p.Memo.Alloc()
					*e = MemoEntry{Ok: true, End: pos + 1, From: from, Examined: examined, Gen: p.Gen, Vgen: p.Gen}
					p.MemoPut(keyOf(pos, v), e)
					delete(decided, e)
				})
				if got > want || !present && got != want {
					t.Fatalf("seed %d: a store at %d of a key that is %v charged %d, the model counts %d", seed, pos, map[bool]string{false: "absent", true: "present"}[present], got, want)
				}
				stores++
				entry := model.Call(storeOp, append(slices.Clone(args), true, int64(pos+1), false, nil, nil))[0]
				model.Call(examineOp, []any{entry, before, int64(examined)})
			default:
				es := chain(pos)
				at := slices.IndexFunc(es, func(e *MemoEntry) bool { return e.Env[0] == any(int64(v)) })
				var vgen uint32
				if at >= 0 {
					vgen = es[at].Vgen
				}
				want := costBound(model, lookupOp, args)
				var found bool
				got := spent(p, func() {
					e, ok := p.MemoGet(keyOf(pos, v))
					found = ok && (e.Vgen == p.Gen || p.AdvanceEntry(e, p.Edits[:p.Gen]))
				})
				if modelFound := model.Call(lookupOp, args)[1].(bool); found != modelFound {
					t.Fatalf("seed %d: a lookup at %d found %v, the model %v", seed, pos, found, modelFound)
				}
				switch {
				case got > want:
					t.Fatalf("seed %d: a lookup at %d charged %d, more than the model counts, %d", seed, pos, got, want)
				case at < 0:
					if got != want {
						t.Fatalf("seed %d: a lookup at %d of an absent key charged %d, the model counts %d", seed, pos, got, want)
					}
					absent++
				case at != len(es)-1:
				case decided[es[at]]:
					if got < want {
						overstated++
					}
				case found:
					if got != want {
						t.Fatalf("seed %d: a lookup at %d of a valid entry charged %d, the model counts %d", seed, pos, got, want)
					}
					valid++
				case p.Gen-vgen == 1:
					if got != want {
						t.Fatalf("seed %d: a lookup at %d of a dropped entry charged %d, the model counts %d", seed, pos, got, want)
					}
					held++
				}
				if found {
					delete(decided, es[at])
				}
			}
		}
	}
	// Each kind of lookup, and the case that is only a bound, occurred.
	if absent == 0 || valid == 0 || held == 0 || overstated == 0 || stores == 0 {
		t.Fatalf("lookups of absent keys %d, of valid entries %d, of dropped entries %d, bounded by design %d, stores %d", absent, valid, held, overstated, stores)
	}
}

// movedBound returns the bound of doc.move for a value of the given nodes and edges, with the given
// edits to replay.
func movedBound(nodes, edges, edits int) uint64 { return uint64(2*(nodes+edges) + edits) }

func TestDocMoveChargesAtItsBound(t *testing.T) {
	t.Parallel()
	entry := func(p *Parser, n *value.Node) *MemoEntry {
		return &MemoEntry{Node: n, shift: 1, shifted: true, Gen: 0}
	}
	moved := func(p *Parser, e *MemoEntry) uint64 { return spent(p, func() { p.moveResult(e) }) }
	edited := func(n int) *Parser {
		p := docParser()
		for range n {
			p.Edits = append(p.Edits, DocEdit{Delta: 1})
		}
		p.Gen = uint32(n)
		return p
	}
	t.Run("an empty root reaches the bound", func(t *testing.T) {
		// One unit for the copy, and one to forget it: the second pass of a copying target.
		p := edited(1)
		n := nodes.Ptr(nodes.Data{Start: 3, End: 3})
		if got := moved(p, entry(p, n)); got != movedBound(1, 0, 0) {
			t.Fatalf("charged %d", got)
		}
	})
	t.Run("a tree of non-empty nodes charges one pass", func(t *testing.T) {
		p := edited(1)
		a, b, f := nodes.Ptr(nodes.Data{Start: 2, End: 3}), nodes.Ptr(nodes.Data{Start: 3, End: 4}), nodes.Ptr(nodes.Data{Start: 4, End: 5})
		n := nodes.Ptr(nodes.Data{Start: 2, End: 5, Children: []*value.Node{a, b}, Fields: valueimpl.Fields{{Name: "f", Value: f}}})
		if got := moved(p, entry(p, n)); got != 7 {
			t.Fatalf("charged %d", got)
		}
	})
	t.Run("a shared empty node charges each reference", func(t *testing.T) {
		// Visits of the root and of three references, three edges, and two copies to forget.
		p := edited(1)
		c := nodes.Ptr(nodes.Data{Start: 3, End: 3})
		n := nodes.Ptr(nodes.Data{Start: 3, End: 3, Children: []*value.Node{c, c, c}})
		got := moved(p, entry(p, n))
		if got != 4+3+2 || got > movedBound(2, 3, 0) {
			t.Fatalf("charged %d", got)
		}
	})
	t.Run("each generation replays its edits once", func(t *testing.T) {
		// Nodes last moved through other results, after 1, 2 and 2 edits of 4: the replays charge 3 and
		// 2, once for the generation, and forgetting the two generations charges one each.
		p := edited(4)
		a := nodes.Ptr(nodes.Data{Start: 2, End: 3, Mark: 1})
		b := nodes.Ptr(nodes.Data{Start: 3, End: 4, Mark: 2})
		c := nodes.Ptr(nodes.Data{Start: 4, End: 5, Mark: 2})
		n := nodes.Ptr(nodes.Data{Start: 2, End: 5, Children: []*value.Node{a, b, c}})
		got := moved(p, entry(p, n))
		if got != 7+3+2+2 || got > movedBound(4, 3, 3+2) {
			t.Fatalf("charged %d", got)
		}
	})
	t.Run("a value with one node per generation stays within the bound", func(t *testing.T) {
		// Each generation has one node and replays one edit: the replay, the node and the unit that
		// forgets the generation use up the node's second unit.
		p := edited(1)
		n := nodes.Ptr(nodes.Data{Start: 2, End: 3})
		p.Edits = append(p.Edits, DocEdit{Delta: 1})
		p.Gen = 2
		nodes.D(n).Mark = 1
		e := entry(p, n)
		if got := moved(p, e); got != 1+1+1 || got > movedBound(1, 0, 1) {
			t.Fatalf("charged %d", got)
		}
	})
	t.Run("the nodes of the entry's generation replay nothing", func(t *testing.T) {
		p := edited(3)
		n := nodes.Ptr(nodes.Data{Start: 2, End: 3, Mark: 1})
		e := &MemoEntry{Node: n, shift: 5, shifted: true, Gen: 1}
		if got := moved(p, e); got != 1 || nodes.D(n).Start != 7 {
			t.Fatalf("charged %d, start %d", got, nodes.D(n).Start)
		}
		if e.Gen != 3 || e.shift != 0 || e.shifted {
			t.Fatalf("the entry is not current: %+v", e)
		}
	})
	t.Run("a root that another result moved stays", func(t *testing.T) {
		p := edited(1)
		n := nodes.Ptr(nodes.Data{Start: 2, End: 3, Mark: 1})
		if got := moved(p, entry(p, n)); got != 0 || nodes.D(n).Start != 2 {
			t.Fatalf("charged %d, start %d", got, nodes.D(n).Start)
		}
	})
	t.Run("a shared node moves once", func(t *testing.T) {
		// The root, three edges, and the shared node once.
		p := edited(1)
		c := nodes.Ptr(nodes.Data{Start: 3, End: 4})
		n := nodes.Ptr(nodes.Data{Start: 2, End: 5, Children: []*value.Node{c, c}, Fields: valueimpl.Fields{{Name: "f", Value: c}}})
		if got := moved(p, entry(p, n)); got != 1+3+1 || nodes.D(c).Start != 4 {
			t.Fatalf("charged %d, start %d", got, nodes.D(c).Start)
		}
		p = edited(1)
		c = nodes.Ptr(nodes.Data{Start: 3, End: 4})
		n = nodes.Ptr(nodes.Data{Start: 2, End: 5, Fields: valueimpl.Fields{{Name: "f", Value: c}, {Name: "g", Value: c}}})
		if got := moved(p, entry(p, n)); got != 1+2+1 || nodes.D(c).Start != 4 {
			t.Fatalf("fields: charged %d, start %d", got, nodes.D(c).Start)
		}
	})
	t.Run("an empty child stays when nothing moves", func(t *testing.T) {
		p := edited(1)
		c := nodes.Ptr(nodes.Data{Start: 3, End: 3})
		n := nodes.Ptr(nodes.Data{Start: 2, End: 5, Children: []*value.Node{c}})
		e := &MemoEntry{Node: n, shifted: true}
		if got := moved(p, e); got != 2 || nodes.D(n).Children[0] != c {
			t.Fatalf("charged %d", got)
		}
	})
	t.Run("an empty node charges its fields", func(t *testing.T) {
		// The copy, one field, and forgetting the copy.
		p := edited(1)
		n := nodes.Ptr(nodes.Data{Start: 3, End: 3, Fields: valueimpl.Fields{{Name: "f", Value: "x"}}})
		if got := moved(p, entry(p, n)); got != 3 || got > movedBound(1, 1, 0) {
			t.Fatalf("charged %d", got)
		}
	})
	t.Run("many copies are dropped, not forgotten one by one", func(t *testing.T) {
		p := edited(1)
		kids := make([]*value.Node, 1025)
		for i := range kids {
			kids[i] = nodes.Ptr(nodes.Data{Start: 3, End: 3})
		}
		n := nodes.Ptr(nodes.Data{Start: 3, End: 4, Children: kids})
		if got := moved(p, entry(p, n)); got != 1+2*1025 {
			t.Fatalf("charged %d", got)
		}
	})
	t.Run("an entry with no value charges nothing", func(t *testing.T) {
		p := edited(1)
		if got := moved(p, entry(p, nil)); got != 0 {
			t.Fatalf("charged %d", got)
		}
	})
}

// oldRun returns n elements of length 2 from position 0, each with the expectations exp.
func oldRun(n int, exp []ExpID) []RunElem {
	elems := make([]RunElem, n)
	for i := range elems {
		s := 2 * i
		elems[i] = RunElem{Start: s, end: s + 2, From: s, examined: s + 2, far: s + 2, exp: exp}
	}
	return elems
}

// mergeUnits returns what merging exp at far charges, on a parser in the state of p.
func mergeUnits(far int, exp []ExpID) uint64 {
	p := docParser()
	return spent(p, func() { p.MergeExpected(far, exp) })
}

// events returns the units that f charges on p that are events of the parse: those outside the free
// regions of the work of the Document.
func events(p *Parser, f func()) uint64 {
	used, doc := p.Control.Used, p.Control.Doc
	f()
	return (p.Control.Used - used) - (p.Control.Doc - doc)
}

// tailWork returns the units of work of the Document that run.tail does to take, after the last of
// the gen edits, an element with the value v of the run that the parse before recorded; v moves in
// place.
func tailWork(edits []DocEdit, gen uint32, v *value.Node) uint64 {
	p := docParser()
	p.Edits, p.Gen = edits, gen
	ed := edits[gen-1]
	r := RunState{Old: oldRun(1, nil), J: 0, Max: -1, EditStart: ed.Start, EditEnd: ed.End, EditDelta: ed.Delta}
	r.Old[0].V = v
	doc := p.Control.Doc
	p.RunTail(&r)
	return p.Control.Doc - doc
}

// The operations of runs count no event (spec/runtime/documents.md, "Work of a Document Parse"): what
// a run reuses is not an event, as the take of a record skips the iterations it stands for, and the
// bookkeeping of the expectations and the examined input of its elements is work of the Document.
func TestRunOperationsChargeNoEvent(t *testing.T) {
	t.Parallel()
	exp := []ExpID{1, 2, 3}
	run := func(old []RunElem, e DocEdit) RunState {
		return RunState{Old: old, J: -1, Max: -1, EditStart: e.Start, EditEnd: e.End, EditDelta: e.Delta}
	}
	t.Run("run.start and run.begin", func(t *testing.T) {
		p := docParser()
		p.LastRuns = map[RunKey]*RunRecord{{Site: 1}: {Elems: oldRun(1000, nil)}}
		p.Edits, p.Gen = []DocEdit{{Start: 100, End: 101}}, 1
		p.LastRuns[RunKey{Site: 1}].gen = 0
		var r RunState
		if got := events(p, func() { p.RunStart(&r, 1, 0, -1, true); p.RunBegin(&r) }); got != 0 || len(r.Old) != 1000 {
			t.Fatalf("charged %d, took over %d elements", got, len(r.Old))
		}
		if _, ok := p.LastRuns[RunKey{Site: 1}]; ok {
			t.Fatal("the record of the parse before can be taken again")
		}
	})
	t.Run("run.prefix", func(t *testing.T) {
		p := docParser()
		r := run(oldRun(2, exp), DocEdit{Start: 2, End: 3})
		if got := events(p, func() {
			if _, ok := p.RunPrefix(&r); !ok {
				t.Fatal("the element examined only input before the edit")
			}
		}); got != 0 {
			t.Fatalf("charged %d", got)
		}
		if p.Farthest != 2 || len(p.Exp)-p.ExpBase != 1 || p.Stats.Reused != 1 || p.Resumed != 1 || p.Pos != 2 {
			t.Fatalf("replayed far %d, %d items, reused %d, resumed %d, at %d", p.Farthest, len(p.Exp)-p.ExpBase, p.Stats.Reused, p.Resumed, p.Pos)
		}
		if got := events(p, func() {
			if _, ok := p.RunPrefix(&r); ok {
				t.Fatal("the element examined the edit")
			}
		}); got != 0 {
			t.Fatalf("end of prefix charged %d", got)
		}
	})
	t.Run("run.resync", func(t *testing.T) {
		for _, at := range []int{0, 511, 1022} {
			p := docParser()
			r := run(oldRun(1023, nil), DocEdit{Start: -1, End: -1})
			r.Shiftable = true
			p.Pos = 2 * at
			if got := events(p, func() {
				if !p.RunResync(&r) || r.J != at {
					t.Fatalf("no resynchronization at %d: j = %d", at, r.J)
				}
			}); got != 0 {
				t.Fatalf("at %d: charged %d", at, got)
			}
		}
	})
	t.Run("run.tail moves the element", func(t *testing.T) {
		p := docParser()
		p.Edits, p.Gen = []DocEdit{{Start: 0, End: 0, Delta: 2}}, 1
		r := run(oldRun(1, exp), p.Edits[0])
		r.J = 0
		r.Old[0].V = nodes.Ptr(nodes.Data{Start: 0, End: 0})
		if got := events(p, func() {
			if _, ok := p.RunTail(&r); !ok {
				t.Fatal("the element is after the edit")
			}
		}); got != 0 {
			t.Fatalf("charged %d", got)
		}
		// The element moves to 2 through 4, so its expectations merge at 4.
		if s := nodes.D(r.Old[0].V).Start; s != 2 || p.Farthest != 4 || p.Pos != 4 {
			t.Fatalf("the empty value is at %d, far %d, position %d", s, p.Farthest, p.Pos)
		}
		if len(p.Moved) != 0 || len(p.Shifts) != 0 {
			t.Fatalf("the move left %d copies and %d shifts", len(p.Moved), len(p.Shifts))
		}
	})
	t.Run("run.tail leaves a value that another result moved", func(t *testing.T) {
		p := docParser()
		p.Edits, p.Gen = []DocEdit{{Start: 0, End: 0, Delta: 2}}, 1
		r := run(oldRun(1, nil), p.Edits[0])
		r.J = 0
		r.Old[0].V = nodes.Ptr(nodes.Data{Start: 2, End: 4, Mark: 1})
		if got := events(p, func() { p.RunTail(&r) }); got != 0 || nodes.D(r.Old[0].V).Start != 2 {
			t.Fatalf("charged %d", got)
		}
		// The value is current, so the take moves nothing: it does no more work of the Document than the
		// take of an element without a value.
		if got, want := tailWork(p.Edits, p.Gen, nodes.Ptr(nodes.Data{Start: 2, End: 4, Mark: 1})), tailWork(p.Edits, p.Gen, nil); got != want {
			t.Fatalf("the take of a current value did %d units of work of the Document, one without a value %d", got, want)
		}
	})
	t.Run("run.tail moves a value by the shift of its run", func(t *testing.T) {
		// The element was recorded in the parse after the second edit, which its nodes, of the parse after
		// the first, already account for: they move by the shift of the run, without reading the log. The
		// move is the root, its edge to its child and the child.
		edits := []DocEdit{{Start: 0, End: 0, Delta: 1}, {Start: 0, End: 0, Delta: 1}, {Start: 0, End: 0, Delta: 2}}
		value := func() *value.Node {
			child := nodes.Ptr(nodes.Data{Start: 10, End: 15, Mark: 1})
			return nodes.Ptr(nodes.Data{Start: 10, End: 20, Mark: 1, Children: []*value.Node{child}})
		}
		v := value()
		got, base := tailWork(edits, 3, v), tailWork(edits, 3, nil)
		if got-base != 3 || nodes.D(v).Start != 12 || nodes.D(nodes.D(v).Children[0]).End != 17 {
			t.Fatalf("the move did %d units of work of the Document, want 3; the value is at %d, its child ends at %d",
				got-base, nodes.D(v).Start, nodes.D(nodes.D(v).Children[0]).End)
		}
	})
	t.Run("run.end records the element and its kept expected items", func(t *testing.T) {
		p := docParser()
		var r RunState
		r.J = -1
		p.LastKept = []ExpID{1, 2, 4}
		p.RunBegin(&r)
		p.Exp = append(p.Exp, 1, 2, 3)
		p.Pos = 2
		if got := events(p, func() { p.RunEnd(&r, true, nil) }); got != 0 {
			t.Fatalf("charged %d", got)
		}
		if len(r.Mid) != 1 || !slices.Equal(r.Mid[0].exp, []ExpID{1, 2, 3}) || r.Count != 1 {
			t.Fatalf("recorded %+v, count %d", r.Mid, r.Count)
		}
	})
	t.Run("run.finish", func(t *testing.T) {
		p := docParser()
		r := RunState{Old: oldRun(100, nil), K: 40, J: 60, T: 30, Mid: oldRun(5, nil), Post: oldRun(7, nil), Count: 82}
		if got := events(p, func() { p.RunFinish(&r) }); got != 0 || len(p.Runs[r.Key].Elems) != 40+5+30+7 {
			t.Fatalf("charged %d", got)
		}
		r = RunState{Old: oldRun(100, nil), K: minRecordedLen - 1, J: -1, Count: minRecordedLen - 1, Key: RunKey{Site: 2}}
		if got := events(p, func() { p.RunFinish(&r) }); got != 0 || p.Runs[r.Key] != nil {
			t.Fatalf("a short run charged %d or was recorded", got)
		}
	})
	t.Run("a resumed run charges only the iteration it evaluates", func(t *testing.T) {
		p := docParser()
		p.Runs[RunKey{Site: 1}] = &RunRecord{Elems: oldRun(1000, nil)}
		evaluated := 0
		rs := &RunSite{Id: 1, Min: 0, Max: -1, Build: true, M: func(p *Parser) (*value.Node, bool) { evaluated++; return nil, false },
			Value: func(_ *Parser, v *value.Node, _ int) *value.Node { return v }}
		var ok bool
		if got := events(p, func() { _, ok = p.ResumeRepeat(rs) }); got != 1 || !ok || evaluated != 1 || p.Pos != 2000 {
			t.Fatalf("charged %d (ok %v, %d evaluated, at %d); want the one iteration that failed", got, ok, evaluated, p.Pos)
		}
	})
}

// costBound returns the bound of the declared cost of a call of op with args in the state of model.
func costBound(model *ref.Model, op *code.Op, args []any) uint64 {
	n := int64(op.Cost.Const)
	for _, term := range op.Cost.Terms {
		n += int64(term.Coef) * model.Quantity(term.Quantity, op, args)
	}
	return uint64(n)
}
