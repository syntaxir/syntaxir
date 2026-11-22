package compile

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// CheckGrammar checks a grammar as internal/frontend.Check does, with the rule facts and the sites
// that internal/middleend/checkfacts computes on Core IR. Both import this package, so an external
// test of the package sets it (check_hook_test.go) for the internal tests.
var CheckGrammar func(g *ast.Grammar, opts check.Options, flags []check.RuleFlags, names desugar.InlineNames, control *cancel.Control) (*Program, error)

// Check is CheckGrammar.
func Check(g *ast.Grammar, opts check.Options, flags []check.RuleFlags, names desugar.InlineNames, control *cancel.Control) (*Program, error) {
	return CheckGrammar(g, opts, flags, names, control)
}
