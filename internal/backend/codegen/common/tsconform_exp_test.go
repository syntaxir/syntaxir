package common_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"zeroparse.dev/zp/internal/ir/code/exec/conform"
	"zeroparse.dev/zp/internal/ir/code/exec/ref"
	"zeroparse.dev/zp/internal/testing/codegentest"
	"zeroparse.dev/zp/internal/testing/mutanttest"
	"zeroparse.dev/zp/internal/testing/tsconformtest"
)

// The expectation record of the conformance suite on the TypeScript library: the handwritten cases and
// the randomized sequences of the record, with and without budget controls and recording.

func tsExpSuite(controlled bool) conform.Suite {
	s := conform.Suite{Name: fmt.Sprint("typescript expectations (controlled=", controlled, ")"), Ops: append(append([]string{}, tsconformtest.ExpOps...), budgetOps(controlled)...)}
	if controlled {
		s.Exact = []string{"exp.copy"}
	}
	return s
}

func tsExpRun(r tsconformtest.Runner, f tsconformtest.Family, s conform.Suite, seeds, n int) tsConformResult {
	return tsConformRunCases(r, f, s, conform.ExpCases(), seeds, func(seed int) conform.Case {
		rng := rand.New(rand.NewPCG(uint64(seed), 66))
		return conform.RandomExp(fmt.Sprint("random_", seed), rng, conform.RecordOps, n, ref.Config{NoExpectations: seed%3 == 2})
	})
}

func TestTypeScriptConformanceExp(t *testing.T) {
	node := codegentest.RequireNode(t)
	lib := tsConformLib(t)
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	for _, controlled := range []bool{false, true} {
		t.Run(fmt.Sprint("controlled=", controlled), func(t *testing.T) {
			s := tsExpSuite(controlled)
			runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
			res := tsExpRun(runner, tsconformtest.Exp(controlled), s, seeds, 150)
			if res.ran < seeds {
				t.Fatalf("only %d cases ran", res.ran)
			}
			tsReport(t, res, s.Ops, map[string]string{})
		})
	}
}

// TestTypeScriptConformanceExpDetectsMutants is the negative control for the library's own functions
// that the record uses; the record's operations are printed code, whose mutants are in
// TestTypeScriptConformanceExpPrintedDetectsMutants.
func TestTypeScriptConformanceExpDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	node := codegentest.RequireNode(t)
	mutants := []tsMutant{
		{name: "a copy reads the first entry only", sig: "copyIDsSpend(items", old: "out[i - start] = items[i]!;", new: "out[i - start] = items[start]!;", controlled: true},
		{name: "a copy charges nothing", sig: "copyIDsSpend(items", old: "control.charge(stop - from);", new: "", charges: true},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			lib := tsMutate(t, m)
			s := tsExpSuite(m.charges || m.controlled)
			runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
			res := tsExpRun(runner, tsconformtest.Exp(m.charges || m.controlled), s, 150, 150)
			for _, err := range res.fails {
				op, kind := tsFailureKind(err)
				if m.charges && kind != "bound" && kind != "exact" {
					continue
				}
				t.Logf("detected at %s (%s):\n%v", op, kind, strings.SplitN(err.Error(), "\ncase:", 2)[0])
				return
			}
			t.Fatalf("the suite did not notice: %d cases ran, %d failed", res.ran, len(res.fails))
		})
	}
}

// TestTypeScriptConformanceExpPrintedDetectsMutants is the negative control of the code that the printer prints for the operations of the family:
// the suite runs that code, so a change of it must fail.
func TestTypeScriptConformanceExpPrintedDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	mutants := []tsPrintedMutant{
		{name: "an isolation does not move the base", method: "op_exp_isolate", old: "this.expBase = this.expTop;", new: ""},
		{name: "an isolation keeps the farthest position", method: "op_exp_isolate", old: "this.farthest = p0;", new: ""},
		{name: "an end of isolation does not restore the base", method: "op_exp_unisolate", old: "this.expBase = p0Base;", new: ""},
		{name: "an end of isolation keeps the entries", method: "op_exp_unisolate", old: "this.expTop = this.expBase;", new: ""},
		{name: "an end of isolation does not restore the farthest position", method: "op_exp_unisolate", old: "this.farthest = p0Far;", new: ""},
		{name: "a copy keeps the entries of another range", method: "op_exp_copy", old: "this.exp.slice(p0From, p0To)", new: "this.exp.slice(p0From, p0To - 1)", charges: false},
		{name: "an add does not advance the top", method: "op_exp_add", old: "this.exp[this.expTop++] = p0;", new: "this.exp[this.expTop] = p0;"},
		{name: "a reset keeps the entries", method: "op_exp_reset", old: "this.expTop = this.expBase;", new: ""},
		{name: "a reset keeps the farthest position", method: "op_exp_reset", old: "this.farthest = p0;", new: ""},
		{name: "a read of a set shifts the index", method: "op_exp_setAt", old: "p0From + p1", new: "p0From + p1 + 1"},
		{name: "the count of a set is one too large", method: "op_exp_setCount", old: "p0To - p0From", new: "p0To - p0From + 1"},
	}
	tsDetectPrinted(t, mutants, func(r tsconformtest.Runner, charges bool) tsConformResult {
		return tsExpRun(r, tsconformtest.Exp(charges), tsExpSuite(charges), 150, 150)
	})
}
