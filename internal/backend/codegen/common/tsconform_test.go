package common_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"

	"zeroparse.dev/zp/internal/backend/codegen/common"
	"zeroparse.dev/zp/internal/ir/code/exec/conform"
	"zeroparse.dev/zp/internal/ir/code/exec/ref"
	"zeroparse.dev/zp/internal/testing/codegentest"
	"zeroparse.dev/zp/internal/testing/tsconformtest"
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
		code, err := codegentest.GenerateTSBundle(g, common.GenOptions{Start: "main", NoOpt: []string{common.UsedFeatures}})
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
		{Name: name + " memo", Ops: with(memoOps)},
		{Name: name + " environment", Ops: with(envOps), Exact: []string{"env.lookup", "env.bind", "env.key", "budget.chargeKey"}},
		{Name: name + " recovered errors", Ops: with(recOps), Exact: recOps},
	}
}

// tsChargeDivergences are the operations whose charges in the library differ from the model's, each
// with the difference. The charge checks of the suite stop a case at the first call that differs, so the
// suite holds these operations to everything but their charges and tolerates the failure that charges
// cause: a difference of charges or the interruption that follows from one. A failure of any other
// kind, or at any other operation, fails the test. Each reason starts with the name of the divergence it covers
// (listed with a reproduction in the #305 hand-off); when the library holds the model's charges there,
// the entry goes.
var tsChargeDivergences = map[string]string{
	"env.bind": "[env.bind string compare] comparing a string with the value of the slot charges one unit for each byte (the model: two for each byte of strings that are not the same memory, and one for strings that are)",
}

// tsInputChargeDivergences are those of a parse of an input of some length, where the memo has one
// slot for each position.
var tsInputChargeDivergences = map[string]string{}

// tsRuleChargeDivergences are those of the rules: rule.invoke with captures charges more than its cost
// states.
var tsRuleChargeDivergences = map[string]string{
	"rule.invoke": "[rule.invoke capture charges] the frame, the search for the captures that were set and the walk of the names each charge one unit for every capture the rule declares, where the cost states one unit for each capture and each field set",
}

// tsReport fails the test for each failure of res that is not a known difference of charges.
func tsReport(t *testing.T, res tsConformResult, ops []string, known map[string]string) {
	t.Helper()
	t.Logf("%d cases ran, %d skipped, %d pending, %d failed", res.ran, res.skipped, res.pending, len(res.fails))
	seen := map[string]int{}
	for _, err := range res.fails {
		op, kind := tsFailureKind(err)
		switch kind {
		case "bound", "exact", "limit", "interrupt":
			if _, ok := known[op]; ok {
				seen[op]++
				continue
			}
		}
		t.Errorf("%s at %s: %v", kind, op, err)
	}
	for op := range known {
		if seen[op] == 0 && slices.Contains(ops, op) {
			t.Logf("%s no longer differs in charges in this suite", op)
		}
	}
	for op, n := range seen {
		t.Logf("known difference of charges: %s in %d cases: %s", op, n, known[op])
	}
}

func TestTypeScriptConformanceExec(t *testing.T) {
	node := codegentest.RequireNode(t)
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
					known := map[string]string{}
					if controlled {
						known = tsChargeDivergences
					}
					tsReport(t, res, s.Ops, known)
				})
			}
		}
	}
}

// tsFailureKind returns the operation of the call at which a case failed and the kind of the
// difference: "bound" and "exact" for the charges of the call, "limit" for the interruption that
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
	node := codegentest.RequireNode(t)
	lib := tsConformLib(t)
	s := tsExecSuites(true, false)[0]
	runner := tsconformtest.Runner{Session: tsconformtest.Start(t, node, lib)}
	f := tsconformtest.Exec(true)
	f.Options = map[string]any{"input": 100}
	res := tsConformRun(runner, f, s, 100, 150)
	known := map[string]string{}
	for op, reason := range tsChargeDivergences {
		known[op] = reason
	}
	for op, reason := range tsInputChargeDivergences {
		known[op] = reason
	}
	tsReport(t, res, s.Ops, known)
}

// TestTypeScriptConformancePrintedOps pins the operations that the harness does not run as printed
// code. The harness runs every other operation of the suites through the method that the printer prints
// for it (print.OperationMethods), so these are all that the cases run on the library's own state.
//
// The printer never emits them, which tsTarget.Op in print/exec_lib.go shows: it panics with "no
// TypeScript implementation" for each name. term.elemsAbove and term.keep are emitted only by the term
// compiler of the Go state engine (actionlower.go); rule.docMemo and rule.positional only by the
// DocumentParser target; obs.active, obs.enter and obs.exit have no printed form of their own (the printer
// emits the observation hooks inside the printed rule code); env.scope, env.lookup, rec.push and
// budget.used are support operations of the cases.
func TestTypeScriptConformancePrintedOps(t *testing.T) {
	h, err := tsconformtest.Printed()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"budget.used", "env.lookup", "env.scope", "obs.active", "obs.enter", "obs.exit", "rec.push", "rule.docMemo", "rule.positional", "term.elemsAbove", "term.keep"}
	var got []string
	for name, reason := range h.Unprinted {
		got = append(got, name)
		if !strings.Contains(reason, "no TypeScript implementation") {
			t.Errorf("%s is not printed for a reason other than that the printer has no implementation: %s", name, reason)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("the operations that are not printed are\n%v\nwant\n%v", got, want)
	}
}
