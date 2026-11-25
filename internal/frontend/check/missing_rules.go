package check

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend/astinspect"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// markMissingRules records transitive insertion dependencies before matchers or
// value-free twins are compiled. Unrelated rules retain completed memo reuse.
func (m *Model) markMissingRules(control *cancel.Control) {
	var usesMissingType func(ast.TypeExpr) bool
	usesMissingType = func(t ast.TypeExpr) bool {
		control.Poll()
		switch t := t.(type) {
		case *ast.TypeRef:
			return t.Name == valueimpl.TypeMissing
		case *ast.ListType:
			return usesMissingType(t.Elem)
		case *ast.OptionalType:
			return usesMissingType(t.Elem)
		case *ast.TypeGroup:
			return usesMissingType(t.Type)
		case *ast.JoinType:
			for _, t := range t.Types {
				if usesMissingType(t) {
					return true
				}
			}
		case *ast.MeetType:
			for _, t := range t.Types {
				if usesMissingType(t) {
					return true
				}
			}
		case *ast.TupleType:
			for _, t := range t.Types {
				if usesMissingType(t) {
					return true
				}
			}
		case *ast.TypeApp:
			for _, t := range t.Args {
				if usesMissingType(t) {
					return true
				}
			}
		case *ast.StructType:
			for _, f := range t.Fields {
				if usesMissingType(f.Type) {
					return true
				}
			}
		}
		return false
	}
	for _, td := range control.Types(m.Grammar) {
		switch t := td.(type) {
		case *ast.TypeDef:
			for _, f := range DefFields(t) {
				if usesMissingType(f.Type) {
					m.MissingType = true
				}
			}
		case *ast.TypeAlias:
			if usesMissingType(t.Type) {
				m.MissingType = true
			}
		}
	}
	refs := make(map[*Rule][]string)
	for _, r := range m.RuleList {
		if usesMissingType(r.Def.Type) {
			m.MissingType = true
		}
		astinspect.WalkExprControlled(r.Def.Expr, func(e ast.Expr) {
			control.Poll()
			switch e := e.(type) {
			case *ast.Attributed:
				for _, a := range e.Attrs {
					if a.Name == "missing" {
						r.Missing = true
						m.HasMissing, m.MissingType = true, true
					}
				}
			case *ast.Ref:
				refs[r] = append(refs[r], e.Name)
			}
		}, control)
	}
	for changed := true; changed; {
		changed = false
		for _, r := range m.RuleList {
			control.Poll()
			if r.Missing {
				continue
			}
			for _, name := range refs[r] {
				control.Poll()
				if callee := m.ByName[name]; callee != nil && callee.Missing {
					r.Missing = true
					changed = true
					break
				}
			}
		}
	}
}
