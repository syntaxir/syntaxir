package parser

import (
	"maps"
	"math"
	"slices"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The level count of the lexer bounds the recursion of the parsers, but not the structural depth
// of the tree they build: a chain of pragmas, of a left-associative operator or of field accesses
// opens no levels however long it is. The parsers therefore check the tree they built against
// ast.MaxNestingDepth, counted as validation counts it, so that the walks over a parsed grammar
// stay bounded and validation accepts every tree that the parsers return.

// tooDeep returns the first node of the declaration or package attribute root, in source order,
// that lies deeper than ast.MaxNestingDepth, or nil. The walk is iterative.
func tooDeep(root any) any {
	type frame struct {
		node  any
		depth int
	}
	stack := []frame{{root, depthWeight(root)}}
	// seen holds the deepest place where each node was checked, as a parsed tree may share a node.
	seen := map[any]int{}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if f.depth > ast.MaxNestingDepth {
			return f.node
		}
		if d, ok := seen[f.node]; ok && d >= f.depth {
			continue
		}
		seen[f.node] = f.depth
		children := depthChildren(f.node)
		for i := len(children) - 1; i >= 0; i-- {
			c := children[i]
			stack = append(stack, frame{c, f.depth + depthWeight(c)})
		}
	}
	return nil
}

// depthWeight is the depth that node adds below its parent, as ast.MaxNestingDepth states it.
func depthWeight(node any) int {
	switch n := node.(type) {
	case *ast.Group, *ast.ActionGroup, *ast.TypeGroup:
		return 0
	case *ast.IntLit:
		if n.Value < 0 && n.Value != math.MinInt {
			return 2
		}
	}
	return 1
}

