package common_test

import (
	"strings"
	"testing"

	"zeroparse.dev/zp/internal/testing/codegentest"
	"zeroparse.dev/zp/internal/testing/mutanttest"
	"zeroparse.dev/zp/internal/testing/tsconformtest"
)

// tsMutant is a change of the source of the TypeScript library that the conformance suite must notice.
type tsMutant struct {
	name string
	// sig, old and new are the arguments of mutateTSFunc: the function or method, the text in it to
	// replace and its replacement, at the last occurrence if last is set.
	sig, old, new string
	last          bool
	// suite is the suite of tsExecSuites that must fail: 0 memo, 1 environment, 2 recovered errors.
	suite int
	// charges makes the mutant a change of charges: the suite runs with budget controls and must fail
	// at a bound or an exact cost.
	charges bool
	// controlled runs the suite with budget controls and counts any failure, for a change of code that a
	// parse without them does not run.
	controlled bool
}

// tsMutate returns the library source with the mutant applied.
func tsMutate(t *testing.T, m tsMutant) string {
	t.Helper()
	src, ok := mutateTSFunc(tsConformLib(t), m.sig, m.old, m.new, m.last)
	if !ok {
		t.Fatalf("the library has no %s with %q", m.sig, m.old)
	}
	return src
}

// TestTypeScriptConformanceExecDetectsMutants is the negative control of TestTypeScriptConformanceExec:
// with the library changed in one way each, the suite fails.
func TestTypeScriptConformanceExecDetectsMutants(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	node := codegentest.RequireNode(t)
	mutants := []tsMutant{
		// The memo.
		{name: "a lookup ignores the level", sig: "memoGet(rule: number", old: "e.min === min", new: "true", suite: 0},
		{name: "a store replaces the entry of another level", sig: "memoPut(rule: number", old: "x.min === min", new: "true", suite: 0},
		{name: "a store in a silent context narrows the entry", sig: "joinStored(x: MemoEntry", old: "if (!x.silent && e.silent) return x;", new: "", suite: 0},
		{name: "a store keeps the entry of the silent context", sig: "joinStored(x: MemoEntry", old: "if (!x.silent && e.silent) return x;", new: "if (x.silent && !e.silent) return x;", suite: 0},
		{name: "a lookup charges each entry twice", sig: "memoGet(rule: number", old: "this.spend();", new: "this.spend(2);", suite: 0, charges: true},
		// Deferred memoization.
		{name: "a rule is never eager", sig: "seenFirst(s: number", old: "this.eager[s] = 1;", new: "", suite: 0},
		{name: "a rule becomes eager later", sig: "seenFirst(s: number", old: "(calls >> 4)", new: "(calls >> 3)", suite: 0},
		// The variable environment.
		{name: "an equal binding makes a new environment", sig: "bindValue(slot: number", old: "if (control === null ? old === val : equalValuesSpend(old, val, control)) return;", new: "", suite: 1},
		{name: "a lookup reads another slot", sig: "variable(slot: number", old: "envGet(this.env!, slot, this.envDepth)", new: "envGet(this.env!, 0, this.envDepth)", suite: 1},
		{name: "a key leaves out all but its first variable", sig: "envValues(slots: number[]", old: "this.variable(slots[i]!)", new: "this.variable(slots[0]!)", suite: 1},
		{name: "a lookup charges nothing", sig: "variable(slot: number", old: "this.spend(this.envDepth + 1);", new: "", suite: 1, charges: true},
		{name: "a key charges nothing for its reads", sig: "envValues(slots: number[]", old: "this.spend();", new: "", suite: 1, charges: true},
		// The recovered errors.
		{name: "since leaves the entries in place", sig: "recSince(rec: number", old: "this.recovered.length = rec;", new: "", suite: 2},
		{name: "since charges nothing for the copy", sig: "recSince(rec: number", old: "copyEntriesSpend(this.recovered, rec, end, this.control)", new: "this.recovered.slice(rec)", suite: 2, charges: true},
		{name: "add records nothing", sig: "recAdd(errs: Errs", old: "this.recovered.push(errs);", new: "", suite: 2},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			lib := tsMutate(t, m)
			s := tsExecSuites(m.charges, false)[m.suite]
			runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
			f := tsconformtest.Exec(m.charges)
			res := tsConformRun(runner, f, s, 150, 150)
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
