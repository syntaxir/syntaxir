package bench

import (
	"context"
	"math"
	"os"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/bench/inputs"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/lint"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
)

// The derived budget of a Linear grammar (design record 079) bounds the compute units of a parse:
// c*n + k for an input of n position units. These tests hold the bound against the units that
// generated Go counts under logical compute units (design record 076), on the grammars of the
// logical-unit corpus that the classifier classifies as Linear, on valid, truncated and damaged
// inputs, at several sizes, in both position units, for Parse and ParseValue.

type budgetCase struct {
	name, grammar string
	inputs        []string
	parse         [2]unitsParse
}

// budgetCases returns the Linear grammars of the logical-unit corpus, the grammar with #recover
// among them. XML (unknown), chain (polynomial)
// and keyed (linear-env) have no budget, which TestBudgetCorpusClasses checks.
func budgetCases() []budgetCase {
	var out []budgetCase
	for _, c := range exactCases() {
		var grammar string
		var ladder []string
		in := c.inputs
		switch c.name {
		case "JSON":
			grammar = "../testdata/grammars/json/json.sxir"
			for _, n := range []int{1 << 6, 1 << 8, 1 << 10, 1 << 12, 1 << 14, 1 << 16} {
				ladder = append(ladder, inputs.JSONInput(n))
			}
		case "policy":
			grammar = "grammars/logical_policy.sxir"
		case "recover":
			// The whole ladder, of which the exact case keeps the small inputs.
			grammar, in = "grammars/budget_recover.sxir", recoverInputs(math.MaxInt)
		default:
			continue
		}
		out = append(out, budgetCase{c.name, grammar, append(ladder, in...), c.logical})
	}
	return out
}

func classifyFile(t *testing.T, path string) *lint.Classification {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	g, err := enginetest.ParseGrammar(string(src))
	if err != nil {
		t.Fatal(err)
	}
	c, err := lint.Classify(context.Background(), g, "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// boundHeld measures every input of c and counts the parses whose units exceed the bound of the
// classification scaled by num/den. It returns the count and, for each unit, the largest ratio of
// measured units to the bound.
func boundHeld(t *testing.T, c budgetCase, b *lint.Budget, num, den uint64) (violations int, ratio map[sxirinput.PositionUnit]float64) {
	t.Helper()
	ratio = map[sxirinput.PositionUnit]float64{}
	unlimited := sxirruntime.WithLimits(sxirruntime.Limits{Compute: sxirruntime.Unlimited})
	for i, in := range c.inputs {
		for _, unit := range exactUnits {
			n := uint64(len(in))
			if unit == sxirinput.CodePoints {
				n = uint64(utf8.RuneCountInString(in))
			}
			bound := (b.PerUnit*n + b.Fixed) * num / den
			for api := range 2 {
				got, _ := countUnits(c.parse[api], in, unit, unlimited)
				if r := float64(got) / float64(bound); r > ratio[unit] {
					ratio[unit] = r
				}
				if got > bound {
					violations++
					if num == den {
						t.Errorf("%s input %d (%d units of input, %v, api %d): measured %d units, bound %d", c.name, i, n, unit, api, got, bound)
					}
				}
			}
		}
	}
	return violations, ratio
}

// TestBudgetSound checks that no parse of the corpus counts more units than the derived bound.
// The largest ratios are logged: they show how loose the bound is.
func TestBudgetSound(t *testing.T) {
	for _, c := range budgetCases() {
		cl := classifyFile(t, c.grammar)
		if cl.Class != lint.Linear || cl.Budget == nil {
			t.Fatalf("%s: class %v with budget %v, want a Linear grammar with a budget", c.name, cl.Class, cl.Budget)
		}
		_, ratio := boundHeld(t, c, cl.Budget, 1, 1)
		t.Logf("%s: bound %d*n+%d; largest measured/bound: bytes %.3f, code points %.3f (%d inputs)", c.name, cl.Budget.PerUnit, cl.Budget.Fixed, ratio[sxirinput.Bytes], ratio[sxirinput.CodePoints], len(c.inputs))
	}
}

// budgetMutantDivisor is the factor by which TestBudgetSoundMutant shrinks the bound. The bound is
// conservative (design record 079, Limitations): on this corpus a bound with half the constants still
// holds, so the mutant divides them by 64, and the test also fails a derivation that has grown
// looser than that.
const budgetMutantDivisor = 64

// TestBudgetSoundMutant checks that the soundness test can fail, and that the bound is within
// budgetMutantDivisor of the units that the corpus counts: a bound with its constants divided by
// that factor is exceeded by some parse of each grammar.
func TestBudgetSoundMutant(t *testing.T) {
	for _, c := range budgetCases() {
		cl := classifyFile(t, c.grammar)
		half, _ := boundHeld(t, c, cl.Budget, 1, 2)
		v, _ := boundHeld(t, c, cl.Budget, 1, budgetMutantDivisor)
		t.Logf("%s: %d parses exceed half the bound, %d exceed 1/%d of it", c.name, half, v, budgetMutantDivisor)
		if v == 0 {
			t.Errorf("%s: no parse exceeds 1/%d of the derived bound: the test cannot tell a bound that is too small, or the bound is looser than that", c.name, budgetMutantDivisor)
		}
	}
}

// TestBudgetCorpusClasses checks the classes that decide which grammars of the logical-unit corpus have
// a budget.
func TestBudgetCorpusClasses(t *testing.T) {
	for _, c := range []struct {
		grammar string
		class   lint.Class
	}{
		{"../testdata/grammars/json/json.sxir", lint.Linear},
		{"grammars/logical_policy.sxir", lint.Linear},
		{"grammars/budget_recover.sxir", lint.Linear},
		{"grammars/logical_keyed.sxir", lint.LinearEnv},
		{"grammars/logical_chain.sxir", lint.Polynomial},
	} {
		cl := classifyFile(t, c.grammar)
		if cl.Class != c.class || (cl.Budget != nil) != (c.class == lint.Linear) {
			t.Errorf("%s: class %v, budget %v; want class %v", c.grammar, cl.Class, cl.Budget, c.class)
		}
	}
}
