package state_test

import (
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// contextStoreGrowth parses the shape at the two codegentest.ContextStoreSizes on the backend and
// returns the evaluations of B at its busiest position and of all rules, at each size. The parses
// have no compute limit, as a context that the store cannot index exceeds the default one, unless
// limited: then they stop at codegentest.ContextStoreBudgetPerUnit for each unit of the input, and
// stopped reports that one did.
func contextStoreGrowth(t *testing.T, prog *engine.Program, s codegentest.DependencyMemoShape, o engine.ParseOptions, limited bool) (b, all [2]int, stopped bool) {
	t.Helper()
	for i, m := range codegentest.ContextStoreSizes {
		var pr instrument.Profile
		o.Profiles = []*instrument.Profile{&pr}
		input := s.Input(m)
		o.Limits = runtime.Limits{Compute: runtime.Unlimited}
		if limited {
			o.Limits.Compute = codegentest.ContextStoreBudgetPerUnit * uint64(len(input))
		}
		// With missing-token insertion, the parse reports the insertion as an error.
		if r, err := prog.ParseWith("", input, o); err != nil && (r == nil || o.MissingTokens == 0) {
			if le := (*runtime.LimitError)(nil); limited && errors.As(err, &le) && le.Kind == runtime.LimitCompute {
				return b, all, true
			}
			t.Fatalf("%s %s m=%d: %v", s.Name, o.Backend, m, err)
		}
		ps := pr.Snapshot()
		all[i] = ps.Evals
		for _, r := range ps.Rules {
			if r.Rule == "B" {
				b[i] = r.MaxEvals
			}
		}
	}
	return b, all, false
}

// contextStoreBounded reports, for the shape on the backend, whether B is evaluated at its busiest
// position as often for the larger input as for the smaller, and the parse evaluates at most
// codegentest.ContextStoreGrowth times the calls. A limited parse that stops at its limit is not
// bounded (contextStoreGrowth).
func contextStoreBounded(t *testing.T, prog *engine.Program, s codegentest.DependencyMemoShape, o engine.ParseOptions, limited bool) (string, bool) {
	b, all, stopped := contextStoreGrowth(t, prog, s, o, limited)
	if stopped {
		return fmt.Sprintf("%s %s: a parse exceeded %d units of compute for each unit of the input",
			s.Name, o.Backend, codegentest.ContextStoreBudgetPerUnit), false
	}
	msg := fmt.Sprintf("%s %s: B evaluated at most %v times at a position, %v evaluations in all, at m=%v",
		s.Name, o.Backend, b, all, codegentest.ContextStoreSizes)
	return msg, b[0] > 0 && b[0] == b[1] && all[1] <= all[0]*codegentest.ContextStoreGrowth
}

// TestContextStoreBounds checks on every backend that a context with more dependencies or more
// leaders than the fixed-size keys of the context store hold still has its results reused: the
// evaluations of a rule at one position do not grow with the input. The parses run under the limit
// of the controls (TestContextStoreBoundsControls), which they must not reach, and without a limit in
// the reference build, which has no budget controls.
func TestContextStoreBounds(t *testing.T) {
	t.Parallel()
	for _, s := range codegentest.ContextStoreShapes {
		prog := enginetest.MustCompile(t, "@start\nrule main = "+s.Rules, check.Options{SourceInfo: true})
		for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			if msg, ok := contextStoreBounded(t, prog, s, engine.ParseOptions{Backend: b}, state.BudgetControlsEnabled); !ok {
				t.Error(msg)
			}
		}
	}
}

// TestContextStoreBoundsControls checks that TestContextStoreBounds detects a context store that
// drops what its fixed-size keys cannot hold. The broken stores may make the parses exponential, so
// they run under a compute limit, which the reference build does not have.
func TestContextStoreBoundsControls(t *testing.T) {
	if !state.BudgetControlsEnabled {
		t.Skip("budget controls are unavailable in the reference build")
	}
	for _, c := range []struct {
		mutation int
		shape    string
	}{
		{state.CtxCapDeps, "seven dependencies"},
		{state.CtxNarrowPresent, "70 leaders"},
	} {
		var s codegentest.DependencyMemoShape
		for _, x := range codegentest.ContextStoreShapes {
			if x.Name == c.shape {
				s = x
			}
		}
		prog := enginetest.MustCompile(t, "@start\nrule main = "+s.Rules, check.Options{SourceInfo: true})
		for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			if msg, ok := contextStoreBounded(t, prog, s, engine.ParseOptions{Backend: b, Tune: state.Tuning{MemoMut: c.mutation}}, true); ok {
				t.Errorf("mutation %d not detected: %s", c.mutation, msg)
			}
		}
	}
}

