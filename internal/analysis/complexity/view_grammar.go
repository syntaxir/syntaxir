package complexity

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/ir/core"
	syntaxfmt "syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Grammar returns the rules of the view as grammar rules: rules are the rules the source module
// is built from, rule i of the module from rules[i]. A node the view shares with the module is
// the expression it is built from, and a node the view builds is written in the grammar's syntax
// at the position of the expression it rewrites, so that the formatted rules can be compared with
// the grammar, the complexity classifier can read them, and the view can run on an engine that
// reads grammars.
func (v *View) Grammar(rules []*ast.RuleDef) ([]*ast.RuleDef, error) {
	out, _, err := v.GrammarText(rules)
	return out, err
}

// GrammarText returns the rules of Grammar and, for each expression of them that the view
// builds, the source text it stands for (Text).
func (v *View) GrammarText(rules []*ast.RuleDef) ([]*ast.RuleDef, map[ast.Expr]string, error) {
	w, err := v.written(rules)
	if err != nil {
		return nil, nil, err
	}
	return w.rules, w.text, nil
}

// writtenView is the view written as grammar rules (GrammarText), with the node of the view that
// each expression of the rules stands for.
type writtenView struct {
	rules []*ast.RuleDef
	text  map[ast.Expr]string
	// nodes gives, for each expression of rules, the node of the view that it is written from: for
	// an expression that several nodes are written from, as an expression of several attributes,
	// the outermost of them.
	nodes map[ast.Expr]core.Node
	// exprs gives, for each node of the view that an expression of rules is written from, that
	// expression: the expression the node was built from, or the one the writer wrote for it.
	exprs map[core.Node]ast.Expr
}

// written writes the view as grammar rules: rules are the rules the source module is built from.
func (v *View) written(rules []*ast.RuleDef) (*writtenView, error) {
	if len(rules) != len(v.Module.Rules) {
		return nil, fmt.Errorf("%d rules for a module of %d", len(rules), len(v.Module.Rules))
	}
	w := &writer{View: v, text: map[ast.Expr]string{}, nodes: map[ast.Expr]core.Node{}, exprs: map[core.Node]ast.Expr{}}
	out := make([]*ast.RuleDef, len(rules))
	for i, rd := range rules {
		body, err := w.expr(v.Module.Rules[i].Body)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", rd.Name, err)
		}
		// A part that the writer keeps as written, as the skip of a recovery whose body the view
		// rewrites, is a node that the view shares with the module.
		core.Walk(v.Module.Rules[i].Body, func(n core.Node) {
			if x, ok := v.origin(n); ok {
				w.record(x, n)
			} else if l, ok := v.chains[n]; ok {
				w.record(l.innerExpr(), n)
			}
		})
		if body == rd.Expr {
			out[i] = rd
			continue
		}
		cp := *rd
		cp.Expr = body
		out[i] = &cp
	}
	return &writtenView{rules: out, text: w.text, nodes: w.nodes, exprs: w.exprs}, nil
}

// writer writes nodes of a view as grammar expressions and records the source text of each
// expression that it writes for a built node, and the node that each expression is written from.
type writer struct {
	*View
	text  map[ast.Expr]string
	nodes map[ast.Expr]core.Node
	exprs map[core.Node]ast.Expr
}

// record records that the expression x was built from the node n: x stands for n unless it
// already stands for a node outside n, and n for x unless the writer wrote it.
func (v *writer) record(x ast.Expr, n core.Node) {
	if _, seen := v.nodes[x]; !seen {
		v.nodes[x] = n
	}
	if _, seen := v.exprs[n]; !seen {
		v.exprs[n] = x
	}
}

// expr returns the grammar expression of the node n of the view.
func (v *View) expr(n core.Node) (ast.Expr, error) {
	return (&writer{View: v}).expr(n)
}

