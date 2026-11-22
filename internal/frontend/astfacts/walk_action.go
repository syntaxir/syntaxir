package astfacts

import "syntaxir.dev/sxir/pkg/grammar/ast"

// WalkAction visits the value expression and its subexpressions in preorder.
func WalkAction(t ast.ActionExpr, f func(ast.ActionExpr)) {
	if t == nil {
		return
	}
	f(t)
	switch t := t.(type) {
	case *ast.ActionGroup:
		WalkAction(t.Action, f)
	case *ast.Block:
		for _, stmt := range t.Statements {
			WalkAction(stmt, f)
		}
	case *ast.FieldAccess:
		WalkAction(t.X, f)
	case *ast.Constructor:
		for _, fi := range t.Fields {
			WalkAction(fi.Value, f)
		}
		for _, x := range t.Args {
			WalkAction(x, f)
		}
	case *ast.TupleLit:
		for _, x := range t.Elems {
			WalkAction(x, f)
		}
	case *ast.ListConstructor:
		for _, item := range t.Items {
			WalkAction(item, f)
		}
	case *ast.Call:
		for _, a := range t.Args {
			WalkAction(a, f)
		}
	case *ast.Lambda:
		WalkAction(t.Body, f)
	case *ast.Binary:
		WalkAction(t.L, f)
		WalkAction(t.R, f)
	case *ast.Unary:
		WalkAction(t.X, f)
	case *ast.Cast:
		WalkAction(t.X, f)
	case *ast.Bind:
		WalkAction(t.Value, f)
	}
}
