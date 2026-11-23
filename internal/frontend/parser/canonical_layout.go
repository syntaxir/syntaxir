package parser

import (
	"slices"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The raw formatter state of a grammar (the comment tokens and line breaks
// that LineBreak and SourceLayout values keep, see ast.LineBreak) depends
// on where the hand-written parser records them: at which token it starts a
// line break, takes the comments of a gap for a node, or leaves them to the
// line break before. Every comment of the source goes to exactly one of them.
//
// layoutReplay rebuilds that state for a grammar that the canonical frontend
// built: it reads the tokens of the source with the lexer and walks the
// grammar in source order, calling the primitives of layoutCursor, with which
// the parser reads its tokens, at the token where the parser calls them. The
// grammar decides each branch the parser takes by looking ahead, so the replay
// needs no lookahead of its own; it only finds the tokens the nodes start and
// end at.
type layoutReplay struct {
	lc *layoutCursor
	// starts and ends give the index of the token that starts or ends at a
	// position.
	starts, ends map[ast.Pos]int
}

// replayLayout sets the raw formatter state of g, which the canonical
// frontend built from the source of the tokens toks, as Parse sets it.
func replayLayout(toks []token, g *ast.Grammar) {
	r := &layoutReplay{lc: &layoutCursor{toks: toks}, starts: make(map[ast.Pos]int, len(toks)), ends: make(map[ast.Pos]int, len(toks))}
	for i, t := range r.lc.toks {
		r.starts[t.pos] = i
		r.ends[t.end] = i
	}
	r.file(g)
}

// seek consumes the tokens before token i, as the parser does with next.
func (r *layoutReplay) seek(i int) {
	for r.lc.i < i && r.lc.tok().kind != tEOF {
		r.lc.next()
	}
}

// seekStart consumes the tokens before the token at pos.
func (r *layoutReplay) seekStart(pos ast.Pos) {
	if i, ok := r.starts[pos]; ok {
		r.seek(i)
	}
}

// seekText consumes the tokens before the next one that is the punctuation
// or name text, outside the brackets that open after the current token.
func (r *layoutReplay) seekText(text string) {
	lc := r.lc
	depth := 0
	for lc.tok().kind != tEOF {
		t := lc.tok()
		if depth == 0 && (t.kind == tPunct || t.kind == tIdent) && t.text == text {
			return
		}
		if t.kind == tPunct {
			switch t.text {
			case "(", "[", "{", "[]":
				if t.text != "[]" {
					depth++
				}
			case ")", "]", "}":
				depth--
			}
		}
		lc.next()
	}
}

// header runs read with the parser in a statement header at the token at pos:
// the comments it consumes are header comments.
func (r *layoutReplay) header(pos ast.Pos, read func()) {
	lc := r.lc
	header, headerPos := lc.header, lc.headerPos
	lc.header, lc.headerPos = true, pos
	read()
	lc.header, lc.headerPos = header, headerPos
}

func (r *layoutReplay) file(g *ast.Grammar) {
	lc := r.lc
	if pkg := &g.File.Pkg; len(pkg.Attrs) > 0 || lc.is("package") {
		pkg.Break = lc.statementBreak()
		pkg.AttrBreaks, pkg.KeywordBreak = r.attributes(pkg.Attrs)
		pos := lc.next().pos
		r.header(pos, func() { lc.next() })
		r.separator()
	}
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ast.TypeDef:
			r.typeDef(d)
		case *ast.TypeAlias:
			r.typeAlias(d)
		case *ast.VarDef:
			r.varDef(d)
		case *ast.RuleDef:
			r.ruleDef(d)
		}
		r.separator()
	}
	r.seek(len(lc.toks) - 1)
	g.File.EndBreak = lc.lineBreak()
	g.File.HeaderComments = lc.headerComments
}

// separator consumes the semicolon that ends a statement on its line.
func (r *layoutReplay) separator() {
	r.lc.separator()
}

func (r *layoutReplay) typeDef(d *ast.TypeDef) {
	lc := r.lc
	r.seekStart(d.Pos)
	d.Break = lc.statementBreak()
	lc.next() // type
	r.header(d.Pos, func() {
		lc.next() // the name
		switch t := d.Type.(type) {
		case *ast.StructType:
			lc.next() // =
			lc.header = false
			gap := lc.typeBefore()
			t.CloseBreak, t.OneLine = r.structFields(t)
			setTypeBefore(t, gap)
		case *ast.TypeApp:
			lc.next() // =
			lc.header = false
			r.typeOperand(t)
		}
	})
}