// expr returns the grammar expression of the node n of the view.
func (v *writer) expr(n core.Node) (ast.Expr, error) {
	if e, ok := v.origin(n); ok {
		if v.nodes != nil {
			// The node is shared with the module, and so are the nodes below it: each expression
			// below e stands for the outermost node built from it.
			core.Walk(n, func(d core.Node) {
				if x, ok := v.origin(d); ok {
					v.record(x, d)
				} else if l, ok := v.chains[d]; ok {
					v.record(l.innerExpr(), d)
				}
			})
			v.nodes[e], v.exprs[n] = n, e
		}
		return e, nil
	}
	e, err := v.built(n)
	if err != nil || e == nil {
		return e, err
	}
	if v.text != nil {
		if t, ok := v.Text[n]; ok {
			v.text[e] = t
		}
	}
	if v.nodes != nil {
		// The parts of n are written first, so an expression that n shares with a part, as the
		// operand of an expression of attributes that the view drops, stands for n.
		v.nodes[e], v.exprs[n] = n, e
	}
	return e, nil
}

// built returns the grammar expression of the node n that the view builds.
func (v *writer) built(n core.Node) (ast.Expr, error) {
	if l, ok := v.chainOf(n); ok {
		return v.chainExpr(n, l)
	}
	if s, ok := v.replaces[n]; ok {
		if e, ok := v.origin(s); ok {
			r, err := v.withParts(n, e)
			if err != nil {
				return nil, err
			}
			if r != nil {
				if at, ok := v.at[n]; ok && at != s {
					v.place(r, at)
				}
				return r, nil
			}
		}
	}
	exprs := func(ns []core.Node) ([]ast.Expr, error) {
		es := make([]ast.Expr, len(ns))
		for i, x := range ns {
			e, err := v.expr(x)
			if err != nil {
				return nil, err
			}
			es[i] = e
		}
		return es, nil
	}
	e, err := v.spelled(n, exprs)
	if err != nil {
		return nil, err
	}
	if s, ok := v.at[n]; ok {
		v.place(e, s)
	}
	return e, nil
}

// place gives the expression e, which the writer built, the position of the module node at.
func (v *writer) place(e ast.Expr, at core.Node) {
	if o, ok := v.Module.Origin.Of(at).(interface{ Source() *ast.SourceInfo }); ok && o.Source() != nil {
		if d, ok := e.(interface{ Source() *ast.SourceInfo }); ok {
			*d.Source() = ast.SourceInfo{Pos: o.Source().Diagnostic()}
		}
	}
}

// spelled returns the grammar expression of the node n that the view builds, of a kind written
// in the grammar's syntax of its own, with the expressions exprs of its parts.
func (v *writer) spelled(n core.Node, exprs func([]core.Node) ([]ast.Expr, error)) (ast.Expr, error) {
	switch n := n.(type) {
	case *core.Seq:
		items, err := exprs(n.Items)
		if err != nil {
			return nil, err
		}
		return &ast.Seq{Items: items}, nil
	case *core.Choice:
		alts, err := exprs(core.Children(n))
		if err != nil {
			return nil, err
		}
		return &ast.Choice{Alts: alts}, nil
	case *core.Repeat:
		body, err := v.expr(n.Body)
		if err != nil {
			return nil, err
		}
		return &ast.Repeat{Expr: body, Min: n.Min, Max: n.Max}, nil
	case *core.Opt:
		body, err := v.expr(n.Body)
		if err != nil {
			return nil, err
		}
		return &ast.Optional{Expr: body}, nil
	case *core.Look:
		body, err := v.expr(n.Body)
		if err != nil {
			return nil, err
		}
		if n.Neg {
			return &ast.Not{Expr: body}, nil
		}
		return &ast.And{Expr: body}, nil
	case *core.ClassNode:
		c := &ast.CharClass{Negated: n.Class.Negated}
		for _, r := range n.Class.Ranges {
			c.Ranges = append(c.Ranges, ast.CharRange{Lo: r.Lo, Hi: r.Hi})
		}
		return c, nil
	case *core.Cut:
		return &ast.Cut{}, nil
	case *core.Succeed:
		return &ast.Top{}, nil
	case *core.FailNode:
		return &ast.Bottom{}, nil
	}
	return nil, fmt.Errorf("no expression for a built %T", n)
}

