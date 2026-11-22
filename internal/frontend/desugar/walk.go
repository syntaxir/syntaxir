package desugar

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// walkExpr is astinspect.WalkExprWithPoll polling control; a nil control never polls.
func walkExpr(e ast.Expr, f func(ast.Expr), control *cancel.Control) {
	if control == nil {
		astinspect.WalkExpr(e, f)
		return
	}
	astinspect.WalkExprWithPoll(e, f, control.Poll)
}
