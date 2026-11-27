package codepass

import (
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// PassCanonicalize is the name of the pass that applies the local rewrite patterns of a stage until
// none applies (code.ApplyPatterns). It runs at the Flow and at the Exec stage. It is a required step
// with no flag, and its patterns have none: they change no generated parser and no bytecode, only the
// IR between the stages (optimization record 142). Each application of a pattern stays a decision of
// its own, which -opt-bisect-limit numbers and can skip, and -stats counts.
const PassCanonicalize = "canonicalize"

// patternNames returns the names of every pattern (code.Patterns): the patterns of all stages.
func patternNames() []string {
	var names []string
	for _, p := range code.Patterns() {
		names = append(names, p.Name)
	}
	return names
}

// canonicalizeDecl is the declaration of canonicalize, the same at both stages: one name, required,
// and the patterns of every stage, each of which the pass asks about only at a stage that has it. The
// pass imports no dialect: it reads the patterns of its stage, which the stage's dialect declares
// (code.RegisterPatterns), when it runs.
var canonicalizeDecl = pipe.Decl{
	Name:       PassCanonicalize,
	Required:   true,
	Obligation: "Each pattern keeps the behavior of what it rewrites: the same results, the same calls of operations with the same arguments in the same order, and the same way of ending.",
	Patterns:   patternNames(),
}

// canonicalizeAt declares canonicalize at the stage S: it applies the patterns of the stage
// (code.PatternsAt) to each function of the unit, and each application asks the manager (Ctx.Allow).
func canonicalizeAt[S code.Stage](k *pipe.IR[code.At[S]]) *pipe.Pass[code.At[S], code.At[S]] {
	return pipe.New(pipe.Def[code.At[S], code.At[S]]{
		Decl: canonicalizeDecl,
		In:   k, Out: k,
		Run: func(c *pipe.Ctx[code.At[S]], u code.At[S]) (code.At[S], pass.PreservedSet, error) {
			ps, changed := code.PatternsAt[S](), false
			for _, f := range u.Module().Funcs {
				for _, n := range code.ApplyPatterns(f, ps, c.Allow) {
					changed = changed || n > 0
				}
			}
			if !changed {
				return u, pass.PreserveAll(), nil
			}
			return u, pass.PreserveNone(), nil
		},
	})
}

// FlowCanonicalize and ExecCanonicalize are canonicalize at the Flow and at the Exec stage.
var (
	FlowCanonicalize = canonicalizeAt[code.Flow](code.FlowStage)
	ExecCanonicalize = canonicalizeAt[code.Exec](code.ExecStage)
)

// Canonicalize applies every pattern of the Exec stage to f until none applies, outside a pipeline:
// a composition is printed the same whatever optimizations a build turns off.
func Canonicalize(f *code.Func) {
	code.ApplyPatterns(f, code.PatternsAt[code.Exec](), func(string) bool { return true })
}
