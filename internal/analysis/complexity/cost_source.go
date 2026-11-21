package complexity

import (
	"slices"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// CostSource is what the cost view of a grammar is built from: the rules that the complexity
// classifier reads, their untyped Core IR module with its origin map, which gives the expression
// each node of the module is built from, and the facts of the module. Rule i of Rules is rule i of Module.
type CostSource struct {
	// Grammar is the grammar with inline actions lowered and attributes normalized.
	Grammar *ast.Grammar
	// Rules are the unique rules of Grammar.
	Rules  []*ast.RuleDef
	Module *core.Module
	Facts  *corepass.Facts
}

// NewCostSource returns the cost source of g, which must compile, as the front end checks it.
func NewCostSource(g *ast.Grammar, control *cancel.Control) (*CostSource, error) {
	in, err := checkedInput(g, control)
	if err != nil {
		return nil, err
	}
	st := in.structural
	return &CostSource{Grammar: st.Canonical.Grammar, Rules: st.Rules, Module: st.Module, Facts: corepass.FactsOf(st.Analyses, st.Module).WithControl(control)}, nil
}

// NormalView returns the grammar that the compiler runs for g, with inline actions lowered and
// attributes normalized, and with the bodies of its rules replaced by those of their view with
// opts (Build), and the view. The zero Options give the view that the complexity classifier reads;
// with Labels and Inline the view also drops labels and inlines the calls that P1 allows, and keeps
// only what a parse matches and where it ends (≡E), which is what an analysis of matching, such as
// the exclusion of alternatives, needs. g must compile.
func NormalView(g *ast.Grammar, opts Options, control *cancel.Control) (*ast.Grammar, *View, error) {
	cs, err := NewCostSource(g, control)
	if err != nil {
		return nil, nil, err
	}
	v := Build(&Source{Module: cs.Module, Facts: cs.Facts}, opts, control)
	rules, err := v.Grammar(cs.Rules)
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]*ast.RuleDef{}
	for _, r := range rules {
		byName[r.Name] = r
	}
	out := *cs.Grammar
	out.File.Decls = slices.Clone(cs.Grammar.File.Decls)
	for i, d := range out.File.Decls {
		if r, ok := d.(*ast.RuleDef); ok {
			out.File.Decls[i] = byName[r.Name]
		}
	}
	return &out, v, nil
}
