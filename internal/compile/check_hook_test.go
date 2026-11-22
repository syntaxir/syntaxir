package compile_test

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/middleend/checkfacts"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func init() {
	compile.CheckGrammar = func(g *ast.Grammar, opts compile.Options, flags []compile.RuleFlags, names desugar.InlineNames, control *cancel.Control) (*compile.Program, error) {
		return frontend.Check(g, opts, flags, names, checkfacts.Source{}, control)
	}
}
