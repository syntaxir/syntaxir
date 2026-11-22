package closure

import (
	"fmt"

	"syntaxir.dev/sxir/internal/backend/actionlower"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/runtime/state"
)

// actionSite is an action expression of a matcher: a predicate or the value of a binding.
type actionSite struct {
	actionExpr *state.ActionProgram
}

// action expression makes the program of the Core IR action expression t, which reads the frame of the current scope; proj
// are the projected captures of the rule whose action t is, nil for every other action expression.
func (b *Builder) actionExpr(t ir.ActionExpr, proj map[string]string) (*state.ActionProgram, error) {
	prog, err := actionlower.Compile(t, actionlower.Env{Checker: b.checker, Vars: b.checker.Vars, Scope: b.scope, Proj: proj})
	if err != nil {
		return nil, fmt.Errorf("internal error: %w", err)
	}
	return prog, nil
}
