package print

import (
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
)

// Value-free rules of the Node library printed from Exec IR.
//
// The rules of a recognizer (recRules) and the lean rules of the Node table (rules) that the table's
// plan lowers as flat functions (flowplan.Plan) are printed by primPrinter for the untyped Node
// library. The other rules print their framed body from Exec IR as well (nodeFrameBody), in the
// same output. Both tables share the helpers whose printed text is equal. The text passes run on the
// whole output afterwards.

// recRuleFunc prints the flat function spec of the rule r as a flat rule, from the module of the
// table's plan, which runs it flat (flowplan.Unit). It returns nil, with the internal error of
// execRecRule, if the printed form breaks the plan's decision.
func (g *Generator) recRuleFunc(r *common.Rule, spec *plandata.FuncSpec) *flatRule {
	l := g.planned(spec.Key)
	g.Cur = r
	return g.execRecRule(newPrimPrinter(g, false, l), l)
}

// untypedGoType returns the Go type of a variable of an Exec IR function printed for the Node
// library, or false if the printer has no such type for it.
func untypedGoType(t prim.Type) (string, bool) {
	switch t {
	case prim.TypeInt, prim.TypeBool, prim.TypeRune, prim.TypeMark, prim.TypeExpMark, prim.TypeExpIDs:
		return goType(t), true
	}
	return "", false
}
