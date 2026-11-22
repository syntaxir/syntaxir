package conform

import (
	"math/rand/v2"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
)

var actionStateModule = ActionStateModule()

// actionStateFamily is the cases of the elements gathered or of the values made.
type actionStateFamily struct {
	name   string
	ops    []string
	cases  func() []Case
	random func(name string, rng *rand.Rand, n int) Case
	broken func() []ActionStateBroken
}

var actionStateFamilies = []actionStateFamily{
	{"gathered", GatheredOps, GatheredCases, RandomGathered, GatheredBrokenOps},
	{"made", MadeOps, MadeCases, RandomMade, MadeBrokenOps},
}

const actionStateSeeds, actionStateCalls = 200, 120

func actionStateSuite(fam actionStateFamily, mu *mutant) Suite {
	return Suite{Name: fam.name, Ops: fam.ops, New: func(c ref.Config) Impl {
		if mu == nil {
			return modelImpl{ref.New(c)}
		}
		mu.modelImpl = modelImpl{ref.New(c)}
		return *mu
	}}
}

func actionStateFails(fam actionStateFamily, s Suite) (handwritten, random bool) {
	for _, c := range fam.cases() {
		if _, err := Run(s, c); err != nil {
			handwritten = true
		}
	}
	for seed := uint64(0); seed < actionStateSeeds && !random; seed++ {
		_, err := Run(s, fam.random("random", rand.New(rand.NewPCG(seed, 13)), actionStateCalls))
		random = err != nil
	}
	return handwritten, random
}

func TestActionStateCasesVerify(t *testing.T) {
	for _, fam := range actionStateFamilies {
		if err := code.Verify(fam.cases()[0].Module); err != nil {
			t.Fatal(fam.name, err)
		}
		if err := code.Verify(fam.random("random", rand.New(rand.NewPCG(1, 2)), 200).Module); err != nil {
			t.Fatal(fam.name, err)
		}
	}
}

func TestModelPassesActionStateCases(t *testing.T) {
	for _, fam := range actionStateFamilies {
		s := actionStateSuite(fam, nil)
		for _, c := range fam.cases() {
			if skipped, err := Run(s, c); err != nil || skipped {
				t.Errorf("%s: skipped %v, %v", c.Func.Name, skipped, err)
			}
		}
		for seed := uint64(0); seed < actionStateSeeds; seed++ {
			if _, err := Run(s, fam.random("random", rand.New(rand.NewPCG(seed, 13)), actionStateCalls)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestRandomActionStateCasesUseTheOperations checks that randomized cases exercise every operation.
func TestRandomActionStateCasesUseTheOperations(t *testing.T) {
	for _, fam := range actionStateFamilies {
		counts := map[string]int{}
		for seed := uint64(0); seed < actionStateSeeds; seed++ {
			c := fam.random("random", rand.New(rand.NewPCG(seed, 13)), actionStateCalls)
			walkCalls(c.Func.Body, func(x *code.Call) { counts[x.Callee]++ })
		}
		for _, name := range fam.ops {
			if counts[name] < 50 {
				t.Errorf("randomized cases call %s %d times", name, counts[name])
			}
		}
	}
}

// TestActionStateMutantsFail is the negative control of the cases of the elements gathered and the values
// made: each broken model fails a handwritten and a randomized case.
func TestActionStateMutantsFail(t *testing.T) {
	for _, fam := range actionStateFamilies {
		for _, br := range fam.broken() {
			t.Run(br.Name, func(t *testing.T) {
				mu := mutant{op: br.Op, edit: func(m *ref.Model, a []any) []any {
					return br.Edit(func(op string, args ...any) []any { return m.Call(actionStateModule.Op(op), args) }, a)
				}}
				handwritten, random := actionStateFails(fam, actionStateSuite(fam, &mu))
				if !handwritten {
					t.Error("no handwritten case detected the mutant")
				}
				if !random {
					t.Error("no randomized case detected the mutant")
				}
			})
		}
	}
}