func (r *layoutReplay) typeAlias(d *ast.TypeAlias) {
	lc := r.lc
	r.seekStart(d.Pos)
	d.Break = lc.statementBreak()
	lc.next() // type
	r.header(d.Pos, func() {
		lc.next() // the name
		lc.next() // ~
		lc.header = false
		r.typeOperand(d.Type)
	})
}

func (r *layoutReplay) varDef(d *ast.VarDef) {
	lc := r.lc
	r.seekStart(d.Pos)
	d.Break = lc.statementBreak()
	lc.next() // var
	r.header(d.Pos, func() { lc.next() })
	if d.Type != nil {
		lc.headerBefore(d.Pos)
		r.typ(d.Type)
	}
	if d.Value != nil {
		lc.headerBefore(d.Pos)
		lc.next() // =
		r.actionExpr(d.Value)
	}
}

func (r *layoutReplay) ruleDef(d *ast.RuleDef) {
	lc := r.lc
	if len(d.Attrs) > 0 {
		r.seekStart(d.Attrs[0].Pos)
	} else {
		r.seekStart(d.Pos)
	}
	d.Break = lc.statementBreak()
	d.AttrBreaks, d.KeywordBreak = r.attributes(d.Attrs)
	pos := lc.next().pos // rule
	r.header(pos, func() {
		lc.next() // the name
		if d.Type != nil {
			r.typ(d.Type)
		}
		r.seekText("=")
		lc.next()
	})
	// An Operators Rule body starts on the line of "=", and takes no line
	// break before it or its action.
	operators := !lc.tok().lineBefore && lc.is("#") && lc.peek(1).kind == tIdent && lc.peek(1).text == "operators"
	if !operators && lc.tok().lineBefore {
		d.BodyBreak = lc.lineBreak()
	}
	if operators {
		r.operatorsBody(d.Expr)
	} else {
		r.bodyChoice(d.Expr)
	}
	if d.Action != nil || d.DefaultAction {
		if lc.tok().lineBefore {
			d.ActionBreak = lc.lineBreak()
		} else if !operators {
			lc.afterGap(d.Expr)
		}
		lc.next() // =>
		r.action(d.Action, d.DefaultAction)
	}
}

// attributes replays the declaration attributes of a rule or the package
// clause, and returns their line breaks and the line break before the
// keyword that follows them.
func (r *layoutReplay) attributes(attrs []*ast.Attribute) (breaks []*ast.LineBreak, keywordBreak *ast.LineBreak) {
	lc := r.lc
	if len(attrs) == 0 {
		return nil, nil
	}
	for i, a := range attrs {
		var b *ast.LineBreak
		var gap []*ast.Comment
		if i > 0 {
			if lc.atLineStart() {
				b = lc.lineBreak()
			} else {
				gap = lc.takeInlineGap()
			}
		}
		if b != nil || breaks != nil {
			for len(breaks) < i {
				breaks = append(breaks, nil)
			}
			breaks = append(breaks, b)
		}
		r.attribute(a)
		if gap != nil {
			if a.Layout == nil {
				a.Layout = &ast.SourceLayout{}
			}
			a.Layout.Before = gap
		}
	}
	return breaks, lc.lineBreak()
}

func (r *layoutReplay) attribute(a *ast.Attribute) {
	lc := r.lc
	lc.next() // @
	lc.next() // the name
	if !lc.is("(") || lc.tok().spaceBefore {
		return
	}
	lc.next()
	defer lc.restoreBreak()()
	list := &ast.ListLayout{}
	list.OpenBreak, list.BodyBreak = lc.openList()
	a.ArgsLayout = list
	if lc.is(")") {
		list.CloseBreak = lc.lineBreak()
		lc.next()
		return
	}
	values := append([]any(nil), a.Args...)
	for _, k := range a.KeywordOrder {
		values = append(values, a.KwArgs[k])
	}
	for i, v := range values {
		var layout *ast.SourceLayout
		if len(list.Elems) > 0 {
			layout = lc.takeActionLayout()
		}
		if i >= len(a.Args) {
			lc.next() // the name
			lc.next() // =
		}
		r.attributeValue(v)
		list.Elems = append(list.Elems, layout)
		last := &list.Elems[len(list.Elems)-1]
		lc.takeAfter(last)
		if lc.is(")") {
			break
		}
		lc.next() // ,
		lc.afterCommaLayout(last, ")")
		if lc.is(")") {
			a.TrailingComma = true
			break
		}
	}
	list.CloseBreak = lc.lineBreak()
	lc.next() // )
}

