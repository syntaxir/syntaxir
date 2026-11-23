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

// TestClassifyReproducers313 classifies the ten grammars of testdata/classify313, which took 9 s
// to more than 20 s each before the exclusion analysis memoized its rule ends (#313). Each stays
// unknown with its reasons, which the classifier gives without exhausting a budget: the counted
// bound is that no comparison or enumeration of the strict analysis runs out of exclBudget, so the
// result does not depend on the speed of the machine. The context is only a safety net against a
// regression to the old cost, and does not decide the result.
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
	if len(files) != 10 {
		t.Fatalf("got %d reproducers, want 10", len(files))
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
		if k.exclOverBudget != 0 {
			t.Errorf("%s: %d comparisons over exclBudget, want 0", f, k.exclOverBudget)
		}
	}
}
