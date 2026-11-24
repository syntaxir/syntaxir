package codegentest

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// GrammarWithNegativeMaximum parses src and sets the maximum of its repetitions to maximum. The
// syntax writes no negative maximum but -1, and validation rejects the others
// (WantNegativeMaximumRejected).
func GrammarWithNegativeMaximum(t testing.TB, src string, maximum int) *ast.Grammar {
	t.Helper()
	g, err := compiletest.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range g.Rules() {
		astinspect.WalkExpr(r.Expr, func(e ast.Expr) {
			if rep, ok := e.(*ast.Repeat); ok {
				rep.Max = maximum
			}
		})
	}
	return g
}

// WantNegativeMaximumRejected fails t unless err is the validation error of a repetition maximum
// below -1.
func WantNegativeMaximumRejected(t testing.TB, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "repetition maximum must be -1 (unbounded) or nonnegative") {
		t.Fatalf("a repetition maximum below -1 was not rejected by validation: %v", err)
	}
}
