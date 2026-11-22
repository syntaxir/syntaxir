package check

import (
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Checker returns the checker of the program, made on first use.
func (p *Program) Checker() *Checker {
	p.checkerOnce.Do(func() { p.chk = NewChecker(p.Model) })
	return p.chk
}

// CheckActionExpr is ActionExprChecker.CheckActionExpr over the compiler's errors.
func (c *Compiler) CheckActionExpr(t ast.ActionExpr, s *Scope, locals []string) {
	(&ActionExprChecker{Control: c.Control, Prog: c.Prog.Checker(), Errs: &c.errs}).CheckActionExpr(t, s, locals)
}

// checkerOf returns the checker of p, nil for a nil program.
func checkerOf(p *Program) *Checker {
	if p == nil {
		return nil
	}
	return p.Checker()
}
