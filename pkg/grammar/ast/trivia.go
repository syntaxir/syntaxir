package ast

import (
	"strconv"
	"unicode/utf8"
)

// Comments and line breaks are not part of a grammar's meaning, but a source
// formatter has to keep them. The parser records them in LineBreak values
// attached to the nodes that Format may start a new line with: statements,
// struct fields, the items of an Operators Rule, the closing braces of
// blocks, and the optional line breaks inside a rule body. These fields are
// tagged `json:"-"`, so the JSON form of a grammar does not contain them.

// Comment is a line comment (// ...) or nested block comment (/* ... */).
type Comment struct {
	// Header identifies a statement header that illegally contains this comment.
	Header Pos
	Pos    Pos
	// End is the source position just after the comment.
	End Pos
	// Text includes delimiters. Line comments exclude their final line
	// break and trailing white space; block comments preserve their contents.
	Text string
	// Content is marker-free documentation text, including original line
	// endings and whitespace. It is independent of formatter spelling.
	Content string
	// Blank reports whether a blank line precedes the comment.
	Blank bool
	// Run lists the comments that this one stands for when several comments end
	// the same line, with Text and Content holding all of them: the formatter
	// writes them together, as they were. AllComments returns them separately.
	Run []*Comment
}

// LineBreak describes the start of a line in SyntaxIR source: the comments
// before it and the comment at its end.
type LineBreak struct {
	// Comments are the comments on their own lines before the node, in
	// source order. Comments that Format cannot keep in place, because it
	// joins the lines they end, are added to the nearest preceding
	// LineBreak.
	Comments []*Comment
	// Blank reports whether a blank line directly precedes the node.
	Blank bool
	// Trailing is the comment at the end of the line that starts with the
	// node, if any. When several comments end the line, it stands for all of
	// them (see Comment.Run).
	Trailing *Comment
	// TrailingEnds reports whether a line break follows Trailing in the source
	// before the next token, rather than that token following it on the same
	// line. A block comment that ends its line cannot move to the line of the
	// token after it without changing which node owns it.
	TrailingEnds bool
	// Last is the comment on the line of the last token before the closing
	// parenthesis of a group, which trails that token: Trailing, of the break
	// before a ")", is the comment after the ")".
	Last *Comment
}

// AllComments returns every comment kept in g, in no particular order.
func (g *Grammar) AllComments() []*Comment {
	return g.allComments(nil)
}

