package check

import (
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// ResolveCheck is check.Checker.ResolveCheck.
func (prog *Program) ResolveCheck(t ast.TypeExpr) types.Ty { return prog.Checker().ResolveCheck(t) }
