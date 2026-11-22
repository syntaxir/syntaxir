package common_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/internal/testing/tsconformtest"
)

// The expectation record of the conformance suite on the TypeScript library: the handwritten cases and
// the randomized sequences of the record, with and without budget controls and recording.

func tsExpSuite(controlled bool) conform.Suite {
	s := conform.Suite{Name: fmt.Sprint("typescript expectations (controlled=", controlled, ")"), Ops: append(append([]string{}, tsconformtest.ExpOps...), budgetOps(controlled)...)}
	if controlled {
		// Expectations are no event: their copy counts no units.
		s.Free = []string{"exp.copy"}
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
	node := tooltest.RequireNode(t)
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
			tsReport(t, res)
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
		{name: "a copy counts its entries", method: "op_exp_copy", old: "this.lastKept = this.exp.slice(p0From, p0To);", new: "this.units += p0To - p0From;\nthis.lastKept = this.exp.slice(p0From, p0To);", charges: true},
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
