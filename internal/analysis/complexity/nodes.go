package complexity

import (
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The helpers that the classifier's walks of Core IR nodes share.

// ruleBody returns the node of the body of the rule name.
func (k *classifier) ruleBody(name string) core.Node {
	return k.nodeOf(k.byName[name].Expr)
}

// walkNodes calls f for n and the nodes inside it in preorder, as core.Walk does, and the bodies
// of the lines of an Operators Rule, but stops at a right operand of an operator part
// (isRightOperand) as at a call: the nodes of the Operators Rule that it inlines are those of its
// rule.
func walkNodes(n core.Node, f func(core.Node)) {
	if n == nil {
		return
	}
	f(n)
	switch x := n.(type) {
	case *core.OperatorsRule:
		for _, l := range x.Lines() {
			walkNodes(l.Body, f)
		}
		return
	case *core.Nest:
		if isRightOperand(x) {
			return
		}
	}
	for _, c := range core.Children(n) {
		walkNodes(c, f)
	}
}

// calledRule returns the rule that n calls, for a call or a right operand of an operator part
// (isRightOperand).
func (k *classifier) calledRule(n core.Node) (string, bool) {
	switch n := n.(type) {
	case *core.Call:
		return k.rules[n.Rule].Name, true
	case *core.Nest:
		if isRightOperand(n) {
			return k.rules[n.Rule].Name, true
		}
	}
	return "", false
}

// nullableOf reports whether n can succeed without consuming input when token insertion is
// enabled: the nullability of n with insertion (corepass.Facts.NullableIns), as the nullable rules
// (nullRule) are.
func (k *classifier) nullableOf(n core.Node) bool { return k.facts.source.Facts.NullableIns(n) }

// succeedsOf reports a proof that n cannot fail (corepass.Facts.Succeeds).
func (k *classifier) succeedsOf(n core.Node) bool { return k.facts.source.Facts.Succeeds(n) }

// looseCutOf reports whether n has a cut that commits the choice, option or repetition around it
// (corepass.Facts.LooseCut).
func (k *classifier) looseCutOf(n core.Node) bool { return k.facts.source.Facts.LooseCut(n) }

// posOf returns the position of the node n: that of the grammar expression it stands for.
func (k *classifier) posOf(n core.Node) ast.Pos { return exprPos(k.exprOf(n)) }

// showOf returns the text of the node n for messages: that of the grammar expression it stands for.
func (k *classifier) showOf(n core.Node) string { return k.show(k.exprOf(n)) }

// predicateOf returns the predicate that the node n of a predicate or a binding stands for, whose
// action expression the walk of action expressions reads (walkAction).
func (k *classifier) predicateOf(n core.Node) *ast.Predicate { return k.exprOf(n).(*ast.Predicate) }
