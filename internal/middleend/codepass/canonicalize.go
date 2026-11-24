package codepass

import (
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// PassCanonicalize is the name of the pass that applies the local rewrite patterns of a stage until
// none applies (code.ApplyPatterns). It runs at the Flow and at the Exec stage.
const PassCanonicalize = "canonicalize"

// patternNames returns the names of every pattern (code.Patterns): the patterns of all stages.
func patternNames() []string {
	var names []string
	for _, p := range code.Patterns() {
		names = append(names, p.Name)
	}
	return names
}

// The pass and each pattern are optimizations under their names.
func init() {
	flags := []pass.Flag{{Name: PassCanonicalize, Kind: pass.KindCodePass, Default: 1, Doc: "apply the local rewrite patterns of Code IR until none applies"}}
	for _, p := range code.Patterns() {
		flags = append(flags, pass.Flag{Name: p.Name, Kind: pass.KindPattern, Default: 1, Doc: p.Doc})
	}
	pass.Register(flags...)
}

// canonicalizeAt declares canonicalize at the stage S, with the patterns that the stage has (names, the
// patterns of the stage's dialect: a test compares them): it applies the patterns of the stage
// (code.PatternsAt) to each function of the unit, and each application asks the manager (Ctx.Allow). The pass
// declares at each stage the patterns that can match there, so that a pipeline text lists the patterns that
// run. It imports no dialect: it reads the patterns of its stage, which the dialect declares
// (code.RegisterPatterns), when it runs.
func canonicalizeAt[S code.Stage](k *pipe.IR[code.At[S]], names ...string) *pipe.Pass[code.At[S], code.At[S]] {
	return pipe.New(pipe.Def[code.At[S], code.At[S]]{
		Decl: pipe.Decl{
			Name:       PassCanonicalize,
			Obligation: "Each pattern keeps the behavior of what it rewrites: the same results, the same calls of operations with the same arguments in the same order, and the same way of ending.",
			Patterns:   names,
		},
		In: k, Out: k,
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

// FlowCanonicalize and ExecCanonicalize are canonicalize at the Flow and at the Exec stage, with the
// patterns of the stage's dialect (internal/ir/code/flow, internal/ir/code/exec).
var (
	FlowCanonicalize = canonicalizeAt[code.Flow](code.FlowStage, flowPatternNames...)
	ExecCanonicalize = canonicalizeAt[code.Exec](code.ExecStage, execPatternNames...)
)

// The names of the patterns that each stage's canonicalize declares; TestCanonicalizeDeclaresStagePatterns
// compares them with the patterns of the dialects.
var (
	flowPatternNames = []string{code.DropAfterExit.Name}
	execPatternNames = []string{code.FoldConstants.Name, code.FoldBranches.Name, code.DropAfterExit.Name, code.DropUnusedLabels.Name, code.SpliceBlocks.Name}
)

// Canonicalize applies every pattern of the Exec stage to f until none applies, outside a pipeline:
// a composition is printed the same whatever optimizations a build turns off.
func Canonicalize(f *code.Func) {
	code.ApplyPatterns(f, code.PatternsAt[code.Exec](), func(string) bool { return true })
}
