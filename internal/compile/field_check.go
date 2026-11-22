package compile

import (
	"syntaxir.dev/sxir/internal/frontend/types"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// ResolveCheck is check.Checker.ResolveCheck.
func (prog *Program) ResolveCheck(t ast.TypeExpr) types.Ty { return prog.Checker().ResolveCheck(t) }

// CastPos returns the grammar position of the cast t as line and column, or 0, 0 when the program
// records no source information or the position is unknown.
func (prog *Program) CastPos(t *ast.Cast) (int, int) {
	if !prog.RecordsSourceInfo {
		return 0, 0
	}
	if pos := t.Diagnostic(); pos.IsValid() {
		return pos.Line, pos.Col
	}
	return 0, 0
}
