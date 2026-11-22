package core

import "syntaxir.dev/sxir/pkg/grammar/ast"

// Number gives every node of the rules of m that has no ID one, in the order of the rules and of
// Walk, after LastID, and sets LastID; then every action expression of the rules that has no ID
// one, in the order of the rules and of their nodes (eachActionExpr), after LastActionExprID, and
// sets LastActionExprID. The builder of a module numbers it once it is built; a node or an action
// expression reached twice keeps the ID it got first.
func Number(m *Module) {
	for _, r := range m.Rules {
		if r == nil {
			continue
		}
		Walk(r.Body, func(n Node) {
			if a := n.Attrs(); a.ID == 0 {
				a.ID = m.NewID()
			}
		})
	}
	for _, r := range m.Rules {
		if r == nil {
			continue
		}
		eachActionExpr(r, func(t ActionExpr) {
			WalkActionExpr(t, func(x ActionExpr) {
				if b := x.exprBase(); b.id == 0 {
					m.LastActionExprID++
					b.id = m.LastActionExprID
				}
			})
		})
	}
}

// eachActionExpr calls f for each action expression that the rule r holds at the top: its action,
// the actions of the lines of an Operators Rule and the action expressions of its predicates and
// bindings, in the order of Walk and with the action of the rule last.
func eachActionExpr(r *Rule, f func(ActionExpr)) {
	Walk(r.Body, func(n Node) {
		switch n := n.(type) {
		case *Pred:
			f(n.Action)
		case *BindNode:
			f(n.Action)
		case *OperatorsRule:
			for _, l := range n.Lines() {
				if l.Action != nil {
					f(l.Action)
				}
			}
		}
	})
	if r.Finish.Action != nil {
		f(r.Finish.Action)
	}
}

// NewID returns an ID that no node of m has, for a node that a pass adds and that takes the place
// of no other.
func (m *Module) NewID() NodeID {
	m.LastID++
	return m.LastID
}

// ActionExprID identifies an action expression of a module (ActionExprIDOf); 0 identifies none.
// The action expressions are numbered apart from the nodes.
type ActionExprID int32

// Origin maps the nodes and the action expressions of a module, by their IDs (Attrs.ID,
// ActionExprIDOf), to the grammar expressions and the action expressions of the grammar they were
// built from, as the build of the module recorded them (Module.Origin). It holds across the passes
// of the module, which keep the IDs of the nodes they keep and give a node that takes the place of
// another the ID of the node it replaces; no pass builds action expressions. A module built
// without its origin has empty maps.
type Origin struct {
	Exprs       map[NodeID]ast.Expr
	ActionExprs map[ActionExprID]ast.ActionExpr
}

// Of returns the expression the node n was built from, or nil: for a node without an ID (one that a
// pass or a view built and that takes the place of none), or a module built without its origin.
func (o Origin) Of(n Node) ast.Expr {
	if n == nil || n.Attrs().ID == 0 {
		return nil
	}
	return o.Exprs[n.Attrs().ID]
}

// OfActionExpr returns the action expression of the grammar that the action expression t was built
// from, or nil: for an expression without an ID, or a module built without its origin.
func (o Origin) OfActionExpr(t ActionExpr) ast.ActionExpr {
	if t == nil || ActionExprIDOf(t) == 0 {
		return nil
	}
	return o.ActionExprs[ActionExprIDOf(t)]
}