// typ replays the type t, which starts at the current token.
func (r *layoutReplay) typ(t ast.TypeExpr) {
	lc := r.lc
	switch t := t.(type) {
	case *ast.JoinType:
		for i, u := range t.Types {
			if i > 0 {
				lc.typeAfter(t.Types[i-1], "|")
				lc.next() // |
				r.typeRight(u)
				continue
			}
			r.typ(u)
		}
		lc.typeAfter(t.Types[len(t.Types)-1], "|")
	case *ast.MeetType:
		for i, u := range t.Types {
			if i > 0 {
				lc.typeAfter(t.Types[i-1], "&")
				lc.next() // &
				r.typeRight(u)
				continue
			}
			r.typ(u)
		}
		lc.typeAfter(t.Types[len(t.Types)-1], "&")
	case *ast.ListType:
		lc.next() // []
		r.typeOperand(t.Elem)
	case *ast.OptionalType:
		lc.next() // *
		r.typeOperand(t.Elem)
	case *ast.TypeGroup:
		open := lc.i
		lc.next() // (
		outer, header := lc.cur, lc.header
		defer func() { lc.cur, lc.header = outer, header }()
		lc.header = false
		t.OpenBreak = &ast.LineBreak{}
		lc.cur = t.OpenBreak
		t.BodyBreak = lc.lineBreak()
		r.typ(t.Type)
		t.CloseBreak = lc.lineBreak()
		t.OneLine = !lc.multiline(open, lc.i)
		lc.next() // )
	case *ast.TupleType:
		lc.next() // (
		outer, header := lc.cur, lc.header
		defer func() { lc.cur, lc.header = outer, header }()
		lc.header = false
		t.OpenBreak = &ast.LineBreak{}
		lc.cur = t.OpenBreak
		t.BodyBreak = lc.lineBreak()
		for i, u := range t.Types {
			if i > 0 {
				u.(ast.SourceNode).Source().Layout = lc.takeActionLayout()
			}
			r.typ(u)
			lc.takeAfterLayout(u)
			if lc.is(",") {
				lc.next()
				lc.afterCommaLayout(&u.(ast.SourceNode).Source().Layout, ")")
				t.TrailingComma = lc.is(")")
			}
		}
		t.CloseBreak = lc.lineBreak()
		lc.next() // )
	case *ast.StructType:
		outer, header := lc.cur, lc.header
		defer func() { lc.cur, lc.header = outer, header }()
		lc.header = false
		t.CloseBreak, t.OneLine = r.structFields(t)
	case *ast.TypeApp:
		lc.next() // the name
		lc.next() // [
		outer, header := lc.cur, lc.header
		defer func() { lc.cur, lc.header = outer, header }()
		lc.header = false
		t.OpenBreak, t.BodyBreak = lc.openList()
		for _, u := range t.Args {
			layout := lc.takeActionLayout()
			r.typ(u)
			if layout != nil {
				u.(ast.SourceNode).Source().Layout = layout
			}
			lc.takeAfterLayout(u)
			if lc.is(",") {
				lc.next()
				lc.afterCommaLayout(&u.(ast.SourceNode).Source().Layout, "]")
				t.TrailingComma = lc.is("]")
			}
		}
		t.CloseBreak = lc.lineBreak()
		lc.next() // ]
	default:
		lc.next() // a name or _|_
	}
}

// typeRight replays t as the parser's typeRight reads it.
func (r *layoutReplay) typeRight(t ast.TypeExpr) {
	gap := r.lc.typeLead()
	r.typ(t)
	setTypeLead(t, gap)
}

// typeOperand replays t as the parser's typeOperand reads it.
func (r *layoutReplay) typeOperand(t ast.TypeExpr) {
	gap := r.lc.typeBefore()
	r.typ(t)
	setTypeBefore(t, gap)
}

