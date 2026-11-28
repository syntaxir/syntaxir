//go:build !sxir_budget_controls_reference

package engine

import (
	"encoding/json"
	"errors"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

func TestBudgetMemoGrowthFailureCleanup(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = expr $$

rule expr = expr "a" / "a"`)
	p := state.AcquirePooledParser(&prog.sharedProgram)
	retained := nodes.Ptr(nodes.Data{Text: "retained", Children: []*value.Node{nodes.Ptr(nodes.Data{Text: "child"})}})
	before, _ := json.Marshal(retained)
	key := state.MemoKey{Rule: prog.ByName["expr"].ID}
	g := p.GrowBegin(key, inlinedRules(t, prog)[prog.ByName["expr"].ID])
	oldSeed := g.Best
	statetest.MemoPrefix(p.Memo, retained)
	oldHead := p.Memo.Slots[0]
	p.Pos, p.Hw = 1, 1
	p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 64})
	p.Control.Used = 64
	// The step of the growth counts its unit before its evaluation begins, so the limit stops it
	// before the memo or the seed changes.
	if err := statetest.CatchFatal(t, func() { p.GrowNextVM(&g) }); !errors.Is(err, runtime.ErrLimit) {
		t.Fatalf("LR step interruption=%v", err)
	}
	if p.Memo.Slots[0] != oldHead {
		t.Fatal("interrupted growth changed memo chain")
	}
	if seed, ok := p.GrowingSeed(key); !ok || seed != oldSeed || g.Best != oldSeed || !oldSeed.Growing {
		t.Fatal("interruption replaced the old seed")
	}
	chunks := append([][]state.MemoEntry(nil), p.Memo.Chunks...)
	p.ReleaseScratch()
	if p.Control != nil || p.Scratch != nil {
		t.Fatal("cleanup retained control/scratch")
	}
	if state.ScratchPools { // the reference build keeps no buffers, so it clears none
		statetest.RequireMemoCleared(t, chunks)
	}
	after, _ := json.Marshal(retained)
	if string(after) != string(before) {
		t.Fatal("cleanup mutated public graph")
	}
}

// TestBudgetGrowthStepBeforeEvaluation checks that a left-recursion leader counts a unit for each
// step of its growth before the evaluation of its body for that step, as the generated parsers do:
// beginning the growth counts nothing (the call of the rule counted its unit), a step that does not
// grow, after which no evaluation follows, counts nothing, and a step with no room left stops the
// parse before its evaluation begins.
func TestBudgetGrowthStepBeforeEvaluation(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = expr $$

rule expr = expr "a" / "a"`)
	key := state.MemoKey{Rule: prog.ByName["expr"].ID}
	retained := nodes.Ptr(nodes.Data{Text: "retained"})
	// parser returns a parser whose budget has room for n more units.
	parser := func(n uint64) *state.Parser {
		p := state.AcquirePooledParser(&prog.sharedProgram)
		p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1 << 20})
		p.Control.Used = 1<<20 - n
		return p
	}

	// The units of a begun growth and of a growing step, with room for all.
	expr := prog.ByName["expr"]
	p := parser(1 << 10)
	g := p.GrowBegin(key, inlinedRules(t, prog)[expr.ID])
	begin := 1<<10 - (p.Control.Limits.Compute - p.Control.Used)
	p.Pos = 1
	used := p.Control.Used
	p.GrowStepVM(&g, retained, true)
	adopted := p.Control.Used - used
	if !p.GrowNextVM(&g) {
		t.Fatal("the seed did not grow")
	}
	step := p.Control.Used - used - adopted
	p.Pos, used = 1, p.Control.Used
	if p.GrowStepVM(&g, retained, true); p.GrowNextVM(&g) || p.Control.Used != used {
		t.Fatalf("a step that does not grow counted %d units, want 0: no evaluation follows it", p.Control.Used-used)
	}
	p.ReleaseScratch()
	if begin != 0 || adopted != 0 || step != 1 {
		t.Fatalf("a growth counts %d units to begin, %d to adopt a step and %d to evaluate the next, want 0, 0 and 1", begin, adopted, step)
	}

	// With no room, the growth begins and publishes its seed, and the next step stops the parse
	// before its evaluation, at one unit above the limit.
	p = parser(0)
	g = p.GrowBegin(key, inlinedRules(t, prog)[expr.ID])
	if _, ok := p.GrowingSeed(key); !ok {
		t.Fatal("a growth begun with no room did not publish its seed")
	}
	p.Pos = 1
	p.GrowStepVM(&g, retained, true)
	pos := p.Pos
	if err := statetest.CatchFatal(t, func() { p.GrowNextVM(&g) }); !errors.Is(err, runtime.ErrLimit) || p.Control.Used != p.Control.Limits.Compute+1 {
		t.Fatalf("growth step with no room: %v used=%d", err, p.Control.Used)
	}
	if p.Pos != pos {
		t.Fatal("a growth step stopped before its evaluation moved the position")
	}
	p.ReleaseScratch()
}
