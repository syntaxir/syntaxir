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
	if err := statetest.CatchFatal(t, func() { p.GrowStepVM(&g, retained, true) }); !errors.Is(err, runtime.ErrLimit) {
		t.Fatalf("LR replacement interruption=%v", err)
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
	statetest.RequireMemoCleared(t, chunks)
	after, _ := json.Marshal(retained)
	if string(after) != string(before) {
		t.Fatal("cleanup mutated public graph")
	}
}

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
	if p.GrowStepVM(&g, retained, true); !p.GrowNext(&g) {
		t.Fatal("the seed did not grow")
	}
	step := p.Control.Used - used
	p.Pos, used = 1, p.Control.Used
	if p.GrowStepVM(&g, retained, true); p.GrowNext(&g) || p.Control.Used != used {
		t.Fatalf("a step that does not grow charged %d units, want 0: no evaluation follows it", p.Control.Used-used)
	}
	p.ReleaseScratch()
	if begin < 1 || step < 1 {
		t.Fatalf("a growth charges %d units to begin and %d to grow, want the step", begin, step)
	}

	// Short of one unit, neither publishes.
	p = parser(begin - 1)
	if err := statetest.CatchFatal(t, func() { p.GrowBegin(key, inlinedRules(t, prog)[expr.ID]) }); !errors.Is(err, runtime.ErrLimit) {
		t.Fatalf("growth begun short of a unit: %v", err)
	}
	if _, ok := p.GrowingSeed(key); ok {
		t.Fatal("a growth stopped before its seed published the seed")
	}
	p.ReleaseScratch()

	p = parser(begin + step - 1)
	g = p.GrowBegin(key, inlinedRules(t, prog)[expr.ID])
	seed := g.Best
	p.Pos = 1
	if err := statetest.CatchFatal(t, func() { p.GrowStepVM(&g, retained, true) }); !errors.Is(err, runtime.ErrLimit) {
		t.Fatalf("growth step short of a unit: %v", err)
	}
	if stored, ok := p.GrowingSeed(key); !ok || stored != seed || p.Pos != 1 {
		t.Fatal("a growth step stopped short of a unit changed the seed or the position")
	}
	p.ReleaseScratch()
}