func (g *Grammar) allComments(control *metadataControl) []*Comment {
	var cs []*Comment
	add := func(b *LineBreak) {
		control.poll()
		if b == nil {
			return
		}
		cs = appendMetadata(cs, b.Comments, control)
		if b.Trailing != nil {
			cs = append(cs, b.Trailing)
		}
		if b.Last != nil {
			cs = append(cs, b.Last)
		}
	}
	addAll := func(bs []*LineBreak) {
		for _, b := range bs {
			control.poll()
			add(b)
		}
	}
	addLayout := func(l *SourceLayout) {
		if l != nil {
			cs = appendMetadata(cs, l.Before, control)
			cs = appendMetadata(cs, l.After, control)
			cs = appendMetadata(cs, l.Trail, control)
			cs = appendMetadata(cs, l.Operator, control)
			for _, g := range []*Gap{l.Open, l.Eq, l.Close, l.OpGap, l.Sep, l.Lead} {
				if g != nil {
					cs = appendMetadata(cs, g.Comments, control)
				}
			}
		}
	}
	addList := func(l *ListLayout) {
		if l == nil {
			return
		}
		add(l.OpenBreak)
		add(l.BodyBreak)
		add(l.CloseBreak)
		for _, e := range l.Elems {
			control.poll()
			addLayout(e)
		}
	}
	var actionExpr func(ActionExpr)
	var typ func(TypeExpr)
	actionExpr = func(t ActionExpr) {
		control.poll()
		if n, ok := t.(SourceNode); ok {
			addLayout(n.Source().Layout)
		}
		switch t := t.(type) {
		case *ActionGroup:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			actionExpr(t.Action)
		case *Block:
			add(t.OpenBreak)
			addAll(t.Breaks)
			add(t.CloseBreak)
			for _, stmt := range t.Statements {
				actionExpr(stmt)
			}
		case *Constructor:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			for _, field := range t.Fields {
				control.poll()
				if field.Layout != nil {
					cs = appendMetadata(cs, field.Layout.Before, control)
					cs = appendMetadata(cs, field.Layout.After, control)
					cs = appendMetadata(cs, field.Layout.Trail, control)
				}
				actionExpr(field.Value)
			}
			for _, x := range t.Args {
				actionExpr(x)
			}
		case *TupleLit:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			for _, x := range t.Elems {
				actionExpr(x)
			}
		case *ListConstructor:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			typ(t.Elem)
			for _, item := range t.Items {
				actionExpr(item)
			}
		case *Call:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			for _, arg := range t.Args {
				actionExpr(arg)
			}
		case *Lambda:
			addList(t.ParamsLayout)
			actionExpr(t.Body)
		case *Binary:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			actionExpr(t.L)
			actionExpr(t.R)
		case *Unary:
			actionExpr(t.X)
		case *Cast:
			actionExpr(t.X)
			typ(t.Type)
		case *Bind:
			actionExpr(t.Value)
		case *FieldAccess:
			actionExpr(t.X)
		}
	}
	typ = func(t TypeExpr) {
		control.poll()
		if n, ok := t.(SourceNode); ok {
			addLayout(n.Source().Layout)
		}
		switch t := t.(type) {
		case *TypeGroup:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			typ(t.Type)
		case *ListType:
			typ(t.Elem)
		case *OptionalType:
			typ(t.Elem)
		case *JoinType:
			for _, t := range t.Types {
				typ(t)
			}
		case *MeetType:
			for _, t := range t.Types {
				typ(t)
			}
		case *TupleType:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			for _, t := range t.Types {
				typ(t)
			}
		case *TypeApp:
			add(t.OpenBreak)
			add(t.BodyBreak)
			add(t.CloseBreak)
			for _, t := range t.Args {
				typ(t)
			}
		case *StructType:
			add(t.CloseBreak)
			for _, f := range t.Fields {
				add(f.Break)
				cs = appendMetadata(cs, f.HeaderTokens, control)
				typ(f.Type)
			}
		}
	}
	var expr func(e Expr)
	expr = func(e Expr) {
		control.poll()
		if n, ok := e.(SourceNode); ok {
			addLayout(n.Source().Layout)
		}
		switch e := e.(type) {
		case *Group:
			add(e.BodyBreak)
			add(e.CloseBreak)
			expr(e.Expr)
		case *Choice:
			addAll(e.Breaks)
			for _, a := range e.Alts {
				expr(a)
			}
		case *Seq:
			addAll(e.Breaks)
			for _, it := range e.Items {
				expr(it)
			}
		case *Attributed:
			addAll(e.Breaks)
			expr(e.Expr)
			for _, attr := range e.Attrs {
				control.poll()
				addLayout(attr.Layout)
				addList(attr.ArgsLayout)
				for _, arg := range attr.expressionArgs(control) {
					expr(arg)
				}
			}
		case *InlineAction:
			expr(e.Expr)
			actionExpr(e.Action)
		case *Ref:
			if g := e.LevelGaps; g != nil {
				for _, gap := range []*Gap{g.Open, g.Before, g.After, g.Close} {
					if gap != nil {
						cs = appendMetadata(cs, gap.Comments, control)
					}
				}
			}
		case *Predicate:
			if _, block := e.Action.(*Block); !block {
				add(e.OpenBreak)
				addAll(e.Breaks)
				add(e.CloseBreak)
			}
			actionExpr(e.Action)
		case *Repeat:
			add(e.PragmaBreak)
			if e.Spelling != nil {
				for _, g := range e.Spelling.Gaps {
					control.poll()
					cs = appendMetadata(cs, g.Comments, control)
				}
			}
			expr(e.Expr)
		case *Optional:
			expr(e.Expr)
		case *Stream:
			add(e.Break)
			expr(e.Expr)
		case *Recovery:
			add(e.Break)
			expr(e.Expr)
			expr(e.Skip)
		case *Capture:
			expr(e.Expr)
		case *And:
			expr(e.Expr)
		case *Not:
			expr(e.Expr)
		case *Atomic:
			add(e.Break)
			expr(e.Expr)
		case *Discard:
			expr(e.Expr)
		case *OperatorsRule:
			cs = appendMetadata(cs, e.OpenComments, control)
			add(e.SkipBreak)
			add(e.CloseBreak)
			expr(e.Skip)
			for _, o := range e.Operands {
				control.poll()
				add(o.Break)
				expr(o.Expr)
				actionExpr(o.Action)
			}
			for _, l := range e.Levels {
				control.poll()
				add(l.Break)
				add(l.CloseBreak)
				for _, op := range l.Operators {
					control.poll()
					add(op.Break)
					expr(op.Expr)
					actionExpr(op.Action)
				}
			}
		}
	}
	add(g.File.Pkg.Break)
	for _, attr := range g.File.Pkg.Attrs {
		control.poll()
		addLayout(attr.Layout)
		addList(attr.ArgsLayout)
		for _, arg := range attr.expressionArgs(control) {
			expr(arg)
		}
	}
	addAll(g.File.Pkg.AttrBreaks)
	add(g.File.Pkg.KeywordBreak)
	for _, s := range g.File.Decls {
		control.poll()
		switch s := s.(type) {
		case *TypeDef:
			add(s.Break)
			if s.Type != nil {
				typ(s.Type)
			}
		case *TypeAlias:
			add(s.Break)
			typ(s.Type)
		case *VarDef:
			add(s.Break)
			if s.Type != nil {
				typ(s.Type)
			}
			actionExpr(s.Value)
		case *RuleDef:
			for _, attr := range s.Attrs {
				control.poll()
				addLayout(attr.Layout)
				addList(attr.ArgsLayout)
				for _, arg := range attr.expressionArgs(control) {
					expr(arg)
				}
			}
			addAll(s.AttrBreaks)
			add(s.KeywordBreak)
			typ(s.Type)
			add(s.Break)
			add(s.BodyBreak)
			add(s.ActionBreak)
			add(s.CloseBreak)
			expr(s.Expr)
			actionExpr(s.Action)
		}
	}
	add(g.File.EndBreak)
	cs = expandRuns(cs)
	// The comments of the declaration headers are in File.HeaderComments. Most of them stay with
	// the token after them, where the walk above finds them; a header that takes them from that
	// token, as the header of a variable declaration does with the comments before its type or
	// its "=", keeps them only there.
	if len(g.File.HeaderComments) > 0 {
		seen := make(map[*Comment]bool, len(cs))
		for _, c := range cs {
			control.poll()
			seen[c] = true
		}
		for _, c := range g.File.HeaderComments {
			control.poll()
			if !seen[c] {
				cs = append(cs, c)
			}
		}
	}
	return cs
}

