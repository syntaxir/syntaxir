package state_test

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// depMemoGrammars returns the grammars that the dependency memo tests compare
// (codegentest.DependencyMemoCases), without the inputs of limitBoundReferences in a build without
// budget controls.
func depMemoGrammars(seeds int) []codegentest.Case {
	return withoutLimitBoundReferences(codegentest.DependencyMemoCases(seeds))
}

// limitBoundReferences names the inputs of grammars on which the evaluation without a memo, which
// evaluates every call in a context again, takes time exponential in the number of the grammar's
// nested leaders: only the compute limit ends it. The comparisons accept a result that a limit
// ended (endedByResourceLimit), so these inputs check the memo against a reference only where a
// limit applies. In a build without budget controls no limit applies and the evaluation does not
// finish, so the tests leave these inputs out there. checkReferenceLimit keeps the list exact.
var limitBoundReferences = map[string][]string{
	"seven dependencies and 34 leaders":                         {"axy", "aaxx", "aaaxxx", "axq", "aaxzx", "a"},
	"random cycle grammar 341 with cuts and 63 padding leaders": {"a", "accaab"},
}

// withoutLimitBoundReferences returns cases without the inputs of limitBoundReferences in a build
// without budget controls, and cases otherwise.
func withoutLimitBoundReferences(cases []codegentest.Case) []codegentest.Case {
	if state.BudgetControlsEnabled {
		return cases
	}
	kept := make([]codegentest.Case, 0, len(cases))
	for _, c := range cases {
		if bound := limitBoundReferences[c.Name]; len(bound) > 0 {
			c.Inputs = slices.DeleteFunc(slices.Clone(c.Inputs), func(in string) bool { return slices.Contains(bound, in) })
		}
		kept = append(kept, c)
	}
	return kept
}

// checkReferenceLimit reports an error if the result of the evaluation without a memo of the
// grammar of c on in, want, was ended by a limit and limitBoundReferences does not name the input: a
// build without budget controls would not finish that evaluation.
func checkReferenceLimit(t *testing.T, c codegentest.Case, in, want string) {
	t.Helper()
	if endedByResourceLimit(want, "") && !slices.Contains(limitBoundReferences[c.Name], in) {
		t.Errorf("%s %q: the evaluation without a memo ended by a limit; name the input in limitBoundReferences", c.Name, in)
	}
}

// depMemoDiffers compares, on every backend, the results of the grammar with the dependency
// memo and the results of the grammar compiled without a memo that evaluates every call in a
// context again (engine.ParseOptions.ContextMemoOff), and returns the differences, ignoring an input that
// exhausts a limit in either. With examined, the memo puts examined ranges into identities and
// outcomes, as a Document parse does (engine.ParseOptions.ContextMemoExamined). It returns false if the grammar does not
// compile. It stops at the first difference unless parallel: then it runs every comparison
// concurrently, because the evaluation without a memo of one input can take minutes (without limit
// controls, no limit ends it) and the comparisons are independent, and returns the differences in
// the order of the sequential run.
func depMemoDiffers(t *testing.T, c codegentest.Case, limit int, examined, parallel bool, tune state.Tuning) ([]string, bool) {
	t.Helper()
	g, err := enginetest.Parse(c.Src)
	if err != nil {
		return nil, false
	}
	if _, err := entry.Compile(g, entry.Options{}); err != nil {
		return nil, false
	}
	memo, ref := enginetest.MustCompile(t, c.Src), enginetest.MustCompile(t, c.Src, check.Options{DisableMemo: true})
	type job struct {
		in      string
		backend engine.Backend
		rec     bool
	}
	var jobs []job
	for _, in := range c.Inputs {
		for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			for _, recognize := range []bool{false, true} {
				jobs = append(jobs, job{in, backend, recognize})
			}
		}
	}
	compare := func(j job) string {
		o := engine.ParseOptions{Backend: j.backend, Recognize: j.rec, ContextMemoExamined: examined, Tune: tune}
		got, err := memo.ParseWith("", j.in, o)
		o.ContextMemoExamined, o.ContextMemoOff = false, true
		want, wantErr := ref.ParseWith("", j.in, o)
		a, b := codegentest.ResultJSON(got, err), codegentest.ResultJSON(want, wantErr)
		checkReferenceLimit(t, c, j.in, b)
		if a != b && !endedByResourceLimit(a, b) {
			return fmt.Sprintf("%s %q %s recognize=%v:\n got %s\nwant %s", c.Name, j.in, j.backend, j.rec, a, b)
		}
		return ""
	}
	results := make([]string, len(jobs))
	if parallel {
		var wg sync.WaitGroup
		sem := make(chan struct{}, runtime.GOMAXPROCS(0))
		for i, j := range jobs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				results[i] = compare(j)
			}()
		}
		wg.Wait()
	} else {
		for i, j := range jobs {
			if results[i] = compare(j); results[i] != "" && countNonEmpty(results) >= limit {
				break
			}
		}
	}
	var diffs []string
	for _, d := range results {
		if d != "" {
			if diffs = append(diffs, d); len(diffs) >= limit {
				break
			}
		}
	}
	return diffs, true
}