// withParts returns the expression e of the node of the module that the built node n replaces,
// with the parts of n: a copy of e with the expression of the body of n. It returns nil for a
// node whose kind is written in the grammar's syntax of its own.
func (v *writer) withParts(n core.Node, e ast.Expr) (ast.Expr, error) {
	var body core.Node
	switch n := n.(type) {
	case *core.Repeat:
		body = n.Body
	case *core.Opt:
		body = n.Body
	case *core.Look:
		body = n.Body
	case *core.Capture:
		body = n.Body
	case *core.Drop:
		body = n.Body
	case *core.Text:
		body = n.Body
	case *core.Label:
		body = n.Body
	case *core.Recover:
		body = n.Body
	case *core.Stream:
		body = n.Repeat
	default:
		return nil, nil
	}
	x, err := v.expr(body)
	if err != nil {
		return nil, err
	}
	switch e := e.(type) {
	case *ast.Repeat:
		r, ok := n.(*core.Repeat)
		if !ok {
			return nil, fmt.Errorf("a built %T of a %T", n, e)
		}
		c := *e
		c.Expr, c.Min, c.Max = x, r.Min, r.Max
		return &c, nil
	case *ast.Optional:
		c := *e
		c.Expr = x
		return &c, nil
	case *ast.And:
		c := *e
		c.Expr = x
		return &c, nil
	case *ast.Not:
		c := *e
		c.Expr = x
		return &c, nil
	case *ast.Capture:
		c := *e
		c.Expr = x
		return &c, nil
	case *ast.Discard:
		c := *e
		c.Expr = x
		return &c, nil
	case *ast.Atomic:
		c := *e
		c.Expr = x
		return &c, nil
	case *ast.Attributed:
		if len(e.Attrs) != 1 {
			return nil, fmt.Errorf("a built %T of %d attributes", n, len(e.Attrs))
		}
		c := *e
		c.Expr, c.Breaks = x, nil
		return &c, nil
	}
	return nil, fmt.Errorf("a built %T of a %T", n, e)
}

// BuiltTexts returns, for each node of the rules of the view that the view builds, its spelling
// in the grammar's syntax and the text it stands for, as "spelling = text", sorted. A node that
// cannot be written is spelled by its error.
func (v *View) BuiltTexts() []string {
	var out []string
	for _, r := range v.Module.Rules {
		// The nodes of the attributes of an expression of several attributes below the outermost
		// one are written with it, as one expression.
		inner := map[core.Node]bool{}
		core.Walk(r.Body, func(n core.Node) {
			if inner[n] {
				return
			}
			if l, ok := v.chainOf(n); ok {
				_, parts, _ := v.chainParts(n, l)
				for _, p := range parts[1:] {
					inner[p] = true
				}
			}
			if _, ok := v.origin(n); ok {
				return
			}
			spelling := ""
			if e, err := v.expr(n); err != nil {
				spelling = err.Error()
			} else {
				spelling = syntaxfmt.FormatExpr(e)
			}
			out = append(out, spelling+" = "+v.Text[n])
		})
	}
	slices.Sort(out)
	return out
}

// An expression of several attributes, as `(X @error(message="m")) #recover(skip=s)`, is one
// grammar expression that the module builds into one node per attribute, the last attribute
// outermost: a Label for @error and @diagnostic, a Recover for #recover, a Missing for #missing,
// and for #stream the Stream of the repetition, innermost. Only the outermost node has the
// expression as its origin. An attrChain records the others, so that the view writes the nodes
// that it keeps back as one expression.
type attrChain struct {
	e *ast.Attributed
	// nodes[i] is the node of the module that attribute i is built into, nil for #stream, whose
	// node is stream.
	nodes  []core.Node
	stream core.Node
}

// A chainLink is the place of a node in an attrChain: the attribute at it is built from.
type chainLink struct {
	chain *attrChain
	at    int
}

// attrChains returns the place of each node of the module of src that an attribute of an
// expression of several attributes is built into.
func attrChains(m *core.Module) map[core.Node]chainLink {
	links := map[core.Node]chainLink{}
	for _, r := range m.Rules {
		core.Walk(r.Body, func(n core.Node) {
			e, ok := m.Origin.Of(n).(*ast.Attributed)
			if !ok || len(e.Attrs) < 2 {
				return
			}
			c := &attrChain{e: e, nodes: make([]core.Node, len(e.Attrs))}
			streamAt := -1
			node := n
			for i := len(e.Attrs) - 1; i >= 0; i-- {
				if e.Attrs[i].Name == "stream" {
					streamAt = i
					continue
				}
				if !attrNode(e.Attrs[i].Name, node) {
					return
				}
				c.nodes[i], node = node, attrBody(node)
			}
			if streamAt >= 0 {
				if _, ok := node.(*core.Stream); !ok {
					return
				}
				c.stream = node
				links[node] = chainLink{c, streamAt}
			}
			for i, x := range c.nodes {
				if x != nil {
					links[x] = chainLink{c, i}
				}
			}
		})
	}
	return links
}