// expandRuns replaces each comment that stands for a run of comments by them.
func expandRuns(cs []*Comment) []*Comment {
	for i, c := range cs {
		if c != nil && len(c.Run) > 0 {
			out := append([]*Comment(nil), cs[:i]...)
			for _, c := range cs[i:] {
				if c != nil && len(c.Run) > 0 {
					out = append(out, c.Run...)
				} else {
					out = append(out, c)
				}
			}
			return out
		}
	}
	return cs
}

// invalidLayoutComment returns the field of the first comment string of node's source layout that
// is not valid UTF-8, or "": the text or the content of a comment in the line breaks, gaps and
// other layout the node holds itself. The layout is kept out of JSON, so the field names the Go
// fields with a lower-case first letter. Format writes these comments, so their strings are
// strings of the grammar text. Only a failure builds a field name.
func invalidLayoutComment(node any) string {
	if n, ok := node.(SourceNode); ok && n.Source() != nil {
		if f := badLayout(n.Source().Layout); f != "" {
			return "source.layout" + f
		}
	}
	switch n := node.(type) {
	case *Grammar:
		f := &n.File
		return firstLayoutField(
			layoutField("file.source.layout", badLayout(f.Layout)),
			layoutField("file.pkg.source.layout", badLayout(f.Pkg.Layout)),
			layoutField("file.pkg.break", badLineBreak(f.Pkg.Break)),
			layoutField("file.pkg.attrBreaks", badLineBreaks(f.Pkg.AttrBreaks)),
			layoutField("file.pkg.keywordBreak", badLineBreak(f.Pkg.KeywordBreak)),
			layoutField("file.endBreak", badLineBreak(f.EndBreak)),
			layoutField("file.headerComments", badComments(f.HeaderComments)))
	case *TypeDef:
		return layoutField("break", badLineBreak(n.Break))
	case *TypeAlias:
		return layoutField("break", badLineBreak(n.Break))
	case *VarDef:
		return layoutField("break", badLineBreak(n.Break))
	case *RuleDef:
		return firstLayoutField(
			layoutField("attrBreaks", badLineBreaks(n.AttrBreaks)),
			layoutField("keywordBreak", badLineBreak(n.KeywordBreak)),
			layoutField("break", badLineBreak(n.Break)),
			layoutField("bodyBreak", badLineBreak(n.BodyBreak)),
			layoutField("actionBreak", badLineBreak(n.ActionBreak)),
			layoutField("closeBreak", badLineBreak(n.CloseBreak)))
	case *ActionGroup:
		return badBrackets(n.OpenBreak, n.BodyBreak, n.CloseBreak)
	case *Constructor:
		return badBrackets(n.OpenBreak, n.BodyBreak, n.CloseBreak)
	case *TupleLit:
		return badBrackets(n.OpenBreak, n.BodyBreak, n.CloseBreak)
	case *ListConstructor:
		return badBrackets(n.OpenBreak, n.BodyBreak, n.CloseBreak)
	case *Call:
		return badBrackets(n.OpenBreak, n.BodyBreak, n.CloseBreak)
	case *Binary:
		return badBrackets(n.OpenBreak, n.BodyBreak, n.CloseBreak)
	case *TypeGroup:
		return badBrackets(n.OpenBreak, n.BodyBreak, n.CloseBreak)
	case *TupleType:
		return badBrackets(n.OpenBreak, n.BodyBreak, n.CloseBreak)
	case *TypeApp:
		return badBrackets(n.OpenBreak, n.BodyBreak, n.CloseBreak)
	case *Block:
		return firstLayoutField(
			layoutField("openBreak", badLineBreak(n.OpenBreak)),
			layoutField("breaks", badLineBreaks(n.Breaks)),
			layoutField("closeBreak", badLineBreak(n.CloseBreak)))
	case *Predicate:
		return firstLayoutField(
			layoutField("openBreak", badLineBreak(n.OpenBreak)),
			layoutField("breaks", badLineBreaks(n.Breaks)),
			layoutField("closeBreak", badLineBreak(n.CloseBreak)))
	case *StructType:
		return layoutField("closeBreak", badLineBreak(n.CloseBreak))
	case *Field:
		return firstLayoutField(
			layoutField("break", badLineBreak(n.Break)),
			layoutField("headerTokens", badComments(n.HeaderTokens)))
	case *Lambda:
		return layoutField("paramsLayout", badList(n.ParamsLayout))
	case *Attribute:
		return layoutField("argsLayout", badList(n.ArgsLayout))
	case *Group:
		return firstLayoutField(
			layoutField("bodyBreak", badLineBreak(n.BodyBreak)),
			layoutField("closeBreak", badLineBreak(n.CloseBreak)))
	case *Choice:
		return layoutField("breaks", badLineBreaks(n.Breaks))
	case *Seq:
		return layoutField("breaks", badLineBreaks(n.Breaks))
	case *Attributed:
		return layoutField("breaks", badLineBreaks(n.Breaks))
	case *Ref:
		if g := n.LevelGaps; g != nil {
			return firstLayoutField(
				layoutField("levelGaps.open", badGap(g.Open)),
				layoutField("levelGaps.before", badGap(g.Before)),
				layoutField("levelGaps.after", badGap(g.After)),
				layoutField("levelGaps.close", badGap(g.Close)))
		}
	case *Repeat:
		if f := layoutField("pragmaBreak", badLineBreak(n.PragmaBreak)); f != "" || n.Spelling == nil {
			return f
		}
		for i := range n.Spelling.Gaps {
			if f := badComments(n.Spelling.Gaps[i].Comments); f != "" {
				return "spelling.gaps[" + strconv.Itoa(i) + "].comments" + f
			}
		}
	case *Stream:
		return layoutField("break", badLineBreak(n.Break))
	case *Recovery:
		return layoutField("break", badLineBreak(n.Break))
	case *Atomic:
		return layoutField("break", badLineBreak(n.Break))
	case *OperatorsRule:
		return firstLayoutField(
			layoutField("openComments", badComments(n.OpenComments)),
			layoutField("skipBreak", badLineBreak(n.SkipBreak)),
			layoutField("closeBreak", badLineBreak(n.CloseBreak)))
	case *OperatorsRuleOperand:
		return layoutField("break", badLineBreak(n.Break))
	case *OperatorsRuleLevel:
		return firstLayoutField(
			layoutField("break", badLineBreak(n.Break)),
			layoutField("closeBreak", badLineBreak(n.CloseBreak)))
	case *OperatorsRuleOperator:
		return layoutField("break", badLineBreak(n.Break))
	}
	return ""
}

