package common_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
	"syntaxir.dev/sxir/internal/testing/tsconformtest"
)

// The observation of a parse of the conformance suite on the TypeScript library: the events that
// its Observation delivers to a trace function.

func tsObsRun(r tsconformtest.Runner, seeds, n int) tsConformResult {
	s := conform.Suite{Name: "typescript observation", Ops: conform.ObsOps}
	return tsConformRunCases(r, tsconformtest.Obs(), s, conform.ObsCases(), seeds, func(seed int) conform.Case {
		rng := rand.New(rand.NewPCG(uint64(seed), 66))
		return conform.RandomObs(fmt.Sprint("random_", seed), rng, n, seed%4 != 3)
	})
}

func TestTypeScriptConformanceObs(t *testing.T) {
	node := codegentest.RequireNode(t)
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, tsConformLib(t))}
	res := tsObsRun(runner, seeds, 150)
	if res.ran < seeds {
		t.Fatalf("only %d cases ran", res.ran)
	}
	tsReport(t, res, conform.ObsOps, map[string]string{})
}

// TestTypeScriptConformanceObsDetectsMutants is the negative control of TestTypeScriptConformanceObs.
func TestTypeScriptConformanceObsDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	node := codegentest.RequireNode(t)
	mutants := []tsMutant{
		{name: "a reuse marks nothing", sig: "memoHit(): void", old: "call.memo = true;", new: "call.memo = false;"},
		{name: "a reuse marks the outermost call", sig: "memoHit(): void", old: "this.stack[this.stack.length - 1]", new: "this.stack[0]"},
		{name: "an entry reports the level 0", sig: "enter(rule: string, pos: number, level: number", old: "rule, pos, level, depth", new: "rule, pos, level: 0, depth"},
		{name: "an entry reports a depth one too small", sig: "enter(rule: string, pos: number, level: number", old: "depth: this.stack.length + 1", new: "depth: this.stack.length"},
		{name: "an exit reports a failure", sig: "exit(matched: boolean", old: "this.stack.pop()!, matched,", new: "this.stack.pop()!, false,"},
		{name: "an exit reports the end as the examined end", sig: "exit(matched: boolean", old: "matched, end, examined)", new: "matched, end, end)"},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, tsMutate(t, m))}
			res := tsObsRun(runner, 150, 150)
			if len(res.fails) == 0 {
				t.Fatalf("the suite did not notice: %d cases ran", res.ran)
			}
			t.Logf("detected: %v", strings.SplitN(res.fails[0].Error(), "\ncase:", 2)[0])
		})
	}
}

// TestTypeScriptConformanceObsPrintedDetectsMutants is the negative control of the code that the printer prints for the operations of the family:
// the suite runs that code, so a change of it must fail.
func TestTypeScriptConformanceObsPrintedDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	mutants := []tsPrintedMutant{
		{name: "a reuse is not reported", method: "op_obs_memoHit", old: "this.observation?.memoHit();", new: ""},
	}
	tsDetectPrinted(t, mutants, func(r tsconformtest.Runner, _ bool) tsConformResult {
		return tsObsRun(r, 150, 150)
	})
}
