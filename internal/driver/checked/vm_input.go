package checked

import (
	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// VMInput returns what the bytecode lowering reads of the checked program p (vmlower.Input).
//
// Migration only. Delete it once the front end checking result builds vmlower.Input directly
// (step B of #280); until then it is the one adapter from the compiler program to the lowering
// input, and the lowering needs no other part of the program.
func VMInput(p *compile.Program) vmlower.Input {
	in := vmlower.Input{
		Elab:              p,
		Vars:              p.Vars,
		Zeros:             map[string]vmlower.Zero{},
		StartRules:        p.StartRules,
		DefaultStartRule:  p.DefaultStartRule,
		RecordsSourceInfo: p.RecordsSourceInfo,
		MakesDefaultValue: p.MakesDefaultValue,
		RunKeys:           map[int]vmlower.RunKey{},
	}
	for _, td := range p.Grammar.Types() {
		spec, ok := td.(*ast.TypeDef)
		if !ok {
			continue
		}
		in.Types = append(in.Types, spec)
		if z := p.StructZeroOf(spec.DeclName()); z != nil {
			in.Zeros[spec.DeclName()] = vmlower.Zero{Names: z.Names, Zeros: z.Zeros}
		}
	}
	in.Rules = rulesOf(p.RuleList)
	in.Twins = rulesOf(p.Twins)
	for site, k := range p.RunKeys() {
		in.RunKeys[site] = vmlower.RunKey{Vars: k.Vars, Bind: k.Bind}
	}
	return in
}

// rulesOf converts the rules of a compiled program to the rule table entries of the lowering.
func rulesOf(rs []*compile.Rule) []vmlower.Rule {
	out := make([]vmlower.Rule, len(rs))
	for i, r := range rs {
		out[i] = vmlower.Rule{Name: r.Name, Where: r.Where, NodeType: r.NodeType, OpNodeType: r.OpNodeType,
			Vars: r.Vars, Memo: r.Memo, Leader: r.Leader, Member: r.Member, Positional: r.Positional,
			Splice: r.Splice, Transient: r.Transient, Rec: r.Rec, NoValue: r.Novalue, Cycle: r.Cycle}
	}
	return out
}
