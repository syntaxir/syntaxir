//go:build !sxir_budget_controls_reference

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// Alternate inactive and active invocations after a resumable run has been
// cached, so graph selection must preserve repetition-site identities and
// control admission even when the Document reuses an earlier result.
func TestBudgetDocumentAlternatesDispatchGraphs(t *testing.T) {
	t.Parallel()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, source := range []string{
		`rule main = item+ $$

rule item = "a"`,
		`rule main = expr $$

rule expr = #operators {
    operand "a"
    level {
        infix left "+"
    }
}`,
	} {
		prog := mustCompile(t, source)
		text := strings.Repeat("a", 80)
		if strings.Contains(source, "#operators {") {
			text = strings.TrimSuffix(strings.Repeat("a+", 40), "+")
		}
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			d, err := prog.NewDocumentWith("main", text, ParseOptions{Unit: unit})
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 6; i++ {
				opts := ParseOptions{Unit: unit}
				if i%2 == 1 {
					opts.Limits = runtime.Limits{Compute: 1000000}
				}
				got, err := d.ParseWith(opts)
				if err != nil {
					t.Fatal(err)
				}
				fresh, err := prog.ParseWith("main", d.Text(), opts)
				if err != nil || codegentest.ResultJSON(got, nil) != codegentest.ResultJSON(fresh, err) {
					t.Fatalf("iteration %d: cached/fresh mismatch: %v", i, err)
				}
				// A cached tree must not bypass admission on the other dispatch graph: a cancelled
				// context stops it. A compute limit of one unit need not, as a parse that reuses the whole
				// result counts only the entry into its start rule.
				blocked, err := d.ParseWith(ParseOptions{Unit: unit, Context: cancelled})
				if blocked != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("iteration %d: cached result bypassed admission: %v", i, err)
				}
				if err := d.Edit(0, 1, "a"); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

// Rule calls consume budget even when they match no input, and the zero-width action expressions of one rule
// consume none beyond the call: between two charges only the straight-line code of a rule body
// runs, whose length the grammar bounds. Recognition discards CST construction so input and value
// spend cannot mask a missing call charge on the controlled graph.
func TestBudgetClosureDispatchMetersZeroWidthExpressions(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, "rule a = ^\n"+"rule main = "+strings.Repeat("a ", 64)+"$$")
	straight := mustCompile(t, "rule main = "+strings.Repeat("^ ", 64)+"$$")
	for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
		opts := ParseOptions{Unit: unit, Recognize: true, Limits: runtime.Limits{Compute: 16}}
		n, err := prog.ParseWith("main", "", opts)
		if n != nil || !errors.Is(err, runtime.ErrLimit) {
			t.Fatalf("zero-width calls bypassed budget: node=%v error=%v", n, err)
		}
		if _, err := straight.ParseWith("main", "", opts); err != nil {
			t.Fatalf("straight-line zero-width action expressions were charged: %v", err)
		}
		opts.Limits = runtime.Limits{}
		if _, err := prog.ParseWith("main", "", opts); err != nil {
			t.Fatal(err)
		}
	}
}
