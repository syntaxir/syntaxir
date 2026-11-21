package examinedref

import (
	"fmt"
	"strings"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// rule is a rule of the grammar as the reference evaluates it.
type rule struct {
	name string
	body ast.Expr
	// action reports a Value Action other than the default `=> ...`: the rule's value is that of a
	// user action, which the reference does not compute.
	action bool
	// ownNode reports a rule that makes a node of its own: a rule that is not hidden, or a hidden
	// rule with an action, including the default action of an inline action.
	ownNode bool
	// typed reports a declared type; nodeType is the named node type that the rule declares, if any.
	typed    bool
	nodeType string
	// ops and fold are the Operators Rule of the body, if it is one, and whether #ignorecase follows it.
	ops  *ast.OperatorsRule
	fold bool
}

// ruleSet holds the rules in declaration order.
type ruleSet struct {
	byName map[string]*rule
	order  []string
}

// newRuleSet makes the rules of g. An inline action (e => A) is a call of the hidden rule _r_n whose
// body is e and whose action is A (spec/language/actions.md, Inline Value Actions): n numbers the
// inline actions of the rule r in source order, inner actions first, and an underscore is added while
// a declared rule has the name. The hidden rules of a rule are declared right after it, in the order
// of their numbers, which decides their place among the leaders of left recursion.
func newRuleSet(g *ast.Grammar) (*ruleSet, error) {
	defs := g.Rules()
	rs := &ruleSet{byName: map[string]*rule{}}
	taken := map[string]bool{}
	nodeTypes := map[string]bool{}
	for _, d := range g.Types() {
		if td, ok := d.(*ast.TypeDef); ok && td.NodeValue() != nil {
			nodeTypes[td.Name] = true
		}
	}
	for _, d := range defs {
		taken[d.Name] = true
	}
	for _, d := range defs {
		if _, dup := rs.byName[d.Name]; dup {
			return nil, fmt.Errorf("examinedref: rule %s is declared twice", d.Name)
		}
		l := &inlineLowering{holder: d.Name, taken: taken}
		body := l.expr(d.Expr)
		r := &rule{
			name:    d.Name,
			body:    body,
			action:  d.Action != nil && !d.DefaultAction,
			ownNode: !hidden(d.Name) || d.Action != nil || d.DefaultAction,
			typed:   d.Type != nil,
		}
		if t, ok := d.Type.(*ast.TypeRef); ok && nodeTypes[t.Name] {
			r.nodeType = t.Name
		}
		r.ops, r.fold = operatorsOf(body)
		rs.add(r)
		for _, h := range l.hidden {
			h.ops, h.fold = operatorsOf(h.body)
			rs.add(h)
		}
	}
	return rs, nil
}

func (rs *ruleSet) add(r *rule) {
	rs.byName[r.name] = r
	rs.order = append(rs.order, r.name)
}

func hidden(name string) bool { return strings.HasPrefix(name, "_") }

// operatorsOf returns the Operators Rule of a rule body and whether #ignorecase follows it.
func operatorsOf(body ast.Expr) (*ast.OperatorsRule, bool) {
	switch e := body.(type) {
	case *ast.OperatorsRule:
		return e, false
	case *ast.Attributed:
		if o, ok := e.Expr.(*ast.OperatorsRule); ok {
			for _, a := range e.Attrs {
				if a.Name == "ignorecase" {
					return o, true
				}
			}
			return o, false
		}
	}
	return nil, false
}

// inlineLowering replaces the inline actions of one rule by calls of hidden rules.
type inlineLowering struct {
	holder string
	// fold reports an expression inside #ignorecase, whose literals stay case-insensitive in the
	// hidden rules made from it.
	fold   bool
	count  int
	taken  map[string]bool
	hidden []*rule
}

// expr returns e with its inline actions replaced; an expression without one is returned as it is.
func (l *inlineLowering) expr(e ast.Expr) ast.Expr {
	switch e := e.(type) {
	case *ast.InlineAction:
		body := l.expr(e.Expr)
		if l.fold {
			body = &ast.Attributed{SourceInfo: e.SourceInfo, Expr: body, Attrs: []*ast.Attribute{{Name: "ignorecase", Pragma: true}}}
		}
		l.count++
		name := fmt.Sprintf("_%s_%d", l.holder, l.count)
		for l.taken[name] {
			name += "_"
		}
		l.taken[name] = true
		l.hidden = append(l.hidden, &rule{
			name:    name,
			body:    body,
			action:  e.Action != nil && !e.DefaultAction,
			ownNode: true,
		})
		return &ast.Ref{SourceInfo: e.SourceInfo, Name: name}
	case *ast.Seq:
		items := l.list(e.Items)
		if items == nil {
			return e
		}
		c := *e
		c.Items = items
		return &c
	case *ast.Choice:
		alts := l.list(e.Alts)
		if alts == nil {
			return e
		}
		c := *e
		c.Alts = alts
		return &c
	case *ast.Group:
		return l.wrap(e, e.Expr, func(x ast.Expr) ast.Expr { c := *e; c.Expr = x; return &c })
	case *ast.Repeat:
		return l.wrap(e, e.Expr, func(x ast.Expr) ast.Expr { c := *e; c.Expr = x; return &c })
	case *ast.Optional:
		return l.wrap(e, e.Expr, func(x ast.Expr) ast.Expr { c := *e; c.Expr = x; return &c })
	case *ast.And:
		return l.wrap(e, e.Expr, func(x ast.Expr) ast.Expr { c := *e; c.Expr = x; return &c })
	case *ast.Not:
		return l.wrap(e, e.Expr, func(x ast.Expr) ast.Expr { c := *e; c.Expr = x; return &c })
	case *ast.Stream:
		return l.wrap(e, e.Expr, func(x ast.Expr) ast.Expr { c := *e; c.Expr = x; return &c })
	case *ast.Atomic:
		return l.wrap(e, e.Expr, func(x ast.Expr) ast.Expr { c := *e; c.Expr = x; return &c })
	case *ast.Discard:
		return l.wrap(e, e.Expr, func(x ast.Expr) ast.Expr { c := *e; c.Expr = x; return &c })
	case *ast.Capture:
		return l.wrap(e, e.Expr, func(x ast.Expr) ast.Expr { c := *e; c.Expr = x; return &c })
	case *ast.Attributed:
		outer := l.fold
		for _, a := range e.Attrs {
			l.fold = l.fold || a.Name == "ignorecase"
		}
		defer func() { l.fold = outer }()
		return l.wrap(e, e.Expr, func(x ast.Expr) ast.Expr { c := *e; c.Expr = x; return &c })
	case *ast.Recovery:
		x, skip := l.expr(e.Expr), l.expr(e.Skip)
		if x == e.Expr && skip == e.Skip {
			return e
		}
		c := *e
		c.Expr, c.Skip = x, skip
		return &c
	case *ast.OperatorsRule:
		c := *e
		changed := false
		if e.Skip != nil {
			c.Skip = l.expr(e.Skip)
			changed = c.Skip != e.Skip
		}
		c.Operands = nil
		for _, o := range e.Operands {
			oc := *o
			oc.Expr = l.expr(o.Expr)
			changed = changed || oc.Expr != o.Expr
			c.Operands = append(c.Operands, &oc)
		}
		c.Levels = nil
		for _, lv := range e.Levels {
			lc := *lv
			lc.Operators = nil
			for _, op := range lv.Operators {
				opc := *op
				opc.Expr = l.expr(op.Expr)
				changed = changed || opc.Expr != op.Expr
				lc.Operators = append(lc.Operators, &opc)
			}
			c.Levels = append(c.Levels, &lc)
		}
		if !changed {
			return e
		}
		return &c
	}
	return e
}

// list lowers a list of expressions; it returns nil when none changed.
func (l *inlineLowering) list(es []ast.Expr) []ast.Expr {
	out := make([]ast.Expr, len(es))
	changed := false
	for i, x := range es {
		out[i] = l.expr(x)
		changed = changed || out[i] != x
	}
	if !changed {
		return nil
	}
	return out
}

func (l *inlineLowering) wrap(e, inner ast.Expr, rebuild func(ast.Expr) ast.Expr) ast.Expr {
	x := l.expr(inner)
	if x == inner {
		return e
	}
	return rebuild(x)
}
