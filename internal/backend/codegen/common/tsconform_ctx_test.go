package common_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/internal/testing/tsconformtest"
)

// The dependency memo of left recursion and the growths of the conformance suite on the TypeScript
// library, with the cases that keep a result: the library takes the success of a kept result from the
// operation's ok, not from its value.

func tsCtxSuite(controlled bool) conform.Suite {
	ops := slices.Clone(tsconformtest.CtxOps)
	s := conform.Suite{Name: fmt.Sprint("typescript ctx (controlled=", controlled, ")"), Ops: append(ops, budgetOps(controlled)...)}
	// The composition counts each step that evaluates the body: the operations of the growth, a step taken
	// from the table of a context included, count none.
	s.Free = []string{"grow.begin", "grow.adopt", "grow.memoStep", "grow.isolate", "grow.keepStep", "grow.grew", "grow.adoptStep", "grow.end"}
	return s
}

func tsCtxRun(r tsconformtest.Runner, f tsconformtest.Family, s conform.Suite, seeds, n int) tsConformResult {
	return tsConformRunCases(r, f, s, conform.CtxCases(), seeds, func(seed int) conform.Case {
		return conform.RandomCtx(fmt.Sprint("random_", seed), rand.New(rand.NewPCG(uint64(seed), 68)), n)
	})
}

func TestTypeScriptConformanceCtx(t *testing.T) {
	node := tooltest.RequireNode(t)
	lib := tsConformLib(t)
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	for _, controlled := range []bool{false, true} {
		t.Run(fmt.Sprint("controlled=", controlled), func(t *testing.T) {
			s := tsCtxSuite(controlled)
			runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
			res := tsCtxRun(runner, tsconformtest.Ctx(controlled), s, seeds, 150)
			tsReport(t, res)
		})
	}
}

// TestTypeScriptConformanceCtxDetectsMutants is the negative control of TestTypeScriptConformanceCtx.
func TestTypeScriptConformanceCtxDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	node := tooltest.RequireNode(t)
	mutants := []tsMutant{
		{name: "a result answers a call in any context", sig: "memoAllows(c: CtxCall", old: "(c.present & ~present) === 0", new: "true"},
		{name: "a result that read a seed goes to the memo", sig: "ctxPop(c: CtxCall", old: "if (d.kind !== depShadow) {", new: "if (false) {"},
		{name: "a leader reading its seed notes nothing", sig: "ctxEnter(c: CtxCall", old: "this.noteSeedRead(i);", new: ""},
		{name: "a step of the same end is longer", sig: "growLonger(g: GrowState", old: "this.pos > best.end", new: "this.pos >= best.end"},
		{name: "a kept result is stored without its dependencies", sig: "ctxKeep(c: CtxCall", old: "this.stored(this.popped)", new: "this.stored(noDeps)"},
		{name: "a lookup replays no dependencies", sig: "ctxLookup(c: CtxCall", old: "this.replayDeps(this.pos, c.gp, c.present, s.deps);", new: ""},
		{name: "a lookup ignores the outcomes that a result observed", sig: "ctxFind(pos: number", old: `k += v + ",";`, new: `k += ",";`},
		{name: "a lookup answers for another rule", sig: "ctxLookup(c: CtxCall", old: "headKey(ctxCallKind, rule, min, c.present)", new: "headKey(ctxCallKind, 0, min, c.present)"},
		{name: "an adopted seed keeps its identity", sig: "growAdopt(g: GrowState", old: "rec.id = this.nextSeedID();", new: ""},
		{name: "an adopted seed does not return to the start", sig: "growAdopt(g: GrowState", old: "this.pos = g.start;", new: ""},
		{name: "a jump replays no dependencies", sig: "growMemoStep(g: GrowState", old: "this.replayDeps(g.start, g.gp, g.present, j.deps);", new: ""},
		{name: "a jump merges no expectations", sig: "growMemoStep(g: GrowState", old: "this.mergeKept(j.far, j.exp);", new: ""},
		{name: "a jump counts a unit", sig: "growMemoStep(g: GrowState", old: "this.replayDeps(g.start, g.gp, g.present, j.deps);", new: "this.units++;\n        this.replayDeps(g.start, g.gp, g.present, j.deps);", charges: true},
		{name: "a memoized step replays no dependencies", sig: "growMemoStep(g: GrowState", old: "this.replayDeps(g.start, g.gp, g.present, s.deps);", new: ""},
		{name: "a memoized step merges no expectations", sig: "growMemoStep(g: GrowState", old: "this.mergeKept(s.far, s.exp);", new: ""},
		{name: "a growth counts a unit as it begins", sig: "growBegin(r: Rule", old: "const start = this.pos;", new: "this.units++;\n    const start = this.pos;", charges: true},
		{name: "an adopted seed counts a unit", sig: "growAdopt(g: GrowState", old: "this.pos = g.start;", new: "this.units++;\n    this.pos = g.start;", charges: true},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			lib := tsMutate(t, m)
			s := tsCtxSuite(m.charges)
			runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
			res := tsCtxRun(runner, tsconformtest.Ctx(m.charges), s, 150, 150)
			for _, err := range res.fails {
				op, kind := tsFailureKind(err)
				if m.charges && kind != "bound" && kind != "exact" && kind != "free" {
					continue
				}
				t.Logf("detected at %s (%s):\n%v", op, kind, strings.SplitN(err.Error(), "\ncase:", 2)[0])
				return
			}
			t.Fatalf("the suite did not notice: %d cases ran, %d pending, %d failed", res.ran, res.pending, len(res.fails))
		})
	}
}

// TestTypeScriptConformanceCtxPrintedDetectsMutants is the negative control of the code that the printer prints for the operations of the family:
// the suite runs that code, so a change of it must fail.
func TestTypeScriptConformanceCtxPrintedDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	// Not here: an entry for a rule of no cycle, which no case tells from the entry that the library makes of
	// it (checked: the suite does not notice it).
	mutants := []tsPrintedMutant{
		{name: "a call answers in any context", method: "op_ctx_allows", old: "memoAllows(p0, p1.present)", new: "true"},
		{name: "a done does nothing", method: "op_ctx_done", old: "this.ctxDone(p0);", new: ""},
		{name: "a push does nothing", method: "op_ctx_push", old: "this.ctxPush(p0);", new: ""},
		{name: "a kept result takes its success from its value", method: "op_ctx_keep", old: "newCtxResult(p8, p5,", new: "newCtxResult(p8, p8 !== undefined,"},
		{name: "an end is the success", method: "op_ctx_ok", old: "p0!.ok", new: "true"},
		{name: "a growth begins in another state", method: "op_grow_begin", old: "this.growBegin(p0, p1)", new: "this.growBegin(p0, !p1)"},
		{name: "a step of the same end is longer", method: "op_grow_longer", old: "this.growLonger(p0, p1)", new: "!this.growLonger(p0, p1)"},
		{name: "an adopted seed is not adopted", method: "op_grow_adopt", old: "this.growAdopt(p0, p1, p2);", new: ""},
	}
	tsDetectPrinted(t, mutants, func(r tsconformtest.Runner, charges bool) tsConformResult {
		return tsCtxRun(r, tsconformtest.Ctx(charges), tsCtxSuite(charges), 150, 150)
	})
}
