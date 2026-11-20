package compile

import (
	"zeroparse.dev/zp/internal/cancel"
	"zeroparse.dev/zp/internal/frontend/astfacts"
	"zeroparse.dev/zp/pkg/grammar/ast"
)

// The walks of the grammar's expressions and terms that the checks of the compiler use. The rule
// facts are not computed here: they are those of the Core IR module of the rules (RuleFacts).

// WalkTerm is astfacts.WalkTerm. It stays for the tests and test support that call it through
// compile; new code calls astfacts.
func WalkTerm(t ast.ActionExpr, f func(ast.ActionExpr)) { astfacts.WalkTerm(t, f) }

// WalkExpr is astfacts.WalkExpr; see WalkTerm.
func WalkExpr(e ast.Expr, f func(ast.Expr)) { astfacts.WalkExpr(e, f) }

// valueRecovery reports whether e has a #recover outside lookaheads and Operators Rule skips, and
// returns the rules that it calls outside them (noRecoveringCaptures).
func valueRecovery(e ast.Expr, control *cancel.Control) (bool, []string) {
	silent := map[ast.Expr]bool{}
	mark := func(x ast.Expr) {
		astfacts.WalkExprControlled(x, func(y ast.Expr) { silent[y] = true }, control)
	}
	astfacts.WalkExprControlled(e, func(x ast.Expr) {
		switch x := x.(type) {
		case *ast.And:
			mark(x.Expr)
		case *ast.Not:
			mark(x.Expr)
		case *ast.OperatorsRule:
			mark(x.Skip)
		}
	}, control)
	found := false
	var calls []string
	astfacts.WalkExprControlled(e, func(x ast.Expr) {
		if silent[x] {
			return
		}
		switch x := x.(type) {
		case *ast.Ref:
			calls = append(calls, x.Name)
		case *ast.Recovery:
			found = true
		case *ast.Attributed:
			found = found || hasRecover(x.Attrs)
		}
	}, control)
	return found, calls
}

// hasRecover reports whether attrs has #recover.
func hasRecover(attrs []*ast.Attribute) bool {
	for _, a := range attrs {
		if a.Name == "recover" {
			return true
		}
	}
	return false
}
