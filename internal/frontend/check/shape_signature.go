package check

import (
	"fmt"
	"strings"

	"syntaxir.dev/sxir/internal/frontend/astfacts"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// ShapeSignature returns a text that describes the elements and captures that every subexpression
// of the rule name contributes to the tuple of its scope, in preorder, leaving out the lookaheads
// and what they hold. It reports false if the model has no such rule or its types were not
// checked. poll, when not nil, is called at every subexpression.
func (p *Model) ShapeSignature(name string, poll func()) (string, bool) {
	if p == nil || p.Typed == nil {
		return "", false
	}
	r := p.ByName[name]
	if r == nil || r.Def == nil {
		return "", false
	}
	step := func() {
		if poll != nil {
			poll()
		}
	}
	// The subexpressions of lookaheads are left out.
	inside := map[ast.Expr]bool{}
	astfacts.WalkExpr(r.Def.Expr, func(e ast.Expr) {
		step()
		var body ast.Expr
		switch x := e.(type) {
		case *ast.And:
			body = x.Expr
		case *ast.Not:
			body = x.Expr
		}
		if body != nil {
			inside[e] = true
			astfacts.WalkExpr(body, func(x ast.Expr) { step(); inside[x] = true })
		}
	})
	var b strings.Builder
	astfacts.WalkExpr(r.Def.Expr, func(e ast.Expr) {
		step()
		if inside[e] {
			return
		}
		fmt.Fprintf(&b, "%T{", e)
		for _, el := range ShapeElems(p.Checker(), e) {
			step()
			if el.Type() != nil {
				b.WriteString(el.Type().String())
			}
			for _, c := range el.Captures() {
				fmt.Fprintf(&b, "<%s %v %v %v>", c.Name, c.Paths, c.Side, c.Maybe)
				if c.T != nil {
					b.WriteString(c.T.String())
				}
			}
			b.WriteByte(';')
		}
		b.WriteString("}")
	})
	return b.String(), true
}
