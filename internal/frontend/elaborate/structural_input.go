package elaborate

import (
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// StructuralInput returns the input of the untyped Core IR module (Structural) of rules,
// the rules of g once each, as UniqueRules gives them: rule i has ID i. starts names the start
// rules in order; a name that is not a rule is left out. The variables are those g declares,
// by name only.
func StructuralInput(g *ast.Grammar, rules []*ast.RuleDef, starts []string) *Input {
	in := &Input{ByName: map[string]*Rule{}}
	for i, rd := range rules {
		_, operators := rd.Expr.(*ast.OperatorsRule)
		r := &Rule{ID: i, Name: rd.Name, Def: rd, Body: rd.Expr, Action: rd.Action,
			Splice: desugar.HiddenRule(rd.Name) && rd.Action == nil && !rd.DefaultAction && !operators}
		in.Rules = append(in.Rules, r)
		in.ByName[rd.Name] = r
	}
	for _, name := range starts {
		if r := in.ByName[name]; r != nil {
			in.Starts = append(in.Starts, r)
		}
	}
	for _, d := range g.File.Decls {
		if vd, ok := d.(*ast.VarDef); ok {
			if in.Vars == nil {
				in.Vars = check.NewVarTable()
			}
			if in.Vars.Lookup(vd.Name) == nil {
				in.Vars.Add(&check.VarDecl{Name: vd.Name})
			}
		}
	}
	return in
}
