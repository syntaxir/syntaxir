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

// The rules of the conformance suite on the TypeScript library: their descriptors and the invocation
// of scripted bodies. The cases of a Document parse, a stream parse, an observed parse and the
// missing-token mode are skipped: the library's Parser has none of them.

func tsRuleSuite(controlled bool) conform.Suite {
	return conform.Suite{Name: fmt.Sprint("typescript rules (controlled=", controlled, ")"), Ops: append(append([]string{}, tsconformtest.RuleOps...), budgetOps(controlled)...), CallUnitBefore: true,
		// The library memoizes the same rules in every parse (rule.docOnlyMemo is false).
		DocCallMemo: true}
}

func tsRuleRun(r tsconformtest.Runner, f tsconformtest.Family, s conform.Suite, seeds, n int) tsConformResult {
	return tsConformRunCases(r, f, s, conform.RuleCases(), seeds, func(seed int) conform.Case {
		rng := rand.New(rand.NewPCG(uint64(seed), 68))
		return conform.RandomRule(fmt.Sprint("random_", seed), rng, s.Ops, n, false)
	})
}

func TestTypeScriptConformanceRule(t *testing.T) {
	node := tooltest.RequireNode(t)
	lib := tsConformLib(t)
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	for _, controlled := range []bool{false, true} {
		t.Run(fmt.Sprint("controlled=", controlled), func(t *testing.T) {
			s := tsRuleSuite(controlled)
			runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
			res := tsRuleRun(runner, tsconformtest.Rule(controlled), s, seeds, 150)
			if res.ran < seeds {
				t.Fatalf("only %d cases ran", res.ran)
			}
			tsReport(t, res)
		})
	}
}

// TestTypeScriptConformanceRuleDetectsMutants is the negative control of TestTypeScriptConformanceRule.
func TestTypeScriptConformanceRuleDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	node := tooltest.RequireNode(t)
	// Not here: a body that leaves its frame, its environment or its trail behind, and a value that stays
	// fresh: no operation of the rules reads them, so no case of this family can tell.
	mutants := []tsMutant{
		{name: "an invocation keeps the nesting level of a rule on a cycle", sig: "invoke(r: Rule, min: number", old: "this.depth--;", new: ""},
		{name: "a plain failure keeps the errors it recovered", sig: "invokePlainBody(r: Rule", old: "this.recovered.length = rec;", new: ""},
		{name: "a plain failure leaves the position where the body stopped", sig: "invokePlainBody(r: Rule", old: "this.pos = start;", new: ""},
		{name: "a plain invocation does not check the nesting limit", sig: "invokePlainBody(r: Rule", old: "if (++this.depth > this.maxDepth) {", new: "if (++this.depth < 0) {"},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			lib := tsMutate(t, m)
			s := tsRuleSuite(m.charges)
			runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
			res := tsRuleRun(runner, tsconformtest.Rule(m.charges), s, 150, 150)
			for _, err := range res.fails {
				op, kind := tsFailureKind(err)
				if m.charges && kind != "bound" && kind != "exact" && kind != "free" {
					continue
				}
				t.Logf("detected at %s (%s):\n%v", op, kind, strings.SplitN(err.Error(), "\ncase:", 2)[0])
				return
			}
			t.Fatalf("the suite did not notice: %d cases ran, %d failed", res.ran, len(res.fails))
		})
	}
}

// TestTypeScriptConformanceRulePrintedDetectsMutants is the negative control of the code that the printer prints for the operations of the family:
// the suite runs that code, so a change of it must fail.
func TestTypeScriptConformanceRulePrintedDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	mutants := []tsPrintedMutant{
		{name: "an invocation runs another level", method: "op_rule_invoke", old: "this.invoke(p0, p1)", new: "this.invoke(p0, p1 + 1)"},
	}
	tsDetectPrinted(t, mutants, func(r tsconformtest.Runner, charges bool) tsConformResult {
		return tsRuleRun(r, tsconformtest.Rule(charges), tsRuleSuite(charges), 150, 150)
	})
}
