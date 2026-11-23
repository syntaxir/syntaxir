package libgo

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

// TestMemoIndexMatchesChain drives random lookups and stores of the three memo
// paths (the table of Nodes, with and without budget controls, and the typed
// memo, likewise) against a plain map. The keys cluster on few positions, so
// the chains pass memoScan and the index is built, extended and used for
// replacement.
func TestMemoIndexMatchesChain(t *testing.T) {
	envs := [][]any{nil, {1}, {int64(1)}, {"a"}, {"ab"}, {true}, {false}, {0.0}, {1.5}, {nil}, {[2]int{1, 2}}, {&struct{}{}}}
	for i := 0; i < 40; i++ {
		envs = append(envs, []any{i, fmt.Sprint("s", i%7)})
	}
	envKey := func(env []any) string {
		s := ""
		for _, v := range env {
			if _, ok := v.(*struct{}); ok {
				s += fmt.Sprintf("%p|", v)
			} else {
				s += fmt.Sprintf("%T:%v|", v, v)
			}
		}
		return s
	}
	type kind struct {
		name     string
		budgeted bool
		typed    bool
	}
	for _, k := range []kind{{"nodes", false, false}, {"nodes budgeted", true, false}, {"typed", false, true}, {"typed budgeted", true, true}} {
		t.Run(k.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(1))
			p := &Parser{}
			if k.budgeted {
				p.Control = newBudgetControl(sxirruntime.Limits{Memory: 1 << 40})
			}
			tp := &TParser{Parser: p}
			var table *memoTable = &p.memo
			type key struct {
				rule, pos, min int
				env            string
			}
			want := map[key]any{}
			for step := 0; step < 20000; step++ {
				env := envs[rng.Intn(len(envs))]
				rule, pos, min := rng.Intn(4), rng.Intn(5), rng.Intn(2)
				wk := key{rule, pos, min, envKey(env)}
				var got any
				if k.typed {
					if e := tp.memoGet(rule, pos, min, env, false); e != nil {
						got = e
					}
				} else if e := table.getAny(p, rule, pos, min, env); e != nil {
					got = e
				}
				if got != want[wk] {
					t.Fatalf("step %d: get %+v = %p, want %p", step, wk, got, want[wk])
				}
				if rng.Intn(3) == 0 {
					continue
				}
				if k.typed {
					e := new(tmemoEntry)
					if got != nil && rng.Intn(5) == 0 {
						e = got.(*tmemoEntry)
					}
					tp.memoPut(rule, pos, min, env, false, e)
					want[wk] = e
				} else {
					e := table.alloc()
					if got != nil && rng.Intn(5) == 0 {
						e = got.(*memoEntry)
					}
					table.putAny(p, rule, pos, min, env, e)
					want[wk] = e
				}
			}
			n := 0
			if k.typed {
				for _, head := range tp.memoSlots {
					for e := head; e != nil; e = e.next {
						n++
					}
				}
				if len(tp.memoIndexes) == 0 {
					t.Fatal("no position was indexed")
				}
			} else {
				for _, head := range table.slots {
					for e := head; e != nil; e = e.next {
						n++
					}
				}
				if len(table.index) == 0 {
					t.Fatal("no position was indexed")
				}
			}
			if n != len(want) {
				t.Fatalf("chains hold %d entries, want %d", n, len(want))
			}
		})
	}
}

func (t *memoTable) getAny(p *Parser, rule, pos, min int, env []any) *memoEntry {
	if p.Control != nil {
		return p.memoGetControlled(rule, pos, min, env, false)
	}
	return t.get(rule, pos, min, env)
}

func (t *memoTable) putAny(p *Parser, rule, pos, min int, env []any, e *memoEntry) {
	if p.Control != nil {
		p.memoPutControlled(rule, pos, min, env, false, e)
		return
	}
	t.put(rule, pos, min, env, e, nil)
}

// TestMemoIndexStreamPrune checks that pruning a stream's memo keeps the
// indexes of the positions that remain, at their new offsets.
func TestMemoIndexStreamPrune(t *testing.T) {
	p := &Parser{Stream: &streamInput{}}
	for pos := 0; pos < 6; pos++ {
		for i := 0; i < 40; i++ {
			p.memo.put(1, pos, 0, []any{i}, p.memo.alloc(), nil)
		}
		p.memo.get(1, pos, 0, []any{-1}) // builds the index
	}
	p.pruneStreamMemo(3)
	if len(p.memo.index) != 3 {
		t.Fatalf("%d indexes after the prune, want 3", len(p.memo.index))
	}
	for pos := 0; pos < 3; pos++ {
		for i := 0; i < 40; i++ {
			if p.memo.get(1, pos, 0, []any{i}) == nil {
				t.Fatalf("entry %d at position %d lost by the prune", i, pos)
			}
		}
	}
}

// TestDocOnlyLookupComparesAssignments checks the lookup of a rule that only a Document parse memoizes:
// a key whose string the same assignment bound is a hit, in other memory too, and one that another
// assignment bound is a miss, with the same bytes too; initial values, which no assignment bound, are
// compared as values. The lookup of any other rule finds the equal string.
func TestDocOnlyLookupComparesAssignments(t *testing.T) {
	big := strings.Repeat("x", 1<<16)
	clone := strings.Clone(big)
	p := &Parser{Control: newBudgetControl(sxirruntime.Limits{Memory: 1 << 40})}
	e := p.memo.alloc()
	p.memoPutControlled(1, 0, 0, []any{assigned{s: big, stamp: 1}}, true, e)
	if got := p.memoGetControlled(1, 0, 0, []any{assigned{s: clone, stamp: 1}}, true); got != e {
		t.Errorf("the same assignment: entry %v, want the entry", got == e)
	}
	if got := p.memoGetControlled(1, 0, 0, []any{assigned{s: big, stamp: 2}}, true); got != nil {
		t.Errorf("another assignment of the same bytes: found %v, want a miss", got != nil)
	}
	if got := p.memoGetControlled(1, 0, 0, []any{big}, true); got != nil {
		t.Errorf("the initial value against an assignment: found %v, want a miss", got != nil)
	}
	initial := p.memo.alloc()
	p.memoPutControlled(2, 0, 0, []any{big}, true, initial)
	if got := p.memoGetControlled(2, 0, 0, []any{clone}, true); got != initial {
		t.Errorf("equal initial values: entry %v, want the entry", got == initial)
	}
	// The lookup of any other rule finds the key, for a string of any length as for a value that is
	// not a string.
	for _, key := range []any{int64(7), "x", big} {
		q := &Parser{Control: newBudgetControl(sxirruntime.Limits{Memory: 1 << 40})}
		x := q.memo.alloc()
		q.memoPutControlled(1, 0, 0, []any{key}, false, x)
		if got := q.memoGetControlled(1, 0, 0, []any{key}, false); got != x {
			t.Errorf("key %T: entry %v, want the entry", key, got == x)
		}
	}
	other := p.memo.alloc()
	p.memoPutControlled(3, 0, 0, []any{big}, false, other)
	if got := p.memoGetControlled(3, 0, 0, []any{clone}, false); got != other {
		t.Errorf("a rule a fresh parse memoizes: found %v, want the entry", got == other)
	}
}
