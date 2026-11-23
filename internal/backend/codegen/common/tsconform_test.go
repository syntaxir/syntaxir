package common_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/internal/testing/tsconformtest"
)

// The conformance suite of the parser VM (internal/ir/code/exec/conform) on the generated TypeScript
// library, by trace replay (internal/testing/tsconformtest): the cases and randomized sequences of
// the suite that run on generated Go run here against the library's own functions.

var (
	tsLibOnce sync.Once
	tsLibSrc  string
	tsLibErr  error
)

// tsConformLib returns the source of the whole TypeScript library as a bundle: with the used features
// off, so that it holds the functions that no parser of its grammar uses.
func tsConformLib(t testing.TB) string {
	t.Helper()
	tsLibOnce.Do(func() {
		g, err := parseTest("@start\nrule main = \"a\"\n")
		if err != nil {
			tsLibErr = err
			return
		}
		code, err := codegentest.GenerateTSBundle(g, common.GenOptions{Start: "main", NoOpt: []string{codegen.UsedFeatures}})
		tsLibSrc, tsLibErr = string(code), err
	})
	if tsLibErr != nil {
		t.Fatal(tsLibErr)
	}
	return tsLibSrc
}

// tsConformResult is what a run of the cases of a suite found.
type tsConformResult struct {
	ran, skipped, pending int
	fails                 []error
}

// tsConformRun runs the handwritten cases and seeds randomized cases of n calls of the suite s on the
// library, as the suites of generated Go run them.
func tsConformRun(r tsconformtest.Runner, f tsconformtest.Family, s conform.Suite, seeds, n int) tsConformResult {
	return tsConformRunCases(r, f, s, conform.Cases(), seeds, func(seed int) conform.Case {
		// One case in four has a limit that the case may reach; the others run to their end.
		var limit uint64
		if seed%4 == 0 {
			limit = uint64(200 + seed)
		}
		rng := rand.New(rand.NewPCG(uint64(seed), 65))
		return conform.Random(fmt.Sprint("random_", seed), rng, s.Ops, n, ref.Config{Rules: 3 + seed%2*130, Limit: limit, MaxDepth: 4, Vars: conform.RandomVars(seed), RuleVars: conform.RandomRuleVars})
	})
}

// tsConformRunCases runs the handwritten cases and the cases that random draws for the seeds.
func tsConformRunCases(r tsconformtest.Runner, f tsconformtest.Family, s conform.Suite, cases []conform.Case, seeds int, random func(seed int) conform.Case) tsConformResult {
	var res tsConformResult
	run := func(c conform.Case) {
		status, err := r.Run(f, s, c)
		switch status {
		case tsconformtest.Skipped:
			res.skipped++
		case tsconformtest.Pending:
			res.pending++
		default:
			res.ran++
		}
		if err != nil {
			res.fails = append(res.fails, err)
		}
	}
	for _, c := range cases {
		run(c)
	}
	for seed := range seeds {
		run(random(seed))
	}
	return res
}

// tsExecSuites are the suites of the operations of the memo, the variable environment and the
// recovered errors that the library implements.
func tsExecSuites(controlled, sparse bool) []conform.Suite {
	memoOps := []string{"memo.lookup", "memo.store", "memo.ok", "memo.end", "memo.silent", "seen.first"}
	envOps := []string{"env.lookup", "env.bind", "env.scope", "env.restore", "env.key", "memo.lookup", "memo.store"}
	recOps := []string{"rec.mark", "rec.push", "rec.truncate", "rec.since", "rec.add", "rec.none"}
	with := func(ops []string) []string {
		if controlled {
			return append(ops, tsconformtest.ExecBudgetOps...)
		}
		return ops
	}
	name := fmt.Sprintf("typescript (controlled=%v, sparse seen=%v)", controlled, sparse)
	return []conform.Suite{
		{Name: name + " memo", Ops: with(memoOps), Free: memoOps},
		{Name: name + " environment", Ops: with(envOps), Free: []string{"env.lookup", "env.bind", "env.key", "memo.lookup", "memo.store"}, Exact: []string{"budget.chargeKey"}},
		{Name: name + " recovered errors", Ops: with(recOps), Free: recOps},
	}
}

// tsReport fails the test for each failure of res.
func tsReport(t *testing.T, res tsConformResult) {
	t.Helper()
	t.Logf("%d cases ran, %d skipped, %d pending, %d failed", res.ran, res.skipped, res.pending, len(res.fails))
	for _, err := range res.fails {
		op, kind := tsFailureKind(err)
		t.Errorf("%s at %s: %v", kind, op, err)
	}
}

