package check_test

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/middleend/checkfacts"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func init() {
	check.CheckGrammar = func(g *ast.Grammar, opts check.Options, flags []check.RuleFlags, names desugar.InlineNames, control *cancel.Control) (*check.Program, error) {
		r, err := frontend.Check(g, opts, flags, names, checkfacts.Source{}, control)
		if err != nil {
			return nil, err
		}
		return r.Program, nil
	}
}