// missingChain is the shape of TestContextStoreMissingInsertion: a chain whose rules insert nothing,
// in a grammar where main inserts a missing ";" after it.
var missingChain = codegentest.DependencyMemoShape{Name: "missing-token insertion", Rules: codegentest.ContextChain(2, 1, true), Input: codegentest.ContextChainInput}

// TestContextStoreMissingInsertion checks that a parse with missing-token insertion keeps the
// results of rules that insert nothing in contexts: their evaluations at one position do not grow
// with the input, as without insertion.
func TestContextStoreMissingInsertion(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, "@start\nrule main = "+missingChain.Rules, check.Options{SourceInfo: true})
	if msg, ok := contextStoreBounded(t, prog, missingChain, engine.ParseOptions{Backend: engine.Closure, MissingTokens: 1}, false); !ok {
		t.Error(msg)
	}
}

// TestContextStoreMissingInsertionControl checks that TestContextStoreMissingInsertion detects a
// parse with insertion that keeps no result in a context.
func TestContextStoreMissingInsertionControl(t *testing.T) {
	prog := enginetest.MustCompile(t, "@start\nrule main = "+missingChain.Rules, check.Options{SourceInfo: true})
	if msg, ok := contextStoreBounded(t, prog, missingChain, engine.ParseOptions{Backend: engine.Closure, MissingTokens: 1, Tune: state.Tuning{MemoMut: state.CtxStrictMissing}}, false); ok {
		t.Errorf("CtxStrictMissing not detected: %s", msg)
	}
}

// TestContextStoreMissingRandom checks that keeping results in contexts in parses with
// missing-token insertion changes only the cost: random cycle grammars, some of whose literals may
// be inserted, give the same results, errors and expectations as without memos, where some
// rules of the cycles insert tokens and others read their seeds.
func TestContextStoreMissingRandom(t *testing.T) {
	t.Parallel()
	seeds := 40
	if codegentest.Long() {
		seeds = 400
	}
	for _, msg := range missingRandomMismatches(t, seeds, state.Tuning{}) {
		t.Error(msg)
	}
}

// TestContextStoreMissingRandomControl checks that TestContextStoreMissingRandom detects rules
// that insert tokens keeping their results in contexts.
func TestContextStoreMissingRandomControl(t *testing.T) {
	if len(missingRandomMismatches(t, 40, state.Tuning{MemoMut: state.CtxStoreInserts})) == 0 {
		t.Error("CtxStoreInserts not detected")
	}
}

// missingRandomMismatches parses the inputs of seeds random cycle grammars with insertion, with and
// without memos, with the test-only settings tune, and returns the differences.
func missingRandomMismatches(t *testing.T, seeds int, tune state.Tuning) []string {
	t.Helper()
	var out []string
	lit := regexp.MustCompile(`"[a-z]"`)
	checked := 0
	for s := int64(0); s < int64(seeds); s++ {
		c := codegentest.RandomCycleGrammar(s, false)
		rng := rand.New(rand.NewSource(s))
		src := lit.ReplaceAllStringFunc(c.Src, func(l string) string {
			if rng.Intn(3) == 0 {
				return l + " #missing"
			}
			return l
		})
		if !strings.Contains(src, "#missing") {
			continue
		}
		g, err := enginetest.Parse(src)
		if err != nil {
			continue
		}
		if _, err := entry.Compile(g, entry.Options{}); err != nil {
			continue
		}
		checked++
		memo, ref := enginetest.MustCompile(t, src), enginetest.MustCompile(t, src, check.Options{DisableMemo: true})
		for _, in := range c.Inputs {
			for _, recognize := range []bool{false, true} {
				o := engine.ParseOptions{Backend: engine.Closure, Recognize: recognize, MissingTokens: 3, Tune: tune}
				got, err := memo.ParseWith("", in, o)
				o.ContextMemoOff = true
				want, wantErr := ref.ParseWith("", in, o)
				a, b := codegentest.ResultJSON(got, err), codegentest.ResultJSON(want, wantErr)
				// Only the compute and memory limits may end the two parses at different points: rules
				// that insert tokens keep no result, so both make the same insertion attempts.
				if a != b && !strings.Contains(a+b, "compute limit") && !strings.Contains(a+b, "memory limit") {
					out = append(out, fmt.Sprintf("seed %d %q recognize=%v:\n%s\n got %s\nwant %s", s, in, recognize, src, a, b))
				}
			}
		}
	}
	if checked < seeds/4 {
		t.Fatalf("only %d of %d grammars compiled with insertion", checked, seeds)
	}
	return out
}
