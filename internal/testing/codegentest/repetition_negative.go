package codegentest

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// GrammarWithNegativeMaximum parses src and sets the maximum of its repetitions to maximum, which the syntax cannot write.
func GrammarWithNegativeMaximum(t testing.TB, src string, maximum int) *ast.Grammar {
	t.Helper()
	g, err := compiletest.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range g.Rules() {
		astfacts.WalkExpr(r.Expr, func(e ast.Expr) {
			if rep, ok := e.(*ast.Repeat); ok {
				rep.Max = maximum
			}
		})
	}
	return g
}
