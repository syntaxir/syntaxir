package common_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/internal/testing/tsconformtest"
)

// The descriptors of the Operators Rules, the matches of operators and the records of the split forms
// of their loop, of the conformance suite on the TypeScript library. The generated parser prints every one
// of these operations inline over the library's descriptors, which the bindings of the harness mirror.

func tsOprRun(r tsconformtest.Runner, seeds, n int) tsConformResult {
	s := conform.Suite{Name: "typescript operators", Ops: tsconformtest.OprSuiteOps}
	return tsConformRunCases(r, tsconformtest.Opr(), s, conform.OprCases(), seeds, func(seed int) conform.Case {
		rng := rand.New(rand.NewPCG(uint64(seed), 7))
		return conform.RandomOpr(fmt.Sprint("random_", seed), rng, tsconformtest.OprSuiteOps, n, conform.OprConfig())
	})
}

func TestTypeScriptConformanceOpr(t *testing.T) {
	node := tooltest.RequireNode(t)
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

// TestTypeScriptConformanceOprDetectsMutants is the negative control of TestTypeScriptConformanceOpr. The
// operations are the code that the printer prints for them (print.OperationMethods), so each mutant changes
// the printed text of one operation.
func TestTypeScriptConformanceOprDetectsMutants(t *testing.T) {
	node := tooltest.RequireNode(t)
	mutants := []tsPrintedMutant{
		{name: "led reads the prefix operators", method: "op_opr_led", old: "operatorsRule!.led", new: "operatorsRule!.prefix"},
		{name: "prefix reads the led operators", method: "op_opr_prefix", old: "operatorsRule!.prefix", new: "operatorsRule!.led"},
		{name: "count is one more", method: "op_opr_count", old: "p0.length", new: "p0.length + 1"},
		{name: "at stops at the first operator", method: "op_opr_at", old: "p0[p1]!", new: "p0[0]!"},
		{name: "operand stops at the first line", method: "op_opr_operand", old: "operands[p1]!", new: "operands[0]!"},
		{name: "level is one more", method: "op_opr_level", old: "p0.level", new: "p0.level + 1"},
		{name: "infix is postfix", method: "op_opr_infix", old: `"infix"`, new: `"postfix"`},
		{name: "right is not associative", method: "op_opr_right", old: `"right"`, new: `"none"`},
		{name: "scope is one more", method: "op_opr_scope", old: "p0.scope.length", new: "p0.scope.length + 1"},
		{name: "hasSkip is negated", method: "op_opr_hasSkip", old: "!== null", new: "=== null"},
		{name: "skip is negated", method: "op_opr_skip", old: "!== undefined", new: "=== undefined"},
		{name: "skip runs the trivia twice", method: "op_opr_skip", old: "p0.operatorsRule!.skip!(this)",
			new: "(p0.operatorsRule!.skip!(this), p0.operatorsRule!.skip!(this))"},
		{name: "match runs the line twice", method: "op_opr_match", old: "p0.m(this)", new: "(p0.m(this), p0.m(this))"},
		{name: "match never matches", method: "op_opr_match", old: "p0.m(this)", new: "(p0.m(this), undefined)"},
		{name: "lineValue starts after its start", method: "op_opr_lineValue", old: "p3 as Node | null, p4)", new: "p3 as Node | null, p4 + 1)"},
		{name: "lineValue drops the captures", method: "op_opr_lineValue", old: "p1, p2, p3", new: "p1, p2.map(() => null), p3"},
		{name: "lineValue is the value of the expression", method: "op_opr_lineValue",
			old: "this.lineResult(p0, p1, p2, p3 as Node | null, p4)", new: "(this.lineResult(p0, p1, p2, p3 as Node | null, p4), p3 as Node | null)"},
		{name: "build swaps the operands", method: "op_opr_build", old: "p2 as Node | null, p3 as Node | null", new: "p3 as Node | null, p2 as Node | null"},
		{name: "build has no value", method: "op_opr_build", old: "this.operatorsRuleBuild(", new: "null && this.operatorsRuleBuild("},
		{name: "an attempt ends at its start", method: "op_opr_attempt", old: "end: this.pos", new: "end: p3"},
		{name: "an attempt forgets the cut", method: "op_opr_attempt", old: "cut: p4", new: "cut: false"},
		{name: "an attempt keeps no environment", method: "op_opr_attempt", old: "env: this.env", new: "env: null"},
		{name: "found is negated", method: "op_opr_found", old: "p0 !== null", new: "p0 === null"},
		{name: "the cut flag is not kept", method: "op_opr_setCutFailed", old: "this.cutFailed = p0", new: "this.cutFailed = false"},
		{name: "the binding bound is not kept", method: "op_opr_setNudBound", old: "this.nudBound = p0", new: "this.nudBound = 0"},
		{name: "the empty operator flag is not read", method: "op_opr_afterEmpty", old: "this.afterEmpty", new: "false"},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			runner := tsconformtest.Runner{Session: tsconformtest.StartSources(t, node, tsPrintedSources(t, m))}
			res := tsOprRun(runner, 150, 250)
			if len(res.fails) == 0 {
				t.Fatalf("the suite did not notice: %d cases ran", res.ran)
			}
			t.Logf("detected: %v", strings.SplitN(res.fails[0].Error(), "\ncase:", 2)[0])
		})
	}
}

// TestTypeScriptConformanceOprDetectsLibraryMutants is the negative control of the conversion of the
// operators' values to their named node type, which the library does: the parse whose operator's value
// does not convert goes on with it, or ends at the compute limit, and the suite fails.
func TestTypeScriptConformanceOprDetectsLibraryMutants(t *testing.T) {
	node := tooltest.RequireNode(t)
	throw := "throw new Fatal(new Error(`cannot convert ${typeName(u)} to ${r.opNodeType} in result of rule ${r.where || r.name}`));"
	mutants := []tsMutant{
		{name: "a value that does not convert is kept", sig: "convertOperator(r: Rule", old: throw, new: "return u as unknown as Node;"},
		{name: "a value that does not convert ends the parse at the limit", sig: "convertOperator(r: Rule", old: throw,
			new: `throw new Fatal(new ParseInterruptionError("compute-limit"));`},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, tsMutate(t, m))}
			res := tsOprRun(runner, 60, 250)
			if len(res.fails) == 0 {
				t.Fatalf("the suite did not notice: %d cases ran", res.ran)
			}
			t.Logf("detected: %v", strings.SplitN(res.fails[0].Error(), "\ncase:", 2)[0])
		})
	}
}
