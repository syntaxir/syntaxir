package check

import (
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Checker returns the checker of the model, made on first use. A model has this one checker,
// whose caches live as long as the model.
func (m *Model) Checker() *Checker {
	m.checkerOnce.Do(func() { m.chk = NewChecker(m) })
	return m.chk
}

// CheckActionExpr is ActionExprChecker.CheckActionExpr over the compiler's errors.
func (c *Compiler) CheckActionExpr(t ast.ActionExpr, s *Scope, locals []string) {
	(&ActionExprChecker{Control: c.Control, Checker: c.Model.Checker(), Errs: &c.errs}).CheckActionExpr(t, s, locals)
}

// checkerOf returns the checker of p, nil for a nil model.
func checkerOf(p *Model) *Checker {
	if p == nil {
		return nil
	}
	return p.Checker()
}
