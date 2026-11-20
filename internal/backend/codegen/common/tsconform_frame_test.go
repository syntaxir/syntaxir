package common_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"zeroparse.dev/zp/internal/ir/code/exec/conform"
	"zeroparse.dev/zp/internal/testing/codegentest"
	"zeroparse.dev/zp/internal/testing/mutanttest"
	"zeroparse.dev/zp/internal/testing/tsconformtest"
)

// The capture frames of the conformance suite on the TypeScript library. The library has no
// missing-token record, so the suite is the one without it (conform.FrameSuiteOps(false)).

func tsFrameRun(r tsconformtest.Runner, f tsconformtest.Family, s conform.Suite, seeds, n int) tsConformResult {
	return tsConformRunCases(r, f, s, conform.FrameCases(), seeds, func(seed int) conform.Case {
		rng := rand.New(rand.NewPCG(uint64(seed), 67))
		return conform.RandomFrame(fmt.Sprint("random_", seed), rng, conform.FrameRandomOps(false), n)
	})
}

func TestTypeScriptConformanceFrame(t *testing.T) {
	node := codegentest.RequireNode(t)
	lib := tsConformLib(t)
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	for _, controlled := range []bool{false, true} {
		t.Run(fmt.Sprint("controlled=", controlled), func(t *testing.T) {
			s := conform.Suite{Name: fmt.Sprint("typescript frame (controlled=", controlled, ")"), Ops: append(conform.FrameSuiteOps(false), budgetOps(controlled)...), Exact: tsFrameExact(controlled)}
			runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
			res := tsFrameRun(runner, tsconformtest.Frame(controlled), s, seeds, 150)
			if res.ran < seeds {
				t.Fatalf("only %d cases ran", res.ran)
			}
			known := map[string]string{}
			if controlled {
				known = tsChargeDivergences
			}
			tsReport(t, res, s.Ops, known)
		})
	}
}

// tsFrameExact are the operations whose charge the suite holds to the model's exactly, not only to its
// bound: the operations that spend for each slot or each write.
func tsFrameExact(controlled bool) []string {
	if !controlled {
		return nil
	}
	return []string{"frame.open", "frame.undo", "frame.save", "frame.restore"}
}

func budgetOps(controlled bool) []string {
	if controlled {
		return tsconformtest.ExecBudgetOps
	}
	return nil
}

// TestTypeScriptConformanceFrameDetectsMutants is the negative control of TestTypeScriptConformanceFrame
// for the library's own functions of the frame; frame.mark, frame.undo, frame.save and frame.restore are
// printed code in the generated parser, which the binding of the harness mirrors, so they are not
// mutants of the library source.
func TestTypeScriptConformanceFrameDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	node := codegentest.RequireNode(t)
	mutants := []tsMutant{
		{name: "a write does not go on the trail", sig: "setCapture(slot: number", old: "this.trail.push({ f: this.frame, slot, old: this.frame[slot]! });", new: ""},
		{name: "a write trails the new value", sig: "setCapture(slot: number", old: "old: this.frame[slot]!", new: "old: v"},
		{name: "a write is not made", sig: "setCapture(slot: number", old: "this.frame[slot] = v;", new: ""},
		{name: "a new frame has no empty slots", sig: "newFrame(n: number", old: "f[i] = null;", new: "f[i] = emptyFrame as unknown as null;"},
		{name: "a frame of no slots is not shared", sig: "newFrame(n: number", old: "return emptyFrame;", new: "return [];"},
		{name: "a new frame charges nothing for its slots", sig: "newFrame(n: number", old: "this.spend();", new: "", charges: true},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			lib := tsMutate(t, m)
			s := conform.Suite{Name: "typescript frame mutant", Ops: append(conform.FrameSuiteOps(false), budgetOps(m.charges)...), Exact: tsFrameExact(m.charges)}
			runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
			res := tsFrameRun(runner, tsconformtest.Frame(m.charges), s, 150, 150)
			for _, err := range res.fails {
				op, kind := tsFailureKind(err)
				if m.charges && kind != "bound" && kind != "exact" {
					continue
				}
				if _, known := tsChargeDivergences[op]; known && kind != "result" && kind != "other" {
					continue
				}
				t.Logf("detected at %s (%s):\n%v", op, kind, strings.SplitN(err.Error(), "\ncase:", 2)[0])
				return
			}
			t.Fatalf("the suite did not notice: %d cases ran, %d failed", res.ran, len(res.fails))
		})
	}
}
