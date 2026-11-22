package compile

import (
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Checker returns the checker of the program, made on first use.
func (p *Program) Checker() *check.Checker {
	p.checkerOnce.Do(func() { p.chk = check.NewChecker(p.Model) })
	return p.chk
}

// CheckActionExpr is check.ActionExprChecker.CheckActionExpr over the compiler's errors.
func (c *Compiler) CheckActionExpr(t ast.ActionExpr, s *check.Scope, locals []string) {
	(&check.ActionExprChecker{Control: c.Control, Prog: c.Prog.Checker(), Errs: &c.errs}).CheckActionExpr(t, s, locals)
}

// checkerOf returns the checker of p, nil for a nil program.
func checkerOf(p *Program) *check.Checker {
	if p == nil {
		return nil
	}
	return p.Checker()
}
