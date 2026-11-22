package state_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
)

// TestDocumentSeedDependentResultsNotInMemo checks the invariant that lets a Document keep its
// memo table between parses: no entry of the table depends on a growing seed. A result of a rule
// of a cycle that reads a seed is kept only for the growth in progress (context_memo.go); one that
// reads none is stored with the leaders that grew (MemoEntry.Present).
func TestDocumentSeedDependentResultsNotInMemo(t *testing.T) {
	t.Parallel()
	leader := func(id, idx int) *state.Rule {
		r := &state.Rule{}
		r.ID, r.Leader, r.LeaderIndex, r.Cycle = id, true, idx, 1
		return r
	}
	member := func(id int) *state.Rule {
		r := &state.Rule{}
		r.ID, r.Member, r.Cycle = id, true, 1
		return r
	}
	t.Run("a member that reads the seed", func(t *testing.T) {
		p := &state.Parser{Memo: state.NewMemoTable(), MemoAll: true}
		head, m := leader(1, 0), member(2)
		var headCall state.CallState
		p.CallBegin(head, 0, &headCall)
		growth := p.GrowBegin(p.GrowKey(head, 0), head)
		if !p.GrowNextVM(&growth) {
			t.Fatal("the growth did not evaluate its first step")
		}
		var mCall state.CallState
		_, _, hit := p.CallBegin(m, 0, &mCall)
		if hit {
			t.Fatal("fresh member unexpectedly memoized")
		}
		if _, _, hit := p.CallBegin(head, 0, &state.CallState{}); !hit || p.SeedReads != 1 {
			t.Fatal("the member did not read the seed")
		}
		p.CallEnd(m, &mCall, nil, false)
		if e, found := p.Memo.Get(state.MemoKey{Rule: m.ID}); found {
			t.Fatalf("a result that read the growing seed was stored in the memo table: %+v", e)
		}
		p.GrowStepVM(&growth, nil, false)
		if p.GrowNextVM(&growth) {
			t.Fatal("a failed step continued the growth")
		}
		v, ok := p.GrowEndVM(head, &growth)
		p.CallEnd(head, &headCall, v, ok)
		if e, found := p.Memo.Get(state.MemoKey{Rule: head.ID}); !found || e.Present != 0 {
			t.Fatal("the result of the growth was not stored without a present set")
		}
	})
	t.Run("a member that reads no seed", func(t *testing.T) {
		p := &state.Parser{Memo: state.NewMemoTable(), MemoAll: true}
		head, m := leader(1, 0), member(2)
		var headCall state.CallState
		p.CallBegin(head, 0, &headCall)
		growth := p.GrowBegin(p.GrowKey(head, 0), head)
		p.GrowNextVM(&growth)
		var mCall state.CallState
		p.CallBegin(m, 0, &mCall)
		p.CallEnd(m, &mCall, nil, false)
		if e, found := p.Memo.Get(state.MemoKey{Rule: m.ID}); !found || e.Present != 1 {
			t.Fatal("a result that read no seed was not stored with the leader that grew")
		}
		if _, _, hit := p.CallBegin(m, 0, &state.CallState{}); !hit {
			t.Fatal("the result was not reused while the leader grows")
		}
		p.GrowStepVM(&growth, nil, false)
		p.GrowNextVM(&growth)
		v, ok := p.GrowEndVM(head, &growth)
		p.CallEnd(head, &headCall, v, ok)
	})
	t.Run("a nested leader that reads the outer seed", func(t *testing.T) {
		p := &state.Parser{Memo: state.NewMemoTable(), MemoAll: true}
		outer, inner := leader(1, 0), leader(2, 1)
		var outerCall state.CallState
		p.CallBegin(outer, 0, &outerCall)
		outerGrow := p.GrowBegin(p.GrowKey(outer, 0), outer)
		p.GrowNextVM(&outerGrow)
		var innerCall state.CallState
		_, _, hit := p.CallBegin(inner, 0, &innerCall)
		if hit {
			t.Fatal("the inner leader was reused before it grew")
		}
		innerGrow := p.GrowBegin(p.GrowKey(inner, 0), inner)
		if !p.GrowNextVM(&innerGrow) {
			t.Fatal("the inner growth did not evaluate its first step")
		}
		if _, _, hit := p.CallBegin(outer, 0, &state.CallState{}); !hit {
			t.Fatal("outer seed was not read")
		}
		p.GrowStepVM(&innerGrow, nil, false)
		p.GrowNextVM(&innerGrow)
		v, ok := p.GrowEndVM(inner, &innerGrow)
		p.CallEnd(inner, &innerCall, v, ok)
		if _, found := p.Memo.Get(state.MemoKey{Rule: inner.ID}); found {
			t.Fatal("the inner result, which read the outer seed, was stored in the memo table")
		}
		p.GrowStepVM(&outerGrow, nil, false)
		p.GrowNextVM(&outerGrow)
		v, ok = p.GrowEndVM(outer, &outerGrow)
		p.CallEnd(outer, &outerCall, v, ok)
		if e, found := p.Memo.Get(state.MemoKey{Rule: outer.ID}); !found || e.Present != 0 {
			t.Fatal("the outer result was not stored")
		}
	})
}

