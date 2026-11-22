package state_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/closure"
	"syntaxir.dev/sxir/internal/frontend"
	irbuild "syntaxir.dev/sxir/internal/frontend/elaborate"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// closureExprMatcher returns the closure that the closure backend builds for the expression e of
// the program prog, from the program's Core IR after the closure pipeline. The expression must be
// one that a node of Core IR is built from.
func closureExprMatcher(t *testing.T, prog *frontend.Result, e ast.Expr) state.Matcher {
	t.Helper()
	m, found := closureExprCore(t, prog, e)
	f, err := closure.Matcher(prog.Runtime(), prog.Checker(), m, found, nil)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// closureExprNode returns the Core IR node, after the passes, that closureExprMatcher builds for e.
func closureExprNode(t *testing.T, prog *frontend.Result, e ast.Expr) ir.Node {
	t.Helper()
	_, n := closureExprCore(t, prog, e)
	return n
}

func closureExprCore(t *testing.T, prog *frontend.Result, e ast.Expr) (*ir.Module, ir.Node) {
	t.Helper()
	m, err := irbuild.CoreFrom(prog, true)
	if err != nil {
		t.Fatal(err)
	}
	origin := m.Origin
	ir.Analyze(m)
	if m, _, err = corepass.Run(m, corepass.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	var found ir.Node
	for _, r := range m.Rules {
		ir.Walk(r.Body, func(n ir.Node) {
			if found == nil && origin.Of(n) == e {
				found = n
			}
		})
	}
	if found == nil {
		t.Fatal("no Core IR node of the expression")
	}
	return m, found
}