// depthChildren returns the child nodes of node in source order: the nodes that validation
// walks below it.
func depthChildren(node any) []any {
	var out []any
	// add adds an attribute value that is a node: a parser expression.
	add := func(v any) {
		switch v.(type) {
		case nil, int, string, bool:
		default:
			out = append(out, v)
		}
	}
	addNode := func(n any, present bool) {
		if present {
			out = append(out, n)
		}
	}
	switch n := node.(type) {
	case *ast.TypeDef:
		addNode(n.Type, n.Type != nil)
	case *ast.TypeAlias:
		addNode(n.Type, n.Type != nil)
	case *ast.VarDef:
		addNode(n.Type, n.Type != nil)
		addNode(n.Value, n.Value != nil)
	case *ast.RuleDef:
		for _, a := range n.Attrs {
			out = append(out, a)
		}
		addNode(n.Type, n.Type != nil)
		addNode(n.Expr, n.Expr != nil)
		addNode(n.Action, n.Action != nil)
	case *ast.StructType:
		for _, fd := range n.Fields {
			out = append(out, fd)
		}
	case *ast.Field:
		addNode(n.Type, n.Type != nil)
	case *ast.ListType:
		addNode(n.Elem, n.Elem != nil)
	case *ast.OptionalType:
		addNode(n.Elem, n.Elem != nil)
	case *ast.TypeGroup:
		addNode(n.Type, n.Type != nil)
	case *ast.JoinType:
		for _, t := range n.Types {
			out = append(out, t)
		}
	case *ast.MeetType:
		for _, t := range n.Types {
			out = append(out, t)
		}
	case *ast.TupleType:
		for _, t := range n.Types {
			out = append(out, t)
		}
	case *ast.TypeApp:
		for _, t := range n.Args {
			out = append(out, t)
		}
	case *ast.Seq:
		for _, e := range n.Items {
			out = append(out, e)
		}
	case *ast.Choice:
		for _, e := range n.Alts {
			out = append(out, e)
		}
	case *ast.Repeat:
		addNode(n.Expr, n.Expr != nil)
	case *ast.Group:
		addNode(n.Expr, n.Expr != nil)
	case *ast.InlineAction:
		addNode(n.Expr, n.Expr != nil)
		addNode(n.Action, n.Action != nil)
	case *ast.Optional:
		addNode(n.Expr, n.Expr != nil)
	case *ast.And:
		addNode(n.Expr, n.Expr != nil)
	case *ast.Not:
		addNode(n.Expr, n.Expr != nil)
	case *ast.Atomic:
		addNode(n.Expr, n.Expr != nil)
	case *ast.Recovery:
		addNode(n.Expr, n.Expr != nil)
		addNode(n.Skip, n.Skip != nil)
	case *ast.Stream:
		addNode(n.Expr, n.Expr != nil)
	case *ast.Discard:
		addNode(n.Expr, n.Expr != nil)
	case *ast.Capture:
		addNode(n.Expr, n.Expr != nil)
	case *ast.Predicate:
		addNode(n.Action, n.Action != nil)
	case *ast.Attributed:
		addNode(n.Expr, n.Expr != nil)
		for _, a := range n.Attrs {
			out = append(out, a)
		}
	case *ast.Attribute:
		for _, v := range n.Args {
			add(v)
		}
		for _, key := range slices.Sorted(maps.Keys(n.KwArgs)) {
			add(n.KwArgs[key])
		}
	case *ast.OperatorsRule:
		addNode(n.Skip, n.Skip != nil)
		for _, o := range n.Operands {
			out = append(out, o)
		}
		for _, l := range n.Levels {
			out = append(out, l)
		}
	case *ast.OperatorsRuleOperand:
		addNode(n.Expr, n.Expr != nil)
		addNode(n.Action, n.Action != nil)
	case *ast.OperatorsRuleLevel:
		for _, o := range n.Operators {
			out = append(out, o)
		}
	case *ast.OperatorsRuleOperator:
		addNode(n.Expr, n.Expr != nil)
		addNode(n.Action, n.Action != nil)
	case *ast.Block:
		for _, st := range n.Statements {
			out = append(out, st)
		}
	case *ast.FieldAccess:
		addNode(n.X, n.X != nil)
	case *ast.ActionGroup:
		addNode(n.Action, n.Action != nil)
	case *ast.Constructor:
		for _, fi := range n.Fields {
			out = append(out, fi)
		}
		for _, a := range n.Args {
			out = append(out, a)
		}
	case *ast.TupleLit:
		for _, e := range n.Elems {
			out = append(out, e)
		}
	case *ast.ListConstructor:
		addNode(n.Elem, n.Elem != nil)
		for _, e := range n.Items {
			out = append(out, e)
		}
	case *ast.FieldInit:
		addNode(n.Value, n.Value != nil)
	case *ast.Call:
		for _, a := range n.Args {
			out = append(out, a)
		}
	case *ast.Lambda:
		addNode(n.Body, n.Body != nil)
	case *ast.Binary:
		addNode(n.L, n.L != nil)
		addNode(n.R, n.R != nil)
	case *ast.Unary:
		addNode(n.X, n.X != nil)
	case *ast.Cast:
		addNode(n.X, n.X != nil)
		addNode(n.Type, n.Type != nil)
	case *ast.Bind:
		addNode(n.Value, n.Value != nil)
	}
	return out
}

// boundDepth removes from g the declarations, and the package clause, that hold a node deeper
// than ast.MaxNestingDepth, and returns an error "nesting too deep" at each such node.
func boundDepth(g *ast.Grammar) []*Error {
	if g == nil {
		return nil
	}
	var errs []*Error
	report := func(root, node any) {
		errs = append(errs, &Error{Pos: depthPos(root, node), Msg: msgNestingTooDeep})
	}
	for _, a := range g.File.Pkg.Attrs {
		if deep := tooDeep(a); deep != nil {
			report(a, deep)
			g.File.Pkg = ast.Pkg{}
			break
		}
	}
	kept := g.File.Decls[:0]
	for _, d := range g.File.Decls {
		if deep := tooDeep(d); deep != nil {
			report(d, deep)
			continue
		}
		kept = append(kept, d)
	}
	clear(g.File.Decls[len(kept):])
	g.File.Decls = kept
	return errs
}

// depthPos returns the diagnostic position of node, or that of root when node has none.
func depthPos(root, node any) ast.Pos {
	for _, n := range []any{node, root} {
		if s, ok := n.(ast.SourceNode); ok {
			if pos := s.Source().Diagnostic(); pos.IsValid() {
				return pos
			}
		}
	}
	return ast.Pos{}
}
