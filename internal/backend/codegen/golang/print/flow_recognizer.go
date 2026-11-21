package print

import (
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
)

// Value-free rules of the Node library printed from Flow IR.
//
// The rules of a recognizer (recRules) and the lean rules of the Node table (rules) that the table's
// plan lowers as flat functions (flowplan.Plan) are printed by flowPrinter for the untyped Node
// library. The other rules print their framed body from Flow IR as well (flowNodeFrameBody), in the
// same output. Both tables share the helpers whose printed text is equal. The text passes run on the
// whole output afterwards.

// flowRecRule prints the flat function spec of the rule r as a flat rule, from the module of the
// table's plan, which runs it flat (flowplan.Unit). It returns nil, with the internal error of
// execRecRule, if the printed form breaks the plan's decision.
func (g *Generator) flowRecRule(r *common.Rule, spec *plandata.FuncSpec) *flatRule {
	l := g.planned(spec.Key)
	g.Cur, g.Proj = r, g.Projections(r)
	defer func() { g.Proj = nil }()
	p := newFlowPrinter(g, false)
	p.document = prim.AttrsOf(l.Func).Document
	return g.execRecRule(p, l)
}

// untypedGoType returns the Go type of a variable of a Flow IR function printed for the Node
// library, or false if the printer has no such type for it.
func untypedGoType(t prim.Type) (string, bool) {
	switch t {
	case prim.TypeInt, prim.TypeBool, prim.TypeRune, prim.TypeMark, prim.TypeExpMark, prim.TypeExpIDs:
		return goType(t), true
	}
	return "", false
}