// The bad* functions below return the path, relative to their argument, of its first comment
// string that is not valid UTF-8, or "". A path starts with "." or "[", so layoutField joins it to the
// name of the argument.

func layoutField(name, bad string) string {
	if bad == "" {
		return ""
	}
	return name + bad
}

func firstLayoutField(fields ...string) string {
	for _, f := range fields {
		if f != "" {
			return f
		}
	}
	return ""
}

func badComment(c *Comment) string {
	if c == nil {
		return ""
	}
	if !utf8.ValidString(c.Text) {
		return ".text"
	}
	if !utf8.ValidString(c.Content) {
		return ".content"
	}
	// The comments of a run are those c stands for; each is checked here, as Format writes it
	// through c.
	for i, r := range c.Run {
		if r == nil {
			continue
		}
		if !utf8.ValidString(r.Text) {
			return ".run[" + strconv.Itoa(i) + "].text"
		}
		if !utf8.ValidString(r.Content) {
			return ".run[" + strconv.Itoa(i) + "].content"
		}
	}
	return ""
}

func badComments(cs []*Comment) string {
	for i, c := range cs {
		if f := badComment(c); f != "" {
			return "[" + strconv.Itoa(i) + "]" + f
		}
	}
	return ""
}

func badLineBreak(b *LineBreak) string {
	if b == nil {
		return ""
	}
	return firstLayoutField(
		layoutField(".comments", badComments(b.Comments)),
		layoutField(".trailing", badComment(b.Trailing)),
		layoutField(".last", badComment(b.Last)))
}

