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
// that the record uses; the record's operations themselves are printed code that the harness mirrors.
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
