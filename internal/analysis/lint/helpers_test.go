package lint

import (
	"context"
	"slices"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// checked runs the front end on g without cancellation. Many test grammars are one rule without
// @start; the front end checks a copy whose first rule starts. The copy shares the expressions of g,
// so the grammar that the front end checked keeps the positions of g, as RunChecked requires.
func checked(g *ast.Grammar) *frontend.Result {
	res, err := frontend.Run(g, compile.Options{}, nil)
	if err == nil {
		return res
	}
	if copied := firstRuleStarts(g); copied != nil {
		if res, err2 := frontend.Run(copied, compile.Options{}, nil); err2 == nil {
			return res
		}
	}
	panic(err)
}

// firstRuleStarts returns a copy of g whose first rule is marked @start, or nil if g has a start
// rule or no rule. g is not changed.
func firstRuleStarts(g *ast.Grammar) *ast.Grammar {
	copied := *g
	copied.File.Decls = slices.Clone(g.File.Decls)
	for i, d := range copied.File.Decls {
		rd, ok := d.(*ast.RuleDef)
		if !ok {
			continue
		}
		if slices.ContainsFunc(rd.Attrs, func(a *ast.Attribute) bool { return a.Name == "start" }) {
			return nil
		}
		first := *rd
		first.Attrs = append([]*ast.Attribute{{Name: "start"}}, rd.Attrs...)
		copied.File.Decls[i] = &first
		for _, d := range copied.File.Decls[i+1:] {
			if rd, ok := d.(*ast.RuleDef); ok && slices.ContainsFunc(rd.Attrs, func(a *ast.Attribute) bool { return a.Name == "start" }) {
				return nil
			}
		}
		return &copied
	}
	return nil
}

// runUnchecked lints g from start without a cancellation control.
func runUnchecked(g *ast.Grammar, start string, opts options) []Finding {
	return run(checked(g), g, start, opts, nil)
}

// testRunContext lints g from start, with cancellation by ctx for the lint and not the compilation.
func testRunContext(ctx context.Context, g *ast.Grammar, start string, opts options) ([]Finding, error) {
	return runContext(ctx, checked(g), g, start, opts)
}

// newAnalysis returns the analysis of the grammar that the front end checked g to: its expressions
// are those of the analysis's grammar (analysis.g), not of g.
func newAnalysis(g *ast.Grammar) *analysis { return testNewAnalysisControlled(g, nil) }

func testNewAnalysisControlled(g *ast.Grammar, control *lintControl) *analysis {
	return newAnalysisControlled(checked(g), control)
}

// withControl returns a copy of a whose checks and fact queries poll control.
func (a *analysis) withControl(control *lintControl) *analysis {
	copied := *a
	copied.control = control
	copied.facts = a.facts.WithControl(&cancel.Control{Ctx: control.ctx})
	return &copied
}