// memoStepSeedReads grows a leader twice at one position inside the growth of another leader, the
// first time evaluating four steps that each read the growth's seed, and returns the body steps
// that the second growth evaluated and the reads of seeds it counted: that growth takes its steps
// from the table and a jump over them, and evaluates no body.
func memoStepSeedReads(t *testing.T, mutation int) (bodySteps, seedReads int) {
	t.Helper()
	leader := func(id, idx int) *state.Rule {
		r := &state.Rule{}
		r.ID, r.Leader, r.LeaderIndex, r.Cycle = id, true, idx, 1
		return r
	}
	p := &state.Parser{Memo: state.NewMemoTable(), MemoAll: true}
	p.SetTuning(state.Tuning{MemoMut: mutation})
	outer, inner := leader(1, 0), leader(2, 1)
	var outerCall state.CallState
	p.CallBegin(outer, 0, &outerCall)
	outerGrow := p.GrowBegin(p.GrowKey(outer, 0), outer)
	p.GrowNextVM(&outerGrow)
	grow := func(call bool) (steps int) {
		p.Pos = 0
		var innerCall state.CallState
		if call {
			p.CallBegin(inner, 0, &innerCall)
		}
		g := p.GrowBegin(p.GrowKey(inner, 0), inner)
		for p.GrowNextVM(&g) {
			steps++
			p.CallBegin(inner, 0, &state.CallState{})
			p.Pos = 0
			if steps <= 4 {
				p.Pos = steps
			}
			p.GrowStepVM(&g, nil, steps <= 4)
			p.Pos = 0
		}
		v, ok := p.GrowEndVM(inner, &g)
		if call {
			p.CallEnd(inner, &innerCall, v, ok)
		}
		return steps
	}
	if steps := grow(true); steps != 5 {
		t.Fatalf("the first growth evaluated %d body steps, want 5", steps)
	}
	before := p.SeedReads
	bodySteps = grow(false)
	return bodySteps, p.SeedReads - before
}

// TestDocumentSeedReadsOfMemoizedSteps checks that a growth in a context that takes its steps from
// the table or a jump counts each as a read of a seed (Parser.SeedReads), which is all that a
// Document's repetition run records to know that its elements depend on a growth that is open:
// a run recorded from such steps would be resumed where no growth is open. The second growth reads
// no seed in a body, so the counts are the jump and the final memoized step alone. The controls
// drop one of the counts. Document parses of 3000 random grammars of cycles with two repetitions
// showed no difference in the runs recorded or in the results, because a run whose element took a
// jump or a memoized step also evaluated a body that read a seed there; this test holds the count
// where Document parses do not.
func TestDocumentSeedReadsOfMemoizedSteps(t *testing.T) {
	steps, reads := memoStepSeedReads(t, 0)
	if steps != 0 || reads != 2 {
		t.Fatalf("the second growth evaluated %d body steps and counted %d seed reads, want 0 and 2 (a jump and a memoized step)", steps, reads)
	}
	for _, c := range []struct {
		name string
		m    int
	}{{"jump", state.CtxNoJumpSeedRead}, {"step", state.CtxNoStepSeedRead}} {
		t.Run("control without the read of a "+c.name, func(t *testing.T) {
			if steps, reads := memoStepSeedReads(t, c.m); steps != 0 || reads == 2 {
				t.Errorf("the second growth evaluated %d body steps and counted %d seed reads: the mutation changed nothing", steps, reads)
			}
		})
	}
}

// BenchmarkDocumentLeftRecursion measures edits to the last line of a thousand
// direct or indirect left-recursive expressions.
func BenchmarkDocumentLeftRecursion(b *testing.B) {
	for _, kind := range []string{"direct", "indirect", "nested"} {
		src := `rule main = (expr "\n")* $$
rule expr = expr "+" num / num
rule num = [0-9]+ #atomic`
		if kind == "indirect" {
			src = `rule main = (expr "\n")* $$
rule expr = sum / num
rule sum = expr "+" num
rule num = [0-9]+ #atomic`
		}
		if kind == "nested" {
			src = `rule main = (expr "\n")* $$
rule expr = expr "+" term / term
rule term = term "*" num / num
rule num = [0-9]+ #atomic`
		}
		prog, err := entry.Compile(enginetest.MustParse(src), entry.Options{})
		if err != nil {
			b.Fatal(err)
		}
		text := strings.Repeat("1+2+3\n", 1000)
		for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			b.Run(kind+"/"+backend.String(), func(b *testing.B) {
				doc, err := prog.NewDocumentWith("main", text, engine.ParseOptions{Backend: backend})
				if err != nil {
					b.Fatal(err)
				}
				if _, err := doc.Parse(); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				i := 0
				for b.Loop() {
					if err := doc.Edit(len(text)-2, len(text)-1, string(rune('3'+i%2))); err != nil {
						b.Fatal(err)
					}
					i++
					if _, err := doc.Parse(); err != nil {
						b.Fatal(err)
					}
				}
				got, gotErr := doc.Parse()
				want, wantErr := prog.ParseWith("main", doc.Text(), engine.ParseOptions{Backend: backend})
				if codegentest.ResultJSON(got, gotErr) != codegentest.ResultJSON(want, wantErr) {
					b.Fatal("edited Document differs from a fresh parse")
				}
			})
		}
	}
}
