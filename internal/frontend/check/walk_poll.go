package check

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// walkExprControlled is astinspect.WalkExprWithPoll polling control.
func walkExprControlled(e ast.Expr, f func(ast.Expr), control *cancel.Control) {
	if control == nil {
		astinspect.WalkExpr(e, f)
		return
	}
	astinspect.WalkExprWithPoll(e, f, control.Poll)
}

// expressionArgs returns the expression arguments of the attribute, polling control.
func expressionArgs(control *cancel.Control, a *ast.Attribute) []ast.Expr {
	if control == nil {
		return a.ExpressionArgs()
	}
	return astinspect.ExpressionArgsWithPoll(a, control.Poll)
}