// innerExpr returns the expression that the node at an inner place of the chain stands for, below
// the outermost: the attributes up to the one at it, around the operand.
func (l chainLink) innerExpr() *ast.Attributed {
	c := *l.chain.e
	c.Attrs, c.Breaks = c.Attrs[:l.at+1], nil
	return &c
}

// attrNode reports whether n is of the kind that the attribute name is built into.
func attrNode(name string, n core.Node) bool {
	switch n.(type) {
	case *core.Label:
		return name == "error" || name == "diagnostic"
	case *core.Recover:
		return name == "recover"
	case *core.Missing:
		return name == "missing"
	}
	return false
}

// attrBody returns the operand of the attribute node n.
func attrBody(n core.Node) core.Node {
	switch n := n.(type) {
	case *core.Label:
		return n.Body
	case *core.Recover:
		return n.Body
	case *core.Missing:
		return n.Body
	case *core.Stream:
		return n.Repeat
	}
	return nil
}

// chainOf returns the place in an attrChain of the node n of the view: that of n, or of the node
// of the module that n replaces.
func (v *View) chainOf(n core.Node) (chainLink, bool) {
	if l, ok := v.chains[n]; ok {
		return l, true
	}
	if s, ok := v.replaces[n]; ok {
		l, ok := v.chains[s]
		return l, ok
	}
	return chainLink{}, false
}

// chainParts follows the chain of l from its node n of the view down to the operand of the
// attributes: keep reports the attributes whose nodes the view keeps (a rule may drop one, as N7
// drops a label), parts are n and the kept nodes below it, and body is the operand.
func (v *View) chainParts(n core.Node, l chainLink) (keep []bool, parts []core.Node, body core.Node) {
	c := l.chain
	keep = make([]bool, len(c.e.Attrs))
	parts = []core.Node{n}
	node := n
	is := func(x, src core.Node) bool { return x == src || v.replaces[x] == src }
	for i := l.at; i >= 0; i-- {
		if src := c.nodes[i]; src != nil && is(node, src) {
			keep[i] = true
			if node != n {
				parts = append(parts, node)
			}
			node = attrBody(node)
		}
	}
	if c.stream != nil && is(node, c.stream) {
		// #stream has no node of its own: it stays with the Stream.
		for i, a := range c.e.Attrs {
			keep[i] = keep[i] || a.Name == "stream"
		}
		if node != n {
			parts = append(parts, node)
		}
		node = attrBody(node)
	}
	return keep, parts, node
}

// chainExpr returns the expression of the node n of the view at l: the expression of the
// attributes whose nodes the view keeps from n down, around the expression of their operand. Each
// kept node below n stands for the expression of the attributes from it down around the same
// operand, which the rules do not hold but the classifier can read it by.
func (v *writer) chainExpr(n core.Node, l chainLink) (ast.Expr, error) {
	_, parts, body := v.chainParts(n, l)
	if body == nil {
		return nil, fmt.Errorf("a built %T without an operand", n)
	}
	x, err := v.expr(body)
	if err != nil {
		return nil, err
	}
	if v.nodes != nil {
		for _, p := range parts[1:] {
			if lp, ok := v.chainOf(p); ok {
				pe := v.chainOver(p, lp, x)
				if _, seen := v.nodes[pe]; !seen {
					v.nodes[pe] = p
				}
				v.exprs[p] = pe
			}
		}
	}
	return v.chainOver(n, l, x), nil
}

// chainOver returns the expression of the attributes whose nodes the view keeps from the node n
// at l down, around x, the expression of their operand.
func (v *writer) chainOver(n core.Node, l chainLink, x ast.Expr) ast.Expr {
	keep, _, _ := v.chainParts(n, l)
	e := l.chain.e
	var attrs []*ast.Attribute
	for i, a := range e.Attrs {
		if keep[i] {
			attrs = append(attrs, a)
		}
	}
	if len(attrs) == 0 {
		return x
	}
	if x == e.Expr && len(attrs) == len(e.Attrs) {
		return e
	}
	c := *e
	c.Expr, c.Attrs, c.Breaks = x, attrs, nil
	return &c
}
