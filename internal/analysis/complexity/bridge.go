package complexity

import (
	"fmt"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The classifier walks the rules it classifies as grammar expressions, and reads the facts of
// their Core IR nodes: each expression of the rules stands for the node of the view, or of the
// module, that it is written from (classifier.nodes), and each expression that the classifier
// builds itself, such as the repetition that stands for the later iterations of another, stands
// for a node built with it.

// originNodes returns, for each grammar expression that a node of m is built from, the outermost
// such node (core.Module.Origin).
func originNodes(m *core.Module) map[ast.Expr]core.Node {
	nodes := map[ast.Expr]core.Node{}
	for _, r := range m.Rules {
		if r == nil {
			continue
		}
		core.Walk(r.Body, func(n core.Node) {
			if e := m.Origin.Of(n); e != nil {
				if _, seen := nodes[e]; !seen {
					nodes[e] = n
				}
			}
		})
	}
	return nodes
}

// nodeOf returns the Core IR node that the expression e stands for. Every expression of the rules
// and every expression that the classifier builds stands for one; nil stands for nil.
func (k *classifier) nodeOf(e ast.Expr) core.Node {
	if e == nil {
		return nil
	}
	n, ok := k.nodes[e]
	if !ok {
		panic(fmt.Sprintf("complexity: no Core IR node for %T %s", e, k.show(e)))
	}
	return n
}

// built records that the expression e, which the classifier builds, stands for the node n, and
// returns e.
func built[E ast.Expr](k *classifier, e E, n core.Node) E {
	if k.nodes == nil {
		k.nodes = map[ast.Expr]core.Node{}
	}
	k.nodes[e] = n
	return e
}

// callOf returns a reference to rule, which stands for a call of the rule.
func (k *classifier) callOf(rule string) *ast.Ref {
	return built(k, &ast.Ref{Name: rule}, core.Node(&core.Call{Rule: k.ruleIDs[rule]}))
}

// rightOperand returns the expression that stands for a right operand of an operator part of the
// Operators Rule of rule: the call of the rule that parses it. The classifier tells no level of
// operators apart, so the call applies every operator (core.Call.Level 0): it parses what the
// right operand of any operator part parses.
func (k *classifier) rightOperand(rule string) *ast.Ref {
	return built(k, &ast.Ref{Name: rule, Level: rhsLevel}, core.Node(&core.Call{Rule: k.ruleIDs[rule]}))
}

// prefixOf returns the sequence of the first items prefix of the sequence s, which stands for the
// sequence of their nodes.
func (k *classifier) prefixOf(s *ast.Seq, prefix []ast.Expr) *ast.Seq {
	items := make([]core.Node, len(prefix))
	for i, it := range prefix {
		items[i] = k.nodeOf(it)
	}
	return built(k, &ast.Seq{SourceInfo: s.SourceInfo, Items: prefix}, core.Node(&core.Seq{Items: items}))
}

// succeeds reports a proof that the expression e cannot fail (corepass.Facts.Succeeds).
func (k *classifier) succeeds(e ast.Expr) bool {
	return k.facts.source.Facts.Succeeds(k.nodeOf(e))
}

// leftCalls returns the rules that the expression e may call before it consumes input, in the
// order of e, a rule called at several such places listed at each (corepass.Facts.LeftCalls).
func (k *classifier) leftCalls(e ast.Expr) []string {
	ids := k.facts.source.Facts.LeftCalls(k.nodeOf(e), false)
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = k.rules[id].Name
	}
	return names
}

// literalRest returns a literal of the characters rest, the rest of a literal after a prefix the
// exclusion analysis compared, which stands for the literal node of them. The runes give valid
// UTF-8, the text of the node as the front end builds it.
func (k *classifier) literalRest(rest []rune, ignoreCase bool) *ast.Literal {
	text := string(rest)
	return built(k, &ast.Literal{Value: text, IgnoreCase: ignoreCase}, core.Node(&core.Lit{Text: text, Fold: ignoreCase}))
}

// looseCut reports whether e has a cut that commits the choice, option or repetition around it
// (corepass.Facts.LooseCut).
func (k *classifier) looseCut(e ast.Expr) bool {
	return k.facts.source.Facts.LooseCut(k.nodeOf(e))
}