// endedByResourceLimit reports whether either result is the compute or memory limit error of the
// parse (limits.Error), which may stop two parses of one input at different points. Other errors
// that mention a limit, such as the missing-token insertion limit, are real differences.
func endedByResourceLimit(a, b string) bool {
	both := a + b
	return strings.Contains(both, "parse compute limit exceeded") || strings.Contains(both, "parse memory limit exceeded")
}

func countNonEmpty(s []string) int {
	n := 0
	for _, x := range s {
		if x != "" {
			n++
		}
	}
	return n
}

// TestLeftRecursionDependencyMemoRandom compares the results of random grammars with left
// recursion through several leaders with the results of evaluating every call in a context again.
// It pins "Evaluations of Rules" and the rule of "Growths of Left Recursion" in
// spec/runtime/parsing.md that a run of steps taken at once holds only steps whose result became the
// current result: a run that held the stopping step would end a growth farther than evaluating it.
func TestLeftRecursionDependencyMemoRandom(t *testing.T) {
	t.Parallel()
	seeds := 800
	if testing.Short() {
		seeds = 80
	}
	var compiled, n atomic.Int64
	codegentest.EachParallel(t, depMemoGrammars(seeds), func(t *testing.T, c codegentest.Case) {
		diffs, ok := depMemoDiffers(t, c, 3, true, true, state.Tuning{})
		if !ok {
			return
		}
		compiled.Add(1)
		for _, d := range diffs {
			if n.Add(1) <= 5 {
				t.Errorf("%s\n%s", c.Src, d)
			}
		}
	})
	if n.Load() > 0 || int(compiled.Load()) < seeds*2 {
		t.Errorf("%d differences in %d grammars", n.Load(), compiled.Load())
	}
}

// TestLeftRecursionDependencyMemoControls checks that the random grammars of
// TestLeftRecursionDependencyMemoRandom detect each check of the dependency memo: with the check
// broken, some result differs from evaluating every call in a context again. It changes a global
// and so does not run in parallel.
func TestLeftRecursionDependencyMemoControls(t *testing.T) {
	seeds := 400
	if testing.Short() {
		// A smaller search, with the grammars known to detect the check that the search does not
		// reach in so few seeds (codegentest.DependencyMemoControlSeeds).
		seeds = 120
	}
	cases := depMemoGrammars(seeds)
	if testing.Short() {
		cases = append(cases, withoutLimitBoundReferences(codegentest.DependencyMemoControlCases())...)
	}
	for _, m := range []struct {
		name     string
		mutation int
		examined bool // a difference shows with examined ranges; the others show without them
	}{
		{"seed identities", state.CtxIgnoreSeeds, true},
		{"observations", state.CtxIgnoreObs, true},
		{"present growths", state.CtxIgnorePresent, true},
		{"canonical seeds", state.CtxNoCanonical, true},
		{"jumps over observations of the growing seed", state.CtxJumpAnySeed, true},
		{"wide present identity", state.CtxConstWide, true},
		{"replayed observations", state.CtxStaleReplay, false},
		{"observations replayed in the current context", state.CtxNoReplayObs, false},
	} {
		t.Run(m.name, func(t *testing.T) {
			for _, c := range cases {
				if diffs, _ := depMemoDiffers(t, c, 1, m.examined, false, state.Tuning{MemoMut: m.mutation}); len(diffs) > 0 {
					return
				}
			}
			t.Errorf("no difference in %d grammars", len(cases))
		})
	}
}

// TestLeftRecursionDependencyMemoLinear checks that the shapes of codegentest.DependencyMemoShapes
// evaluate a number of calls linear in the length of the input on every backend: doubling the input
// at most a little more than doubles the evaluations.
func TestLeftRecursionDependencyMemoLinear(t *testing.T) {
	t.Parallel()
	for _, s := range codegentest.DependencyMemoShapes {
		prog := enginetest.MustCompile(t, "@start\nrule main = "+s.Rules, check.Options{SourceInfo: true})
		for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			evals := func(n int) int {
				var pr instrument.Profile
				if _, err := prog.ParseWith("", s.Input(n), engine.ParseOptions{Backend: b, Profiles: []*instrument.Profile{&pr}}); err != nil {
					t.Fatalf("%s %s n=%d: %v", s.Name, b, n, err)
				}
				return pr.Snapshot().Evals
			}
			small, large := evals(80), evals(160)
			if large > small*5/2 {
				t.Errorf("%s %s: %d evaluations at 80, %d at 160", s.Name, b, small, large)
			}
		}
	}
}

