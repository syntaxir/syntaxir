package astinspect

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The walks and syntactic questions about the expressions and action expressions of a rule that the compiler and
// the analyses share. The rule facts are not computed here: they are those of the Core IR module of
// the rules.

// AllCalls returns every rule called in the expression.
func AllCalls(e ast.Expr, acc []string) []string {
	return AllCallsControlled(e, acc, nil)
}

// AllCallsControlled is AllCalls polling control for cancellation.
func AllCallsControlled(e ast.Expr, acc []string, control *cancel.Control) []string {
	WalkExprControlled(e, func(x ast.Expr) {
		control.Poll()
		if r, ok := x.(*ast.Ref); ok {
			acc = append(acc, r.Name)
		}
	}, control)
	return acc
}

// WalkExprControlled is WalkExprWithPoll polling control; a nil control polls nothing.
func WalkExprControlled(e ast.Expr, f func(ast.Expr), control *cancel.Control) {
	if control == nil {
		WalkExpr(e, f)
		return
	}
	WalkExprWithPoll(e, f, control.Poll)
}

// WalkRuleActions visits every action expression in a rule's action and predicates, including Operators Rule
// operand and operator actions.
func WalkRuleActions(r *ast.RuleDef, f func(ast.ActionExpr)) { WalkRuleActionsControlled(r, f, nil) }

// WalkRuleActionsControlled is WalkRuleActions polling control for cancellation.
func WalkRuleActionsControlled(r *ast.RuleDef, f func(ast.ActionExpr), control *cancel.Control) {
	control.Poll()
	WalkAction(r.Action, f)
	WalkExprControlled(r.Expr, func(e ast.Expr) {
		switch e := e.(type) {
		case *ast.Predicate:
			WalkAction(e.Action, f)
		case *ast.OperatorsRule:
			for _, o := range e.Operands {
				control.Poll()
				WalkAction(o.Action, f)
			}
			for _, l := range e.Levels {
				control.Poll()
				for _, op := range l.Operators {
					control.Poll()
					WalkAction(op.Action, f)
				}
			}
		}
	}, control)
}

// HasPragma reports whether e carries the pragma or attribute name.
func HasPragma(e *ast.Attributed, name string) bool {
	for _, a := range e.Attrs {
		if a.Name == name {
			return true
		}
	}
	return false
}

// RecoverSkip returns the skip expression of a #recover on e, if it has one.
func RecoverSkip(e *ast.Attributed) ast.Expr {
	for _, a := range e.Attrs {
		if a.Name == "recover" {
			if skip, ok := a.Arg("skip"); ok {
				return skip
			}
		}
	}
	return nil
}

// SpliceRule reports whether the rule rd makes no node of its own: it is hidden and has no action
// (not even => ...), and its body is not an Operators Rule. A call of it contributes the elements
// of its body, as a group does.
func SpliceRule(rd *ast.RuleDef) bool {
	if !program.HiddenRule(rd.Name) || rd.Action != nil || rd.DefaultAction {
		return false
	}
	_, operators := rd.Expr.(*ast.OperatorsRule)
	return !operators
}