// structFields replays the braces and fields of the struct type t.
func (r *layoutReplay) structFields(t *ast.StructType) (closeBreak *ast.LineBreak, oneLine bool) {
	lc := r.lc
	open := lc.i
	lc.next() // {
	lead := lc.openLead()
	for _, f := range t.Fields {
		f.Break = lc.statementBreak()
		lc.leadBreak(f.Break, lead)
		lead = nil
		lc.next() // the name
		_, f.HeaderTokens = lc.takeHeaderGap()
		r.typ(f.Type)
		r.separator()
	}
	closeBreak = lc.statementBreak()
	lc.leadBreak(closeBreak, lead)
	oneLine = !lc.multiline(open, lc.i)
	lc.next() // }
	return closeBreak, oneLine
}

// operatorsBody replays the Operators Rule that e is, or that e holds with the
// #ignorecase pragmas after it, from its #operators.
func (r *layoutReplay) operatorsBody(e ast.Expr) {
	lc := r.lc
	body := e
	var pragmas []*ast.Attribute
	for {
		a, ok := e.(*ast.Attributed)
		if !ok {
			break
		}
		pragmas = append(a.Attrs, pragmas...)
		e = a.Expr
	}
	pr, ok := e.(*ast.OperatorsRule)
	if !ok {
		return
	}
	before := lc.takeInlineGap()
	lc.next() // #
	open := lc.takeInlineGap()
	lc.next() // operators
	open = append(open, lc.takeInlineGap()...)
	setOperatorsLayout(body, before, open)
	r.operatorsRule(pr)
	for _, attr := range pragmas {
		if !lc.is("#") || lc.tok().lineBefore {
			break
		}
		if gap := lc.takeInlineGap(); gap != nil {
			attr.Layout = &ast.SourceLayout{Before: gap}
		}
		lc.next() // #
		lc.next() // ignorecase
	}
}

// operatorsRule replays the braces and items of pr, from its "{".
func (r *layoutReplay) operatorsRule(pr *ast.OperatorsRule) {
	lc := r.lc
	type item struct {
		pos  ast.Pos
		read func(b *ast.LineBreak)
	}
	var items []item
	if pr.Skip != nil {
		items = append(items, item{pr.SkipSource.Pos, func(b *ast.LineBreak) {
			pr.SkipBreak = b
			lc.next() // skip
			r.choice(pr.Skip)
			lc.endLine(b)
		}})
	}
	for _, o := range pr.Operands {
		items = append(items, item{o.Pos, func(b *ast.LineBreak) {
			o.Break = b
			lc.next() // operand
			r.choice(o.Expr)
			if lc.is("=>") {
				lc.afterGap(o.Expr)
				lc.next()
				r.action(o.Action, o.DefaultAction)
			}
			lc.endLine(b)
		}})
	}
	for _, l := range pr.Levels {
		items = append(items, item{l.Pos, func(b *ast.LineBreak) {
			l.Break = b
			r.level(l)
		}})
	}
	slices.SortFunc(items, func(a, b item) int {
		if a.pos.Line != b.pos.Line {
			return a.pos.Line - b.pos.Line
		}
		return a.pos.Col - b.pos.Col
	})
	lc.next() // {
	lc.leadAfterOpen()
	for _, it := range items {
		b := lc.statementBreak()
		it.read(b)
		r.separator()
	}
	pr.CloseBreak = lc.statementBreak()
	lc.next() // }
}

// level replays a level of an Operators Rule, from its keyword.
func (r *layoutReplay) level(l *ast.OperatorsRuleLevel) {
	lc := r.lc
	pos := lc.next().pos // level
	open := 0
	r.header(pos, func() {
		if lc.tok().kind == tIdent {
			lc.next()
		}
		open = lc.i
		lc.next() // {
	})
	lc.leadAfterOpen()
	for _, op := range l.Operators {
		op.Break = lc.statementBreak()
		start := lc.next() // prefix, postfix or infix
		if start.text == "infix" {
			r.header(start.pos, func() {
				if lc.is("left") || lc.is("right") || lc.is("none") {
					lc.next()
				}
			})
		}
		r.choice(op.Expr)
		if lc.is("=>") {
			lc.afterGap(op.Expr)
			lc.next()
			r.action(op.Action, op.DefaultAction)
		}
		lc.endLine(op.Break)
		r.separator()
	}
	l.CloseBreak = lc.statementBreak()
	l.OneLine = !lc.multiline(open, lc.i)
	lc.next() // }
}