func badLineBreaks(bs []*LineBreak) string {
	for i, b := range bs {
		if f := badLineBreak(b); f != "" {
			return "[" + strconv.Itoa(i) + "]" + f
		}
	}
	return ""
}

func badBrackets(open, body, close *LineBreak) string {
	return firstLayoutField(
		layoutField("openBreak", badLineBreak(open)),
		layoutField("bodyBreak", badLineBreak(body)),
		layoutField("closeBreak", badLineBreak(close)))
}

func badGap(g *Gap) string {
	if g == nil {
		return ""
	}
	return layoutField(".comments", badComments(g.Comments))
}

func badLayout(l *SourceLayout) string {
	if l == nil {
		return ""
	}
	return firstLayoutField(
		layoutField(".before", badComments(l.Before)),
		layoutField(".after", badComments(l.After)),
		layoutField(".trail", badComments(l.Trail)),
		layoutField(".operator", badComments(l.Operator)),
		layoutField(".open", badGap(l.Open)),
		layoutField(".eq", badGap(l.Eq)),
		layoutField(".close", badGap(l.Close)),
		layoutField(".opGap", badGap(l.OpGap)),
		layoutField(".sep", badGap(l.Sep)),
		layoutField(".lead", badGap(l.Lead)))
}

func badList(l *ListLayout) string {
	if l == nil {
		return ""
	}
	if f := firstLayoutField(
		layoutField(".openBreak", badLineBreak(l.OpenBreak)),
		layoutField(".bodyBreak", badLineBreak(l.BodyBreak)),
		layoutField(".closeBreak", badLineBreak(l.CloseBreak))); f != "" {
		return f
	}
	for i, e := range l.Elems {
		if f := badLayout(e); f != "" {
			return ".elems[" + strconv.Itoa(i) + "]" + f
		}
	}
	return ""
}
