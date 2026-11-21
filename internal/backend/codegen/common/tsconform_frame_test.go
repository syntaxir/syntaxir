package common_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/internal/testing/tsconformtest"
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
	node := tooltest.RequireNode(t)
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
// printed code, whose mutants are in TestTypeScriptConformanceFramePrintedDetectsMutants.
func TestTypeScriptConformanceFrameDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	node := tooltest.RequireNode(t)
	mutants := []tsMutant{
		{name: "a write does not go on the trail", sig: "setCapture(slot: number", old: "this.trail.push({ f: this.frame, slot, old: this.frame[slot]! });", new: ""},
		{name: "a write trails the new value", sig: "setCapture(slot: number", old: "old: this.frame[slot]!", new: "old: v"},
		{name: "a write is not made", sig: "setCapture(slot: number", old: "this.frame[slot] = v;", new: ""},
		{name: "a new frame has no empty slots", sig: "newFrame(n: number", old: "f.fill(null);", new: "f.fill(emptyFrame as unknown as null);"},
		{name: "a frame of no slots is not shared", sig: "newFrame(n: number", old: "return emptyFrame;", new: "return [];"},
		{name: "a new frame charges a unit for each of its slots", sig: "newFrame(n: number", old: "f.fill(null);", new: "this.spend(n);\n    f.fill(null);", charges: true},
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

// TestTypeScriptConformanceFramePrintedDetectsMutants is the negative control of the code that the printer prints for the operations of the family:
// the suite runs that code, so a change of it must fail.
func TestTypeScriptConformanceFramePrintedDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	mutants := []tsPrintedMutant{
		{name: "a mark leaves out the position", method: "op_frame_mark", old: "const r0Pos = this.pos;", new: "const r0Pos = 0;"},
		{name: "a mark leaves out the trail", method: "op_frame_mark", old: "const r0Trail = this.trail.length;", new: "const r0Trail = 0;"},
		{name: "an undo does not unwind the trail", method: "op_frame_undo", old: "u.f[u.slot] = u.old;", new: ""},
		{name: "an undo keeps the entries", method: "op_frame_undo", old: "this.trail.length = p0;", new: ""},
		{name: "an undo charges a unit for each write it undoes", method: "op_frame_undo", old: "u.f[u.slot] = u.old;", new: "this.spend();\n      u.f[u.slot] = u.old;", charges: true},
		{name: "a save keeps what was saved before", method: "op_frame_save", old: "this.saved.length = 0;", new: ""},
		{name: "a save charges a unit for each slot", method: "op_frame_save", old: "this.saved.push(x);", new: "this.spend();\n      this.saved.push(x);", charges: true},
		{name: "a restore reads the first slot only", method: "op_frame_restore", old: "p0[t] = this.saved[t]!;", new: "p0[t] = this.saved[0]!;"},
		{name: "a restore charges a unit for each slot", method: "op_frame_restore", old: "p0[t] = this.saved[t]!;", new: "this.spend();\n      p0[t] = this.saved[t]!;", charges: true},
	}
	tsDetectPrinted(t, mutants, func(r tsconformtest.Runner, charges bool) tsConformResult {
		s := conform.Suite{Name: "typescript frame printed mutant", Ops: append(conform.FrameSuiteOps(false), budgetOps(charges)...), Exact: tsFrameExact(charges)}
		return tsFrameRun(r, tsconformtest.Frame(charges), s, 150, 150)
	})
}