func TestTypeScriptConformanceExec(t *testing.T) {
	node := tooltest.RequireNode(t)
	lib := tsConformLib(t)
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	for _, controlled := range []bool{false, true} {
		for _, sparse := range []bool{false, true} {
			for _, s := range tsExecSuites(controlled, sparse) {
				t.Run(s.Name, func(t *testing.T) {
					t.Parallel()
					runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
					f := tsconformtest.Exec(controlled)
					f.Options = map[string]any{"sparse": sparse}
					res := tsConformRun(runner, f, s, seeds, 150)
					if res.ran < seeds {
						t.Fatalf("only %d cases ran", res.ran)
					}
					tsReport(t, res)
				})
			}
		}
	}
}

// tsFailureKind returns the operation of the call at which a case failed and the kind of the
// difference: "bound", "exact" and "free" for the units of the call, "limit" for the interruption that
// follows from them, "result" for a handle or a value, "interrupt" for another interruption, and
// "other" for the rest.
func tsFailureKind(err error) (op, kind string) {
	f, ok := err.(*conform.Failure)
	if !ok {
		return "", "harness"
	}
	if n := len(f.Trace); n > 0 {
		op, _, _ = strings.Cut(f.Trace[n-1], "(")
	}
	switch {
	case strings.Contains(f.Msg, "more than the bound"):
		kind = "bound"
	case strings.Contains(f.Msg, "not the exact cost"):
		kind = "exact"
	case strings.Contains(f.Msg, "is no event"):
		kind = "free"
	case strings.Contains(f.Msg, "past the limit"), strings.Contains(f.Msg, "interrupted by the limit"):
		kind = "limit"
	case strings.HasPrefix(f.Msg, "interrupted:"):
		kind = "interrupt"
	case strings.Contains(f.Msg, "result "), strings.Contains(f.Msg, "handle"):
		kind = "result"
	default:
		kind = "other"
	}
	return op, kind
}

// TestTypeScriptConformanceMemoInput runs the memo suite on a parse of an input of 100 units, whose memo
// has a slot for each position from the first store on.
func TestTypeScriptConformanceMemoInput(t *testing.T) {
	node := tooltest.RequireNode(t)
	lib := tsConformLib(t)
	s := tsExecSuites(true, false)[0]
	runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
	f := tsconformtest.Exec(true)
	f.Options = map[string]any{"input": 100}
	res := tsConformRun(runner, f, s, 100, 150)
	tsReport(t, res)
}

// TestTypeScriptConformancePrintedOps pins the operations that the harness does not run as printed
// code, in each class of the printed operations (print.OperationClasses). The harness runs every other
// operation of the suites through the method that the printer prints for it in the class of the case's
// parser (print.OperationMethods), so these are all that the cases run on the library's own state.
//
// The printer never emits them in that class, which the targets in print/exec_lib.go show: tsTarget.Op
// panics with "no TypeScript implementation" for each name, and tsObsTarget.Op with "the Document operation"
// for the Document bookkeeping. action.elemsAbove and action.keep are emitted only by the action expression compiler of the
// Go state engine (actionlower.go); rule.docMemo, rule.positional and the doc operations only by the
// DocumentParser and ObservedParser targets, since the batch Parser reads no Document and tracks nothing;
// obs.active, obs.enter and obs.exit have no printed form of their own (the printer emits the observation
// hooks inside the printed rule code); env.lookup, rec.push and budget.used are support
// operations of the cases, and budget.charge and budget.chargeKey too: the TypeScript parser has no
// budget form, and the harness counts their units in the parser's counter of compute units. An observed parse without a Document keeps no start of the examined input and
// no statistics.
func TestTypeScriptConformancePrintedOps(t *testing.T) {
	h, err := tsconformtest.Printed()
	if err != nil {
		t.Fatal(err)
	}
	classes := []struct {
		name, reason string
		want         []string
	}{
		{"PrintedParser", "no TypeScript implementation", append([]string{"budget.charge", "budget.chargeKey", "budget.used", "env.lookup", "obs.active", "obs.enter", "obs.exit", "rec.push", "rule.docMemo", "rule.positional", "action.elemsAbove", "action.keep"}, tsconformtest.DocOps...)},
		{"PrintedDocumentParser", "", nil},
		{"PrintedObservedParser", "the Document operation", []string{"doc.evaluated", "doc.low", "doc.reused", "doc.touchFrom"}},
	}
	for _, c := range classes {
		unprinted, ok := h.Unprinted[c.name]
		if !ok {
			t.Errorf("the printed operations have no class %s", c.name)
			continue
		}
		var got []string
		for name, reason := range unprinted {
			got = append(got, name)
			if !strings.Contains(reason, c.reason) {
				t.Errorf("%s: %s is not printed for a reason other than %q: %s", c.name, name, c.reason, reason)
			}
		}
		slices.Sort(got)
		want := slices.Sorted(slices.Values(c.want))
		if !slices.Equal(got, want) {
			t.Errorf("%s: the operations that are not printed are\n%v\nwant\n%v", c.name, got, want)
		}
	}
}
