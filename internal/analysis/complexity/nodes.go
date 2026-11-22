package complexity

import (
	"slices"

	"syntaxir.dev/sxir/internal/ir/core"
	syntaxfmt "syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The helpers that the classifier's walks of Core IR nodes share.

// ruleBody returns the node of the body of the rule name.
func (k *classifier) ruleBody(name string) core.Node {
	return k.byName[name].Body
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

// posOf returns the position of the node n for messages: that of its spelling.
func (k *classifier) posOf(n core.Node) ast.Pos { return exprPos(k.spelling(n)) }

// showOf returns the text of the node n for messages: the source text that a node the view builds
// stands for, or else its spelling.
func (k *classifier) showOf(n core.Node) string {
	if t, ok := k.view.Text[n]; ok {
		return abbreviate(t)
	}
	return abbreviate(syntaxfmt.FormatExpr(k.spelling(n)))
}

// spelling returns the grammar expression that spells the node n in messages, where it gives its
// text and its position: the expression of the grammar that a node of the module is built from,
// the expression of the attributes up to a node of an attribute below the outermost
// (chainLink.innerExpr), the expression that the view writes for a node it builds (View.expr),
// and the one the classifier gives a node that it builds (classifier.spelled).
func (k *classifier) spelling(n core.Node) ast.Expr {
	if e, ok := k.spelled[n]; ok {
		return e
	}
	if e, ok := k.view.origin(n); ok {
		return e
	}
	if l, ok := k.view.chains[n]; ok {
		return l.innerExpr()
	}
	e, err := k.view.expr(n)
	if err != nil {
		// Every node the view builds has a spelling; an error is a defect of the view.
		panic("complexity: cost view: " + err.Error())
	}
	k.spell(n, e)
	return e
}

// spell records e as the spelling of the node n (spelling).
func (k *classifier) spell(n core.Node, e ast.Expr) {
	if k.spelled == nil {
		k.spelled = map[core.Node]ast.Expr{}
	}
	k.spelled[n] = e
}

// bindOf returns the binding that the binding node n is built from, whose position and text the
// messages about it give.
func (k *classifier) bindOf(n *core.BindNode) *ast.Bind {
	return k.spelling(n).(*ast.Predicate).Action.(*ast.Bind)
}

// actionOrigin returns the action expression of the grammar that t is built from
// (core.Origin.OfActionExpr), or nil for one that the classifier builds.
func (k *classifier) actionOrigin(t core.ActionExpr) ast.ActionExpr {
	return k.facts.source.Module.Origin.OfActionExpr(t)
}

// actionPos returns the position of the action expression t: that of the action expression of the
// grammar it is built from, none for one that the classifier builds.
func (k *classifier) actionPos(t core.ActionExpr) ast.Pos { return actionPosOf(k.actionOrigin(t)) }

// showAction returns the text of the action expression t for messages: that of the action
// expression of the grammar it is built from, or the Core IR text of one that the classifier builds.
func (k *classifier) showAction(t core.ActionExpr) string {
	if x := k.actionOrigin(t); x != nil {
		return abbreviate(syntaxfmt.FormatActionExpr(x))
	}
	return abbreviate(core.ActionExprString(t))
}

// capRef returns the name that the action expression t reads when it reads a capture or a lambda
// parameter, and whether it is a lambda parameter: a capture slot, a lambda parameter, or $lhs,
// $rhs, $op or $value of an Operators Rule, which the grammar writes as captures.
func capRef(t core.ActionExpr) (name string, param, ok bool) {
	switch t := t.(type) {
	case *core.SlotRef:
		return t.Name, false, true
	case *core.Local:
		return t.Name, t.Kind == core.LocalParam, true
	}
	return "", false, false
}

// ruleAction returns the action of the rule name, or nil: the whole-rule action of an Operators
// Rule, whose operands and operator parts have their own (valueLines).
func (k *classifier) ruleAction(name string) core.ActionExpr {
	return k.facts.source.Module.Rules[k.ruleIDs[name]].Finish.Action
}

// valueLines returns the lines of the Operators Rule pr that make its values: its operands, then
// its operator parts, level by level, without its skip line.
func valueLines(pr *core.OperatorsRule) []*core.OpLine {
	ls := slices.Clone(pr.Operands)
	for _, op := range pr.Ops {
		ls = append(ls, op.Line)
	}
	return ls
}
