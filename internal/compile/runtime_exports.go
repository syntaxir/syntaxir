package compile

import (
	"zeroparse.dev/zp/grammar"
	"zeroparse.dev/zp/internal/frontend/check"
)

// Scope is check.Scope. It exists only for the runtime side and leaves when the runtime tables
// move to internal/runtime/program.
type Scope = check.Scope

// NewScope is check.NewScope. It exists only for the runtime side and leaves when the runtime
// tables move to internal/runtime/program.
func NewScope() *Scope { return check.NewScope() }

// NewProgram returns a program of the grammar g with no rules or types yet; g is nil for a
// program loaded from a compiled grammar. It exists only for the runtime side, which builds such
// a program without naming the semantic model, and leaves when the runtime tables move to
// internal/runtime/program.
func NewProgram(g *grammar.Grammar) *Program {
	return &Program{Model: check.NewModel(g, nil), ByName: map[string]*Rule{}}
}
