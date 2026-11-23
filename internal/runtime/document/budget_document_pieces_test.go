//go:build !sxir_budget_controls_reference

package document_test

import (
	"errors"
	"strings"
	"testing"

	. "syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/pkg/runtime"
)

func TestBudgetDocumentPieceLiteralRetry(t *testing.T) {
	text := strings.Repeat("a", 8192)
	prog := mustCompile(t, "rule main = "+`"`+text+`" $$`)
	budgetDocumentVariants(t, func(t *testing.T, o ParseOptions) {
		d, err := prog.NewDocumentWith("main", text, o)
		if err != nil {
			t.Fatal(err)
		}
		o.Limits.Compute = 100
		if _, err = d.ParseWith(o); !errors.Is(err, runtime.ErrLimit) {
			t.Fatalf("piece literal bypassed the limit: %v", err)
		}
		budgetDocumentAssertRetry(t, d)
	})
}
