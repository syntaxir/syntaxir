package complexity

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/starttest"
)

// reproducerRuleEndBound is the most computations of rule ends (ruleEndRuns) that each grammar of
// testdata/classify313 may take with the memo on, about twice the count measured at the memoized
// analysis. With the memo off the same grammars take 21 000 to 53 000 computations and exhaust
// exclBudget, so each bound is exceeded by more than an order of magnitude without the memo.
var reproducerRuleEndBound = map[string]int{
	"r00.sxir": 620, "r01.sxir": 284, "r02.sxir": 288, "r03.sxir": 236,
	"r04.sxir": 476, "r05.sxir": 434, "r06.sxir": 592, "r07.sxir": 210,
	"r08.sxir": 380, "r09.sxir": 1764, "r10.sxir": 160,
}

// TestClassifyReproducers313 classifies the grammars of testdata/classify313: the issue's grammar
// (r00) and the ten random ones, which took 9 s to more than 20 s each before the exclusion
// analysis memoized its rule ends (#313). Each stays unknown with its reasons. The counted checks
// are that the computations of rule ends stay within reproducerRuleEndBound and that no step of
// the strict analysis exhausts exclBudget. The 60 s context is only a safety net, and does not
// decide the result.
//
// The grammars are the first ten of 2400 random left-recursive bodies of rule l, drawn from
// seed 313 by testdata/classify313/generate.py (python3 generate.py 313 2400 <dir>), each
// classified at 17bbe9c66 with the original analysis; the ten with the longest run times are
// r01 to r10 (cand c0137, c2339, c0578, c2387, c0587, c1862, c1966, c2363, c1146, c1313).
func TestClassifyReproducers313(t *testing.T) {
	files, err := filepath.Glob("testdata/classify313/r*.sxir")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(reproducerRuleEndBound) {
		t.Fatalf("got %d reproducers, want %d", len(files), len(reproducerRuleEndBound))
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		g, err := parser.Parse(string(src))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancelCtx := context.WithTimeout(context.Background(), 60*time.Second)
		control := &cancel.Control{Ctx: ctx}
		in, err := checkedInput(starttest.MarkAll(g), control)
		if err != nil {
			t.Fatal(err)
		}
		k, err := in.classifier("", control, newViewClassifier)
		if err != nil {
			t.Fatal(err)
		}
		c := k.classify().written(in.names)
		cancelCtx()
		if c.Class != ClassUnknown {
			t.Errorf("%s: class %v, want unknown", f, c.Class)
		}
		if len(c.Reasons) == 0 {
			t.Errorf("%s: no reasons for the unknown class", f)
		}
		bound, ok := reproducerRuleEndBound[filepath.Base(f)]
		if !ok {
			t.Fatalf("%s: no rule-end bound", f)
		}
		if k.ruleEndRuns > bound {
			t.Errorf("%s: %d rule-end computations, want at most %d", f, k.ruleEndRuns, bound)
		}
		if k.exclOverBudget != 0 {
			t.Errorf("%s: %d comparisons over exclBudget, want 0", f, k.exclOverBudget)
		}
	}
}
