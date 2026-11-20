package compile

import (
	"zeroparse.dev/zp/internal/frontend/elaborate"
	"zeroparse.dev/zp/pkg/grammar/ast"
)

// StructuralInput is elaborate.StructuralInput. It stays for the tests that call it through
// compile; new code calls elaborate.
func StructuralInput(g *ast.Grammar, rules []*ast.RuleDef, starts []string) *elaborate.Input {
	return elaborate.StructuralInput(g, rules, starts)
}
