package desugar

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// walkExpr is astfacts.WalkExprWithPoll polling control; a nil control never polls.
func walkExpr(e ast.Expr, f func(ast.Expr), control *cancel.Control) {
	if control == nil {
		astfacts.WalkExpr(e, f)
		return
	}
	astfacts.WalkExprWithPoll(e, f, control.Poll)
}