// TestLeftRecursionDependencyMemoDefaultBudget checks that the shapes of
// codegentest.DependencyMemoShapes parse a long input within the default budget, which grows linearly
// with the input, on every backend. The budget also counts the work that evaluations do not show,
// such as the scan of the jumps recorded for a seed.
func TestLeftRecursionDependencyMemoDefaultBudget(t *testing.T) {
	t.Parallel()
	for _, s := range codegentest.DependencyMemoShapes {
		prog := enginetest.MustCompile(t, "@start\nrule main = "+s.Rules)
		in := s.Input(10000)
		for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			if _, err := prog.ParseWith("", in, engine.ParseOptions{Backend: b}); err != nil {
				t.Errorf("%s %s n=10000: %v", s.Name, b, err)
			}
		}
	}
}

// TestLeftRecursionDependencyMemoUnexamined checks that leaving the examined range out of the
// identities of growth steps and the outcomes of lookaheads, as every parse but a Document's does
// (spec/runtime/parsing.md, Evaluations of Rules), changes only what is evaluated: the random
// grammars give the results of evaluating every call in a context again, and on mutual leaders an
// observed parse takes fewer steps than one that keeps them apart by their examined ranges
// (engine.ParseOptions.ContextMemoExamined), as a Document parse does.
func TestLeftRecursionDependencyMemoUnexamined(t *testing.T) {
	t.Parallel()
	seeds := 200
	if testing.Short() {
		seeds = 40
	}
	n := 0
	compiled := 0
	for _, c := range depMemoGrammars(seeds) {
		if g, err := enginetest.Parse(c.Src); err != nil {
			continue
		} else if _, err := entry.Compile(g, entry.Options{}); err != nil {
			continue
		}
		compiled++
		memo, ref := enginetest.MustCompile(t, c.Src), enginetest.MustCompile(t, c.Src, check.Options{DisableMemo: true})
		for _, in := range c.Inputs {
			for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
				got, err := memo.ParseWith("", in, engine.ParseOptions{Backend: backend})
				want, wantErr := ref.ParseWith("", in, engine.ParseOptions{Backend: backend, ContextMemoOff: true})
				a, b := codegentest.ResultJSON(got, err), codegentest.ResultJSON(want, wantErr)
				checkReferenceLimit(t, c, in, b)
				if a != b && !endedByResourceLimit(a, b) {
					if n++; n <= 5 {
						t.Errorf("%s\n%q %s:\n got %s\nwant %s", c.Src, in, backend, a, b)
					}
				}
			}
		}
	}
	if n > 0 || compiled < seeds*2 {
		t.Errorf("%d differences in %d grammars", n, compiled)
	}
	prog := enginetest.MustCompile(t, "rule main = a $$\nrule a = a \"x\" / b \"y\" / \"n\"\nrule b = b \"z\" / a \"w\" / \"m\"")
	for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		evals := func(examined bool) int {
			var pr instrument.Profile
			if _, err := prog.ParseWith("", "mzy", engine.ParseOptions{Backend: b, Profiles: []*instrument.Profile{&pr}, ContextMemoExamined: examined}); err != nil {
				t.Fatalf("%s: %v", b, err)
			}
			return pr.Snapshot().Evals
		}
		if loose, strict := evals(false), evals(true); loose >= strict {
			t.Errorf("%s: %d evaluations without examined ranges, %d with them", b, loose, strict)
		}
	}
}

// TestEndedByResourceLimit checks that the filter of depMemoDiffers skips only the errors of an
// exhausted compute or memory limit, as the parser reports them, and not the missing-token
// insertion limit: a store that keeps results of rules that insert tokens differs from the
// reference exactly there.
func TestEndedByResourceLimit(t *testing.T) {
	prog := enginetest.MustCompile(t, "@start\nrule main = \"a\" #missing \"b\" #missing \"c\" #missing")
	got, err := prog.ParseWith("", "", engine.ParseOptions{Backend: engine.Closure, MissingTokens: 1})
	ins := codegentest.ResultJSON(got, err)
	if !strings.Contains(ins, "insertion limit") || endedByResourceLimit(ins, "") || endedByResourceLimit("", ins) {
		t.Errorf("the insertion limit error %s must not count as a resource limit", ins)
	}
	// The reference build without budget controls rejects a limit, so there the classifier reads
	// the error that a parse ended by the compute limit returns.
	res := codegentest.ResultJSON(nil, &sxirruntime.LimitError{Kind: sxirruntime.LimitCompute, Limit: 5, Used: 4, Requested: 2})
	if state.BudgetControlsEnabled {
		loop := enginetest.MustCompile(t, "@start\nrule main = \"a\"+")
		res = codegentest.ResultJSON(loop.ParseWith("", strings.Repeat("a", 100), engine.ParseOptions{Backend: engine.Closure, Limits: sxirruntime.Limits{Compute: 5}}))
	}
	if !strings.Contains(res, "compute limit") || !endedByResourceLimit(res, "x") || !endedByResourceLimit("x", res) {
		t.Errorf("the compute limit error %s must count as a resource limit", res)
	}
	if endedByResourceLimit(`{"err":"1:1: syntax error"}`, `{"err":"budget"}`) {
		t.Error("an unrelated error counted as a resource limit")
	}
}
