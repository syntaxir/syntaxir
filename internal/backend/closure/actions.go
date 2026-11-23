package closure

import (
	"fmt"

	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/runtime/state"
)

// actionSite is an action site of a matcher: a predicate or the value of a binding.
type actionSite struct {
	prog *state.ActionProgram
}

// actionProgram makes the program of the action site i, translated from its function of the Action
// module (vmlower.ActionProgram), which reads the frame of the current scope.
func (b *Builder) actionProgram(i int) (*state.ActionProgram, error) {
	f := b.funcs[i]
	if f == nil {
		return nil, fmt.Errorf("internal error: the Action module has no function of the action site %d", i)
	}
	prog, err := vmlower.ActionProgram(b.actions, f, vmlower.ActionEnv{Checker: b.checker, Vars: b.checker.Vars, Scope: b.scope})
	if err != nil {
		return nil, fmt.Errorf("internal error: %w", err)
	}
	return prog, nil
}
