package complexity

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/middleend/checkfacts"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/pass"
)

// factComputations returns how many times f computes the rule facts of a module. The count is
// program-wide, so the test that calls it does not run in parallel with others.
func factComputations(f func()) int {
	before := pass.Computations(corepass.FactsKey.Name())
	f()
	return pass.Computations(corepass.FactsKey.Name()) - before
}

// TestClassifierSharesFacts measures the rule-fact computations of the classification: it reads
// the facts that the front end's check took, through the manager of the check's module, where
// checking and then taking the facts of the same rules again computes them twice, and the
// classification of a front-end result computes none.
func TestClassifierSharesFacts(t *testing.T) {
	g, err := parser.Parse("@start\nrule s = xs:x* {len($xs) < 3} \"!\"\nrule x = \"a\" / \"b\" x\n")
	if err != nil {
		t.Fatal(err)
	}
	separate := factComputations(func() {
		if _, err := frontend.Check(g, check.Options{}, nil, desugar.InlineNames{}, checkfacts.Source{}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := NewCostSource(g, nil); err != nil {
			t.Fatal(err)
		}
	})
	shared := factComputations(func() {
		if _, _, err := prepareClassifier(g, "", nil); err != nil {
			t.Fatal(err)
		}
	})
	res, err := frontend.RunDefault(g, checkfacts.Source{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	checked := factComputations(func() {
		if _, err := ClassifyCheckedControlled(res, "", nil); err != nil {
			t.Fatal(err)
		}
	})
	t.Logf("rule-fact computations: %d checking and taking the facts again, %d shared, %d of a front-end result", separate, shared, checked)
	if separate != 2 || shared != 1 || checked != 0 {
		t.Errorf("computations %d separate, %d shared and %d of a front-end result, want 2, 1 and 0", separate, shared, checked)
	}
}
