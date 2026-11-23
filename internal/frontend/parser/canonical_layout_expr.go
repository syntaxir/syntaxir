package parser

import (
	"slices"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// This file replays the layout inside rule expressions and actions (see
// layoutReplay). Each method mirrors the parser method of the same name and
// starts at the first token of the node it replays.

// bodyChoice replays the top-level choice of a rule body or a group.
func (r *layoutReplay) bodyChoice(e ast.Expr) {
	lc := r.lc
	c, ok := e.(*ast.Choice)
	if !ok {
		r.bodySequence(e)
		return
	}
	r.bodySequence(c.Alts[0])
	breaks := []*ast.LineBreak{nil}
	broken := false
	for i, alt := range c.Alts[1:] {
		var b *ast.LineBreak
		if lc.atLineStart() {
			b = lc.lineBreak()
			broken = true
		} else {
			lc.afterGap(c.Alts[i])
			if lc.peek(1).lineBefore {
				b = lc.lineBreak()
				broken = true
			}
		}
		lc.next() // /
		lead := lc.takeCommentGap()
		r.bodySequence(alt)
		lc.leadOf(alt, lead)
		breaks = append(breaks, b)
	}
	c.Breaks = nil
	if broken {
		c.Breaks = breaks
	}
}

// bodySequence replays a sequence of a rule body or a group, with its line
// breaks.
func (r *layoutReplay) bodySequence(e ast.Expr) {
	lc := r.lc
	s, ok := e.(*ast.Seq)
	if !ok {
		r.prefixed(e)
		return
	}
	breaks := make([]*ast.LineBreak, 0, len(s.Items))
	broken := false
	for i, item := range s.Items {
		var b *ast.LineBreak
		if i > 0 && lc.atLineStart() {
			b = lc.lineBreak()
			broken = true
		}
		r.prefixed(item)
		breaks = append(breaks, b)
	}
	s.Breaks = nil
	if broken {
		s.Breaks = breaks
	}
}

// choice replays a choice inside a pragma, an attribute argument or an item of
// an Operators Rule.
func (r *layoutReplay) choice(e ast.Expr) {
	lc := r.lc
	lc.nest++
	defer func() { lc.nest-- }()
	c, ok := e.(*ast.Choice)
	if !ok {
		r.sequence(e)
		return
	}
	r.sequence(c.Alts[0])
	for _, alt := range c.Alts[1:] {
		sep := lc.takeCommentGap()
		lc.next() // /
		lead := lc.takeCommentGap()
		r.sequence(alt)
		lc.leadOf(alt, lead)
		if sep != nil {
			layoutOf(alt).Sep = sep
		}
	}
}

func (r *layoutReplay) sequence(e ast.Expr) {
	if s, ok := e.(*ast.Seq); ok {
		for i, item := range s.Items {
			var lead *ast.Gap
			if i > 0 {
				lead = r.lc.delimitedGap()
			}
			r.prefixed(item)
			r.lc.leadOf(item, lead)
		}
		return
	}
	r.prefixed(e)
}

func (r *layoutReplay) prefixed(e ast.Expr) {
	lc := r.lc
	gap := lc.takeInlineGap()
	switch x := e.(type) {
	case *ast.Capture:
		lc.next() // the label
		lc.next() // :
		r.operand(x.Expr)
	case *ast.And:
		lc.next()
		r.operand(x.Expr)
	case *ast.Not:
		lc.next()
		r.operand(x.Expr)
	case *ast.Discard:
		lc.next()
		r.operand(x.Expr)
	default:
		r.suffixed(e)
	}
	if gap != nil {
		layoutOf(e).Before = gap
	}
}

func (r *layoutReplay) operand(e ast.Expr) {
	lc := r.lc
	lead := lc.takeCommentGap()
	r.prefixed(e)
	lc.leadOf(e, lead)
}

// suffixed replays a primary expression and the suffixes, pragmas and
// attributes after it, from the innermost out.
func (r *layoutReplay) suffixed(e ast.Expr) {
	lc := r.lc
	var chain []ast.Expr
	for inner := e; ; {
		var next ast.Expr
		switch x := inner.(type) {
		case *ast.Repeat:
			next = x.Expr
		case *ast.Optional:
			next = x.Expr
		case *ast.Attributed:
			next = x.Expr
		case *ast.Atomic:
			next = x.Expr
		case *ast.Stream:
			next = x.Expr
		case *ast.Recovery:
			next = x.Expr
		}
		if next == nil {
			r.primary(inner)
			break
		}
		chain = append(chain, inner)
		inner = next
	}
	for i := len(chain) - 1; i >= 0; i-- {
		switch x := chain[i].(type) {
		case *ast.Attributed:
			r.attributed(x)
		case *ast.Optional:
			if gap := lc.takeCommentGap(); gap != nil {
				layoutOf(x).OpGap = gap
			}
			lc.next() // ?
		case *ast.Repeat:
			if !lc.is("#") {
				if gap := lc.takeCommentGap(); gap != nil {
					layoutOf(x).OpGap = gap
				}
				lc.next() // * or +
				break
			}
			r.pragma(x)
		default:
			r.pragma(x)
		}
	}
}

// pragma replays a pragma that wraps the expression before it, from the
// comments before its "#".
func (r *layoutReplay) pragma(e ast.Expr) {
	lc := r.lc
	operator := lc.takeInlineGap()
	var gap *ast.Gap
	if operator == nil {
		gap = lc.delimitedGap()
	}
	var br *ast.LineBreak
	if lc.nest == 0 && lc.atLineStart() {
		br = lc.lineBreak()
	}
	lc.next() // #
	lc.next() // the name
	switch x := e.(type) {
	case *ast.Atomic:
		x.Break = br
		r.emptyArguments()
	case *ast.Stream:
		x.Break = br
		r.emptyArguments()
	case *ast.Recovery:
		x.Break = br
		lc.next() // (
		open := lc.takeCommentGap()
		lc.next() // skip
		eq := lc.takeCommentGap()
		lc.next() // =
		lead := lc.takeCommentGap()
		r.choice(x.Skip)
		lc.leadOf(x.Skip, lead)
		closing := lc.takeCommentGap()
		lc.next() // )
		if open != nil || eq != nil || closing != nil {
			l := layoutOf(x)
			l.Open, l.Eq, l.Close = open, eq, closing
		}
	case *ast.Repeat:
		x.PragmaBreak = br
		x.Spelling = r.repeatSpelling()
	}
	if operator != nil {
		layoutOf(e).Operator = operator
	}
	if gap != nil {
		layoutOf(e).OpGap = gap
	}
}

// emptyArguments consumes the "()" that may follow #atomic or #stream.
func (r *layoutReplay) emptyArguments() {
	lc := r.lc
	if lc.is("(") && !lc.tok().spaceBefore {
		lc.next()
		lc.next()
	}
}

// repeatSpelling replays the arguments of #repeat, from its "(".
func (r *layoutReplay) repeatSpelling() *ast.RepeatSpelling {
	lc := r.lc
	lc.next() // (
	outer, stray := lc.cur, &ast.LineBreak{}
	lc.cur = stray
	sp := &ast.RepeatSpelling{}
	gap := func() { sp.Gaps = append(sp.Gaps, lc.takeGap()) }
	count := func() {
		if lc.is("-") {
			lc.next()
		}
		lc.next()
	}
	gap()
	first := lc.tok().kind == tInt || lc.is("-")
	if first {
		count()
		sp.HasMin = true
		gap()
	}
	if lc.is(",") {
		lc.next()
		sp.Comma = true
		gap()
		if !lc.is(")") {
			count()
			sp.HasMax = true
			gap()
		}
	}
	lc.next() // )
	lc.cur = outer
	if len(stray.Comments) > 0 {
		g := &sp.Gaps[len(sp.Gaps)-1]
		g.Comments = append(g.Comments, stray.Comments...)
	}
	return sp
}

// attributed replays the attributes and the #ignorecase and #missing pragmas
// of a, which follow its expression.
func (r *layoutReplay) attributed(a *ast.Attributed) {
	lc := r.lc
	a.Breaks = nil
	for i, attr := range a.Attrs {
		var gap []*ast.Comment
		var lead *ast.Gap
		if attr.Pragma {
			if gap = lc.takeInlineGap(); gap == nil {
				lead = lc.delimitedGap()
			}
		}
		var b *ast.LineBreak
		if lc.nest == 0 && lc.atLineStart() {
			b = lc.lineBreak()
		}
		if b != nil || a.Breaks != nil {
			for len(a.Breaks) < i {
				a.Breaks = append(a.Breaks, nil)
			}
			a.Breaks = append(a.Breaks, b)
		}
		if attr.Pragma {
			lc.next() // #
			lc.next() // the name
		} else {
			if b == nil {
				if gap = lc.takeInlineGap(); gap == nil {
					lead = lc.delimitedGap()
				}
			}
			r.attribute(attr)
		}
		if gap != nil || lead != nil {
			attr.Layout = &ast.SourceLayout{Before: gap, Lead: lead}
		}
	}
}

func (r *layoutReplay) primary(e ast.Expr) {
	lc := r.lc
	switch x := e.(type) {
	case *ast.Literal:
		lc.next()
		if x.IgnoreCase {
			lc.next() // i
		}
	case *ast.Ref:
		if x.Level != "" {
			var gaps ast.LevelGaps
			lc.next() // level
			lc.next() // (
			gaps.Open = lc.takeCommentGap()
			lc.next() // rule
			gaps.Before = lc.takeCommentGap()
			lc.next() // ,
			gaps.After = lc.takeCommentGap()
			lc.next() // name
			gaps.Close = lc.takeCommentGap()
			lc.next() // )
			x.LevelGaps = levelGaps(gaps)
			return
		}
		lc.next()
	case *ast.Cut:
		lc.next() // #
		lc.next() // cut
	case *ast.Group:
		r.group(x)
	case *ast.Predicate:
		x.OpenBreak, x.Breaks, x.CloseBreak, x.OneLine = r.actionBlock(x.Action)
		if b, ok := x.Action.(*ast.Block); ok {
			b.OpenBreak, b.Breaks, b.CloseBreak, b.OneLine = x.OpenBreak, x.Breaks, x.CloseBreak, x.OneLine
		}
	default:
		lc.next() // a class, _, ^^, ^, $$, $, _|_ or .
	}
}

func (r *layoutReplay) group(g *ast.Group) {
	lc := r.lc
	open := lc.i
	lc.next() // (
	lead := lc.openLead()
	g.BodyBreak = lc.lineBreak()
	lc.leadBreak(g.BodyBreak, lead)
	if a, ok := g.Expr.(*ast.InlineAction); ok {
		r.bodyChoice(a.Expr)
		gap := lc.takeCommentGap()
		lc.next() // =>
		r.action(a.Action, a.DefaultAction)
		if gap != nil {
			layoutOf(a).OpGap = gap
		}
	} else {
		r.bodyChoice(g.Expr)
	}
	last := lc.toks[lc.i].lineComment
	if last != nil {
		last = lc.toks[lc.i].takeRun(last)
	}
	lc.toks[lc.i].lineComment = nil
	g.CloseBreak = lc.lineBreak()
	g.CloseBreak.Last = last
	g.OneLine = !lc.multiline(open, lc.i)
	lc.next() // )
}

// attributeValue replays an attribute argument v, a value or an expression.
func (r *layoutReplay) attributeValue(v any) {
	lc := r.lc
	switch v := v.(type) {
	case ast.Expr:
		r.choice(v)
	case string:
		// A literal, which choice reads as an expression.
		lc.nest++
		lc.takeInlineGap()
		lc.next()
		if lc.is("i") && !lc.tok().spaceBefore && lc.tok().kind == tIdent {
			lc.next()
		}
		lc.nest--
	default:
		if lc.is("-") {
			lc.next()
		}
		lc.next() // nil, true, false or an integer
	}
}

// action replays the action of a rule, an inline action or an item of an
// Operators Rule, from its first token.
func (r *layoutReplay) action(t ast.ActionExpr, defaultAction bool) {
	lc := r.lc
	b, block := t.(*ast.Block)
	switch {
	case defaultAction:
		lc.next() // ...
	case block:
		b.OpenBreak, b.Breaks, b.CloseBreak, b.OneLine = r.actionBlock(b)
	default:
		r.actionExpr(t)
	}
}

// actionBlock replays the braces and statements of a block. t is the block, or
// its one statement, which a predicate holds instead (a statement never starts
// with "{", so a Block is always the block itself).
func (r *layoutReplay) actionBlock(t ast.ActionExpr) (openBreak *ast.LineBreak, breaks []*ast.LineBreak, closeBreak *ast.LineBreak, oneLine bool) {
	lc := r.lc
	statements := []ast.ActionExpr{t}
	if b, ok := t.(*ast.Block); ok {
		statements = b.Statements
	}
	open := lc.i
	outer := lc.cur
	defer func() { lc.cur = outer }()
	lc.next() // {
	lead := lc.openLead()
	openBreak = &ast.LineBreak{}
	lc.cur = openBreak
	joined := false
	for i := 0; ; i++ {
		var gap []*ast.Comment
		var gapLine int
		if joined && !lc.is("}") {
			if gap = lc.takeInlineGap(); gap != nil {
				gapLine = lc.toks[lc.i-1].end.Line
			}
		}
		br := lc.lineBreak()
		lc.leadBreak(br, lead)
		lead = nil
		if i == len(statements) {
			oneLine = !lc.multiline(open, lc.i)
			lc.next() // }
			return openBreak, breaks, br, oneLine
		}
		expr := statements[i]
		if b, ok := expr.(*ast.Bind); ok {
			lc.next() // the name
			if gap := lc.takeCommentGap(); gap != nil {
				layoutOf(b).OpGap = gap
			}
			lc.next() // :=
			r.actionExpr(b.Value)
		} else {
			r.actionExpr(expr)
		}
		if gap != nil {
			l := layoutOf(expr)
			l.Before, l.PreviousLine, l.NextLine = gap, gapLine, gapLine
		}
		breaks = append(breaks, br)
		if lc.is(";") {
			if gap := lc.takeInlineGap(); gap != nil {
				layoutOf(expr).After = gap
			}
		}
		joined = lc.separator()
	}
}

func (r *layoutReplay) actionExpr(t ast.ActionExpr) {
	if l, ok := t.(*ast.Lambda); ok {
		r.lambda(l)
		return
	}
	r.binary(0, t)
}

func (r *layoutReplay) lambda(l *ast.Lambda) {
	lc := r.lc
	layout := lc.takeActionLayout()
	lc.next() // (
	cur := lc.cur
	list := &ast.ListLayout{}
	list.OpenBreak, list.BodyBreak = lc.openList()
	for i := range l.Params {
		var elem *ast.SourceLayout
		if i > 0 {
			elem = lc.takeActionLayout()
		}
		lc.next() // the parameter
		lc.takeAfter(&elem)
		if i+1 == len(l.Params) {
			l.TrailingComma = lc.is(",")
		}
		if i+1 < len(l.Params) || l.TrailingComma {
			lc.next() // ,
			lc.afterCommaLayout(&elem, ")")
		}
		list.Elems = append(list.Elems, elem)
	}
	list.CloseBreak = lc.lineBreak()
	lc.next() // )
	lc.cur = cur
	if gap := lc.takeCommentGap(); gap != nil {
		if layout == nil {
			layout = &ast.SourceLayout{}
		}
		layout.OpGap = gap
	}
	lc.next() // =>
	r.actionExpr(l.Body)
	l.ParamsLayout = list
	if layout != nil {
		l.Layout = layout
	}
}

// binaryLevels are the binary operators of actions by level of precedence,
// loosest first, as the parser reads them and the replay walks them.
var binaryLevels = [][]string{
	{"||"},
	{"&&"},
	{"==", "!=", "<", "<=", ">", ">="},
	{"+", "-"},
	{"*", "/", "%"},
}

// binary replays t as an operand of the operators of level and above.
func (r *layoutReplay) binary(level int, t ast.ActionExpr) {
	lc := r.lc
	if level == len(binaryLevels) {
		r.unary(t)
		return
	}
	var chain []*ast.Binary
	left := t
	for {
		b, ok := left.(*ast.Binary)
		if !ok || !slices.Contains(binaryLevels[level], b.Op) {
			break
		}
		chain = append(chain, b)
		left = b.L
	}
	r.binary(level+1, left)
	for i := len(chain) - 1; i >= 0; i-- {
		b := chain[i]
		operator := lc.takeCommentGap()
		lc.next() // the operator
		r.binary(level+1, b.R)
		if operator != nil {
			layoutOf(b).OpGap = operator
		}
	}
}

func (r *layoutReplay) unary(t ast.ActionExpr) {
	lc := r.lc
	layout := lc.takeActionLayout()
	if u, ok := t.(*ast.Unary); ok {
		lc.next() // - or !
		r.unary(u.X)
	} else {
		var chain []ast.ActionExpr
		inner := t
		for {
			var x ast.ActionExpr
			switch n := inner.(type) {
			case *ast.Binary:
				if n.Op == "[]" {
					x = n.L
				}
			case *ast.Cast:
				x = n.X
			case *ast.FieldAccess:
				x = n.X
			}
			if x == nil {
				break
			}
			chain = append(chain, inner)
			inner = x
		}
		r.actionExprPrimary(inner)
		for i := len(chain) - 1; i >= 0; i-- {
			lc.next() // . or [
			switch n := chain[i].(type) {
			case *ast.Binary:
				outer := lc.cur
				n.OpenBreak = &ast.LineBreak{}
				lc.cur = n.OpenBreak
				n.BodyBreak = lc.lineBreak()
				r.actionExpr(n.R)
				n.CloseBreak = lc.lineBreak()
				lc.next() // ]
				lc.cur = outer
			case *ast.Cast:
				lc.next() // (
				r.typ(n.Type)
				lc.next() // )
			case *ast.FieldAccess:
				lc.next() // the name
			}
		}
	}
	if layout != nil {
		s := t.(ast.SourceNode).Source()
		if old := s.Layout; old != nil {
			old.Before, old.PreviousLine, old.NextLine = layout.Before, layout.PreviousLine, layout.NextLine
			layout = old
		}
		s.Layout = layout
	}
}

func (r *layoutReplay) actionExprPrimary(t ast.ActionExpr) {
	lc := r.lc
	switch x := t.(type) {
	case *ast.Constructor:
		r.newActionExpr(x)
	case *ast.Call:
		lc.next() // the name
		lc.next() // (
		defer lc.restoreBreak()()
		x.OpenBreak, x.BodyBreak = lc.openList()
		x.TrailingComma = r.listItems(x.Args, ")")
		x.CloseBreak = lc.lineBreak()
		lc.next() // )
	case *ast.ListConstructor:
		lc.next() // []
		r.typ(x.Elem)
		lc.next() // {
		defer lc.restoreBreak()()
		x.OpenBreak, x.BodyBreak = lc.openList()
		x.TrailingComma = r.listItems(x.Items, "}")
		x.CloseBreak = lc.lineBreak()
		lc.next() // }
	case *ast.ActionGroup:
		open := lc.i
		lc.next() // (
		outer := lc.cur
		defer func() { lc.cur = outer }()
		x.OpenBreak = &ast.LineBreak{}
		lc.cur = x.OpenBreak
		x.BodyBreak = lc.lineBreak()
		r.actionExpr(x.Action)
		x.CloseBreak = lc.lineBreak()
		x.OneLine = !lc.multiline(open, lc.i)
		lc.next() // )
	case *ast.TupleLit:
		lc.next() // (
		outer := lc.cur
		defer func() { lc.cur = outer }()
		x.OpenBreak = &ast.LineBreak{}
		lc.cur = x.OpenBreak
		x.BodyBreak = lc.lineBreak()
		r.tupleLit(x)
	default:
		lc.next() // a literal, a name or a capture
	}
}

// tupleLit replays the elements and closing parenthesis of t.
func (r *layoutReplay) tupleLit(t *ast.TupleLit) {
	lc := r.lc
	t.TrailingComma = false
	if len(t.Elems) > 0 {
		r.actionExpr(t.Elems[0])
		lc.takeAfterLayout(t.Elems[0])
		for i := 1; lc.is(","); i++ {
			lc.next()
			lc.afterCommaLayout(&t.Elems[i-1].(ast.SourceNode).Source().Layout, ")")
			if lc.is(")") {
				t.TrailingComma = true
				break
			}
			r.actionExpr(t.Elems[i])
			lc.takeAfterLayout(t.Elems[i])
		}
	}
	t.CloseBreak = lc.lineBreak()
	lc.next() // )
}

func (r *layoutReplay) newActionExpr(n *ast.Constructor) {
	lc := r.lc
	lc.next() // the type
	lc.next() // {
	defer lc.restoreBreak()()
	n.OpenBreak, n.BodyBreak = lc.openList()
	trailing := false
	if !lc.is("}") && !(lc.tok().kind == tIdent && lc.peek(1).text == ":") {
		trailing = r.listItems(n.Args, "}")
	} else {
		for _, field := range n.Fields {
			layout := lc.takeActionLayout()
			lc.next() // the name
			lc.next() // :
			r.actionExpr(field.Value)
			field.Layout = layout
			lc.takeAfterLayout(field)
			trailing = false
			if !lc.is("}") {
				lc.next() // ,
				lc.afterCommaLayout(&field.Layout, "}")
				trailing = lc.is("}")
			}
		}
	}
	n.CloseBreak, n.TrailingComma = lc.lineBreak(), trailing
	lc.next() // }
}

// listItems replays the comma-separated items up to closer, and reports
// whether a trailing comma follows them.
func (r *layoutReplay) listItems(items []ast.ActionExpr, closer string) (trailing bool) {
	lc := r.lc
	for _, item := range items {
		r.actionExpr(item)
		lc.takeAfterLayout(item)
		trailing = false
		if !lc.is(closer) {
			lc.next() // ,
			lc.afterCommaLayout(&item.(ast.SourceNode).Source().Layout, closer)
			trailing = lc.is(closer)
		}
	}
	return trailing
}
