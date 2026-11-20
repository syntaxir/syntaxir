package common_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"zeroparse.dev/zp/internal/ir/code/exec/conform"
	"zeroparse.dev/zp/internal/testing/codegentest"
	"zeroparse.dev/zp/internal/testing/tsconformtest"
)

// The descriptors of the Operators Rules, the matches of operators and the records of the split forms
// of their loop, of the conformance suite on the TypeScript library. The generated parser prints every one
// of these operations inline over the library's descriptors, which the bindings of the harness mirror.

func tsOprRun(r tsconformtest.Runner, seeds, n int) tsConformResult {
	s := conform.Suite{Name: "typescript operators", Ops: tsconformtest.OprSuiteOps}
	return tsConformRunCases(r, tsconformtest.Opr(), s, conform.OprCases(), seeds, func(seed int) conform.Case {
		rng := rand.New(rand.NewPCG(uint64(seed), 7))
		return conform.RandomOpr(fmt.Sprint("random_", seed), rng, conform.OprOps, n, conform.OprConfig())
	})
}

func TestTypeScriptConformanceOpr(t *testing.T) {
	node := codegentest.RequireNode(t)
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, tsConformLib(t))}
	res := tsOprRun(runner, seeds, 250)
	if res.ran < seeds {
		t.Fatalf("only %d cases ran", res.ran)
	}
	tsReport(t, res, tsconformtest.OprSuiteOps, map[string]string{})
}

// TestTypeScriptConformanceOprDetectsMutants is the negative control of TestTypeScriptConformanceOpr.
// The operations are bindings that mirror printed code, not functions of the library, so the mutants
// change the bindings of the harness.
func TestTypeScriptConformanceOprDetectsMutants(t *testing.T) {
	node := codegentest.RequireNode(t)
	mutants := []struct{ name, old, new string }{
		{"led reads the prefix operators", "return [(a[0] as p.Rule).operatorsRule!.led];", "return [(a[0] as p.Rule).operatorsRule!.prefix];"},
		{"prefix reads the led operators", "return [(a[0] as p.Rule).operatorsRule!.prefix];", "return [(a[0] as p.Rule).operatorsRule!.led];"},
		{"count is one more", "return [a[0].length];", "return [a[0].length + 1];"},
		{"at stops at the first operator", "return [a[0][a[1]]!];", "return [a[0][0]!];"},
		{"operand stops at the first line", "operands[a[1]]!];", "operands[0]!];"},
		{"level is one more", "return [a[0].level];", "return [a[0].level + 1];"},
		{"infix is postfix", `return [a[0].kind === "infix"];`, `return [a[0].kind === "postfix"];`},
		{"right is not associative", `return [a[0].assoc === "right"];`, `return [a[0].assoc === "none"];`},
		{"scope is one more", "return [a[0].scope.length];", "return [a[0].scope.length + 1];"},
		{"hasSkip is negated", "operatorsRule!.skip !== null];", "operatorsRule!.skip === null];"},
		{"an attempt ends at its start", "end: P.pos, env: P.env,", "end: a[3], env: P.env,"},
		{"an attempt forgets the cut", "cut: a[4], errs: a[5] }", "cut: false, errs: a[5] }"},
		{"an attempt keeps no environment", "end: P.pos, env: P.env,", "end: P.pos, env: null,"},
		{"found is negated", "case \"opr.found\":\n        return [a[0] !== null];", "case \"opr.found\":\n        return [a[0] === null];"},
		{"the cut flag is not kept", "x.cutFailed = a[0];", "x.cutFailed = false;"},
		{"the binding bound is not kept", "x.nudBound = a[0];", "x.nudBound = 0;"},
		{"the empty operator flag is not read", "return [x.afterEmpty];", "return [false];"},
		{"a record keeps no field", "r[f[3]!.toLowerCase() + f.slice(4)] = a[1];", "void a[1];"},
		{"a record's unset list is null", `return [f === "l" ? this.noOprs :`, `return [f === "l" ? null :`},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			h := tsconformtest.Harness()
			if strings.Count(h, m.old) < 1 {
				t.Fatalf("the harness has no %q", m.old)
			}
			runner := tsconformtest.Runner{Session: tsconformtest.StartWith(t, node, tsConformLib(t), strings.Replace(h, m.old, m.new, 1))}
			res := tsOprRun(runner, 150, 250)
			if len(res.fails) == 0 {
				t.Fatalf("the suite did not notice: %d cases ran", res.ran)
			}
			t.Logf("detected: %v", strings.SplitN(res.fails[0].Error(), "\ncase:", 2)[0])
		})
	}
}
