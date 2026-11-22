package codepass

import (
	"syntaxir.dev/sxir/internal/ir/code"
	// The dialects of the stages declare the patterns of their stage (code.RegisterPatterns), which
	// the pass reads when it is declared.
	_ "syntaxir.dev/sxir/internal/ir/code/exec"
	_ "syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// PassCanonicalize is the name of the pass that applies the local rewrite patterns of a stage until
// none applies (code.ApplyPatterns). It runs at the Flow and at the Exec stage.
const PassCanonicalize = "canonicalize"

// patternNames returns the names of the patterns of both stages, the Exec stage's first, each once.
func patternNames() []string {
	var names []string
	seen := map[string]bool{}
	for _, ps := range [][]code.Pattern{code.PatternsAt[code.Exec](), code.PatternsAt[code.Flow]()} {
		for _, p := range ps {
			if !seen[p.Name] {
				seen[p.Name] = true
				names = append(names, p.Name)
			}
		}
	}
	return names
}

// The pass and each pattern are optimizations under their names.
func init() {
	flags := []pass.Flag{{Name: PassCanonicalize, Kind: pass.KindCodePass, Default: 1, Doc: "apply the local rewrite patterns of Code IR until none applies"}}
	docs := map[string]string{}
	for _, ps := range [][]code.Pattern{code.PatternsAt[code.Exec](), code.PatternsAt[code.Flow]()} {
		for _, p := range ps {
			docs[p.Name] = p.Doc
		}
	}
	for _, name := range patternNames() {
		flags = append(flags, pass.Flag{Name: name, Kind: pass.KindPattern, Default: 1, Doc: docs[name]})
	}
	pass.Register(flags...)
}

// canonicalizeDecl is the declaration of canonicalize, the same at both stages: one name, one flag,
// and the patterns of both stages, each of which the pass asks about only at a stage that has it.
var canonicalizeDecl = pipe.Decl{
	Name:       PassCanonicalize,
	Obligation: "Each pattern keeps the behavior of what it rewrites: the same results, the same calls of operations with the same arguments in the same order, and the same way of ending.",
	Patterns:   patternNames(),
}

// canonicalizeAt declares canonicalize at the stage S: it applies the patterns of the stage
// (code.PatternsAt) to each function of the unit, and each application asks the manager (Ctx.Allow).
func canonicalizeAt[S code.Stage](k *pipe.IR[code.At[S]]) *pipe.Pass[code.At[S], code.At[S]] {
	ps := code.PatternsAt[S]()
	return pipe.New(pipe.Def[code.At[S], code.At[S]]{
		Decl: canonicalizeDecl,
		In:   k, Out: k,
		Run: func(c *pipe.Ctx[code.At[S]], u code.At[S]) (code.At[S], pass.PreservedSet, error) {
			changed := false
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
