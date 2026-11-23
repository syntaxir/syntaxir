package parser

import (
	"strings"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// layoutCursor reads the tokens of a source in order and gives the comments
// before each token an owner: the line break most recently recorded, a line
// break it records at the token, or the layout of a node. The hand-written
// parser reads its tokens with it, and layoutReplay calls the same primitives at
// the same tokens to rebuild the raw formatter state of a grammar that the
// generated frontend built.
type layoutCursor struct {
	toks []token
	i    int
	// semi is the line break whose trailing comment was taken before a semicolon.
	semi *ast.LineBreak
	// inlined is one more than the index of the token whose inline gap was
	// taken, so that the lines inside its comments do not count as a line break
	// before the token.
	inlined int
	// cur is the line break most recently recorded. Comments that do not
	// precede a recorded line break are added to it, because the formatter
	// writes the tokens they follow on the line after it.
	cur *ast.LineBreak
	// nest counts the choices being read. Line breaks are recorded only outside
	// them, in the top level of a rule body.
	nest int
	// header is set while the tokens of a declaration's header are read: their
	// comments are the header comments, which record headerPos.
	header         bool
	headerComments []*ast.Comment
	headerPos      ast.Pos
}

// next consumes the current token and returns it.
func (lc *layoutCursor) next() token {
	t := lc.toks[lc.i]
	lc.collectHeader(t)
	lc.advance()
	return t
}

// collectHeader adds the comments before t, the current token, to the header
// comments while a header is read.
func (lc *layoutCursor) collectHeader(t token) {
	if !lc.header {
		return
	}
	t.source.ownByHeader()
	if t.lineComment != nil {
		t.lineComment.Header = lc.headerPos
		lc.headerComments = append(lc.headerComments, t.lineComment)
	}
	for _, comment := range t.comments {
		comment.Header = lc.headerPos
	}
	lc.headerComments = lc.appendComments(lc.headerComments, t.comments)
}

// accept consumes the current token if it is the punctuation or identifier
// text. The parser has its own accept, which consumes with its own next.
func (lc *layoutCursor) accept(text string) bool {
	if lc.is(text) {
		lc.next()
		return true
	}
	return false
}

// separator consumes the semicolon that ends a statement on its line, as the
// parser's separator does.
func (lc *layoutCursor) separator() bool {
	if lc.tok().lineBefore {
		return false
	}
	return lc.accept(";")
}

// advance moves the comments before the current token to the current line
// break and moves to the next token; it stays at the last token.
func (lc *layoutCursor) advance() {
	lc.takeComments(lc.i)
	if lc.i < len(lc.toks)-1 {
		lc.i++
	}
}

func (lc *layoutCursor) tok() token { return lc.toks[lc.i] }

func (lc *layoutCursor) peek(k int) token {
	if lc.i+k < len(lc.toks) {
		return lc.toks[lc.i+k]
	}
	return lc.toks[len(lc.toks)-1]
}

func (lc *layoutCursor) appendComments(dst, comments []*ast.Comment) []*ast.Comment {
	return append(dst, comments...)
}

// headerGap returns the normalized contents of the comments before the current token, the gap
// between the name and the type of a struct field, which the field owns. No node starting at
// the token owns them (see commentBoundary.ownByHeader).
func (lc *layoutCursor) headerGap() []string {
	t := lc.toks[lc.i]
	t.source.ownByHeader()
	var out []string
	if t.lineComment != nil {
		out = append(out, t.lineComment.Content)
	}
	for _, c := range t.comments {
		out = append(out, c.Content)
	}
	return out
}

// takeHeaderGap is headerGap that also returns the comment tokens, which leave the token: the
// formatter writes them between the name and the type of the field.
func (lc *layoutCursor) takeHeaderGap() ([]string, []*ast.Comment) {
	out := lc.headerGap()
	t := &lc.toks[lc.i]
	var tokens []*ast.Comment
	if t.lineComment != nil {
		tokens = append(tokens, t.lineComment)
	}
	tokens = append(tokens, t.comments...)
	t.lineComment, t.comments = nil, nil
	return out, tokens
}

// headerBefore records the comments before the current token as header comments of the
// declaration at pos, as if the token were read inside its header, and returns their
// normalized contents. The comments leave the token, so the node starting at it does not own
// them: they are the gap between the name and the type of a variable declaration.
func (lc *layoutCursor) headerBefore(pos ast.Pos) []string {
	from := len(lc.headerComments)
	header, headerPos := lc.header, lc.headerPos
	lc.header, lc.headerPos = true, pos
	t := &lc.toks[lc.i]
	lc.collectHeader(*t)
	lc.header, lc.headerPos = header, headerPos
	t.lineComment, t.comments = nil, nil
	out := make([]string, 0, len(lc.headerComments)-from)
	for _, c := range lc.headerComments[from:] {
		out = append(out, c.Content)
	}
	return out
}

// takeRun returns the comment c at the end of the line of the token before t, together with the
// comments after it on that line that all trail the same token (see collectComments), as one
// comment. Those comments leave t.
func (t *token) takeRun(c *ast.Comment) *ast.Comment {
	if t.source == nil || t.source.run <= 1 || c != t.lineComment || len(t.comments) < t.source.run-1 {
		return c
	}
	n := t.source.run - 1
	for _, next := range t.comments[:n] {
		c = joinComments(c, next)
	}
	t.comments = t.comments[n:]
	t.source.run = 0
	return c
}

// joinComments returns the comments a and b, which share a line, as one comment.
func joinComments(a, b *ast.Comment) *ast.Comment {
	run := func(c *ast.Comment) []*ast.Comment {
		if len(c.Run) > 0 {
			return c.Run
		}
		return []*ast.Comment{c}
	}
	return &ast.Comment{Pos: a.Pos, End: b.End, Text: a.Text + " " + b.Text, Content: a.Content + b.Content, Blank: a.Blank,
		Run: append(append([]*ast.Comment(nil), run(a)...), run(b)...)}
}

// takeComments moves the comments before token i to the current line
// break.
func (lc *layoutCursor) takeComments(i int) {
	t := &lc.toks[i]
	if lc.cur != nil {
		if t.lineComment != nil && t.kind == tPunct && t.text == ";" && lc.cur.Trailing == nil {
			// A comment before a semicolon trails the statement it ends; a comment after the
			// semicolon, on its line, follows it (see lineBreak).
			lc.cur.Trailing = t.takeRun(t.lineComment)
			lc.semi = lc.cur
			t.lineComment = nil
		}
		if t.lineComment != nil {
			lc.cur.Comments = append(lc.cur.Comments, t.lineComment)
		}
		lc.cur.Comments = lc.appendComments(lc.cur.Comments, t.comments)
	}
	t.lineComment, t.comments = nil, nil
}

// lineBreak records the start of a line at the current token, where the
// formatter may start a new line, with the comments before the token. A
// comment at the end of the previous line becomes the trailing comment of
// the line break recorded before.
func (lc *layoutCursor) lineBreak() *ast.LineBreak {
	t := &lc.toks[lc.i]
	c := t.lineComment
	trails := c != nil && lc.cur != nil && lc.cur.Trailing == nil
	after := c != nil && !trails && lc.cur != nil && lc.cur == lc.semi && (c.End.Line < t.pos.Line || t.endsScope())
	if trails || after {
		c = t.takeRun(c)
	}
	b := &ast.LineBreak{Comments: t.comments, Blank: t.blankBefore}
	switch {
	case trails:
		lc.cur.Trailing = c
		lc.cur.TrailingEnds = c.End.Line < t.pos.Line
	case after:
		// The comments after the semicolon follow those before it, as they own them together.
		lc.cur.Trailing = joinComments(lc.cur.Trailing, c)
		lc.cur.TrailingEnds = c.End.Line < t.pos.Line
	case c != nil:
		b.Comments = lc.appendComments([]*ast.Comment{c}, b.Comments)
	}
	t.lineComment, t.comments = nil, nil
	lc.cur = b
	return b
}

// statementBreak is lineBreak for a token that starts a declaration, a field or
// a line of an operators rule. A block comment over several lines that ends on
// the line of that token is not a trailing comment of the line before: it is
// detached from the token, so it takes lines of its own before the token, set
// off by blank lines. It does so only when that comment is the only one before
// the token; with several, the first still trails the line before. A block
// comment of one line between two tokens of that line is detached the same way. A closing
// brace starts no statement, so the comment before it trails the last one.
func (lc *layoutCursor) statementBreak() *ast.LineBreak {
	t := &lc.toks[lc.i]
	c := t.lineComment
	if c == nil || len(t.comments) != 0 || t.text == "}" || !strings.HasPrefix(c.Text, "/*") || c.End.Line != t.pos.Line {
		return lc.lineBreak()
	}
	// A block comment on one line is detached only between two tokens of that
	// line, as after a semicolon; after an opening brace it leads the statement, and
	// at the end of a line it trails the statement.
	if c.Pos.Line == c.End.Line && (lc.i == 0 || lc.toks[lc.i-1].end.Line != c.Pos.Line || lc.toks[lc.i-1].text == "{") {
		return lc.lineBreak()
	}
	t.lineComment = nil
	b := lc.lineBreak()
	c.Blank = true
	b.Comments = append([]*ast.Comment{c}, b.Comments...)
	b.Blank = true
	return b
}

// multiline reports whether a line break precedes any of the tokens after
// token i up to token j.
func (lc *layoutCursor) multiline(i, j int) bool {
	for k := i + 1; k <= j && k < len(lc.toks); k++ {
		if lc.toks[k].lineBefore {
			return true
		}
	}
	return false
}

func (lc *layoutCursor) is(text string) bool {
	t := lc.tok()
	return (t.kind == tPunct || t.kind == tIdent) && t.text == text
}

// leadAfterOpen makes a block comment between an opening brace and the first
// token after it, on that line, lead that token; it does not trail the line
// break recorded before.
func (lc *layoutCursor) leadAfterOpen() {
	if c := lc.tok().lineComment; c != nil && strings.HasPrefix(c.Text, "/*") && c.End.Line == lc.tok().pos.Line {
		lc.cur = nil
	}
}

// restoreBreak returns a function that restores the current line break, for
// the delimited list that starts here.
func (lc *layoutCursor) restoreBreak() func() {
	outer := lc.cur
	return func() { lc.cur = outer }
}

// openLead takes the block comment after an opening delimiter that is followed
// by a token on its line. It leads that token, so it goes with the comments
// before it instead of trailing the line of the delimiter.
func (lc *layoutCursor) openLead() *ast.Comment {
	t := &lc.toks[lc.i]
	c := t.lineComment
	if c == nil || !strings.HasPrefix(c.Text, "/*") || c.End.Line != t.pos.Line {
		return nil
	}
	t.lineComment = nil
	return c
}

// leadBreak puts the comment taken by openLead before the others in b.
func (lc *layoutCursor) leadBreak(b *ast.LineBreak, c *ast.Comment) {
	if c != nil {
		b.Comments = append([]*ast.Comment{c}, b.Comments...)
	}
}

// openList starts the layout of a delimited list after its opening delimiter.
// The comment at the end of the delimiter's line is the trailing comment of the
// opening break, and the comments before the first element belong to the body
// break. Both stay with the list instead of the statement around it.
func (lc *layoutCursor) openList() (open, body *ast.LineBreak) {
	open = &ast.LineBreak{}
	lc.cur = open
	return open, lc.lineBreak()
}

// afterCommaLayout is afterComma for the layout of an element, which may not
// be a node.
func (lc *layoutCursor) afterCommaLayout(layout **ast.SourceLayout, closer string) {
	if lc.header || lc.is(closer) {
		return
	}
	t := &lc.toks[lc.i]
	cs := t.comments
	if t.lineComment != nil {
		cs = lc.appendComments([]*ast.Comment{t.lineComment}, cs)
	}
	line := lc.toks[lc.i-1].end.Line
	n := 0
	for n < len(cs) && cs[n].Pos.Line <= line {
		n++
	}
	if n == 0 {
		return
	}
	next := t.pos.Line
	if n < len(cs) {
		next = cs[n].Pos.Line
	}
	if last := cs[n-1]; !strings.HasPrefix(last.Text, "//") && last.End.Line >= next {
		return
	}
	if *layout == nil {
		*layout = &ast.SourceLayout{}
	}
	(*layout).Trail = cs[:n:n]
	t.lineComment, t.comments = nil, cs[n:]
}

// takeAfterLayout keeps the comments between an element of a list and the
// comma that follows it with the element, rather than promoting them to the
// current line break, which would move them before the first element.
func (lc *layoutCursor) takeAfterLayout(node any) {
	if x, ok := node.(ast.SourceNode); ok {
		lc.takeAfter(&x.Source().Layout)
	}
}

// takeAfter is takeAfterLayout for the layout of an element, which may not be
// a node.
func (lc *layoutCursor) takeAfter(layout **ast.SourceLayout) {
	if lc.header || !lc.is(",") {
		return
	}
	t := &lc.toks[lc.i]
	if t.lineComment == nil && len(t.comments) == 0 {
		return
	}
	cs := t.comments
	if t.lineComment != nil {
		cs = lc.appendComments([]*ast.Comment{t.lineComment}, cs)
	}
	if *layout == nil {
		*layout = &ast.SourceLayout{}
	}
	(*layout).After = cs
	(*layout).ElemLine, (*layout).AfterLine = lc.toks[lc.i-1].end.Line, t.pos.Line
	t.lineComment, t.comments = nil, nil
}

// takeInlineGap takes the comments between the previous token and the current
// one when they are all block comments and no line break separates the two
// tokens and the comments but the lines inside the comments. The formatter
// writes them in the same place, so their owners do not change.
func (lc *layoutCursor) takeInlineGap() []*ast.Comment {
	if !lc.inlineGap() {
		return nil
	}
	t := &lc.toks[lc.i]
	cs := t.comments
	if t.lineComment != nil {
		cs = lc.appendComments([]*ast.Comment{t.lineComment}, cs)
	}
	t.lineComment, t.comments = nil, nil
	lc.inlined = lc.i + 1
	return cs
}

// delimitedGap takes the comments before the current token inside a choice that does not
// record line breaks, such as one inside delimiters, where they may end a line or stand on lines
// of their own: before a pragma, an attribute or an item of a sequence after the operand or the
// item before it. An inline gap (see takeInlineGap) is left to the action layout.
func (lc *layoutCursor) delimitedGap() *ast.Gap {
	if lc.nest == 0 || lc.inlineGap() {
		return nil
	}
	return lc.takeCommentGap()
}

// endLine ends a line of an operators rule, whose line break is b: the comments
// at the end of the line trail the line, not a line break inside it, such as
// the one before the ")" of a group, which the formatter writes there.
func (lc *layoutCursor) endLine(b *ast.LineBreak) { lc.cur = b }

// levelGaps returns g, the comments inside a level call, or nil when it has none.
func levelGaps(g ast.LevelGaps) *ast.LevelGaps {
	if g == (ast.LevelGaps{}) {
		return nil
	}
	return &g
}

// typeBefore takes the inline gap before the first token of a type that follows a "=", "~",
// "[]" or "*" (see takeInlineGap). setTypeBefore keeps it in the type's layout.
func (lc *layoutCursor) typeBefore() []*ast.Comment { return lc.takeInlineGap() }

// typeAfter keeps the comments before the type operator op, "|" or "&", when it is the current
// token, in the OpGap field of the layout of the operand x before it. Inside parentheses they may
// stand on lines of their own.
func (lc *layoutCursor) typeAfter(x ast.TypeExpr, op string) {
	if !lc.is(op) {
		return
	}
	if gap := lc.takeCommentGap(); gap != nil {
		layoutOf(x).OpGap = gap
	}
}

// typeLead takes the comments after a type operator, "|" or "&", before its right operand, which
// setTypeLead keeps in the Lead field of the operand's layout.
func (lc *layoutCursor) typeLead() *ast.Gap { return lc.takeCommentGap() }

// setTypeLead keeps the comments that typeLead took in the layout of x.
func setTypeLead(x ast.TypeExpr, gap *ast.Gap) {
	if gap != nil {
		layoutOf(x).Lead = gap
	}
}

// setTypeBefore keeps the comments that typeBefore took in the layout of x.
func setTypeBefore(x ast.TypeExpr, gap []*ast.Comment) {
	if gap != nil {
		l := layoutOf(x)
		l.Before = append(gap, l.Before...)
	}
}

// setOperatorsLayout keeps the inline comments of an Operators Rule opener:
// before, between "=" and "#", in the Before field of the layout of the rule
// body e, and open, inside "#operators {", in the rule's OpenComments.
func setOperatorsLayout(e ast.Expr, before, open []*ast.Comment) {
	if before != nil {
		layoutOf(e).Before = before
	}
	if pr, _ := ast.RuleOperators(e); pr != nil && open != nil {
		pr.OpenComments = open
	}
}

// inlineGap reports whether the comments before the current token form a gap
// takeInlineGap takes.
func (lc *layoutCursor) inlineGap() bool {
	if lc.header || lc.i == 0 {
		return false
	}
	t := &lc.toks[lc.i]
	if t.lineComment == nil && len(t.comments) == 0 {
		return false
	}
	line := lc.toks[lc.i-1].end.Line
	step := func(c *ast.Comment) bool {
		if !strings.HasPrefix(c.Text, "/*") || c.Pos.Line != line {
			return false
		}
		line = c.End.Line
		return true
	}
	if t.lineComment != nil && !step(t.lineComment) {
		return false
	}
	for _, c := range t.comments {
		if !step(c) {
			return false
		}
	}
	return line == t.pos.Line
}

// takeCommentGap takes the comments between the previous token and the current one,
// whatever their kind and place. It is for places inside delimiters or after an
// operator, where a comment may stand on a line of its own: the Gap keeps the
// lines of the tokens and the comments for the formatter.
func (lc *layoutCursor) takeCommentGap() *ast.Gap {
	if lc.header || lc.i == 0 {
		return nil
	}
	t := &lc.toks[lc.i]
	if t.lineComment == nil && len(t.comments) == 0 {
		return nil
	}
	cs := t.comments
	if t.lineComment != nil {
		cs = lc.appendComments([]*ast.Comment{t.lineComment}, cs)
	}
	t.lineComment, t.comments = nil, nil
	return &ast.Gap{Comments: cs, PreviousLine: lc.toks[lc.i-1].end.Line, NextLine: t.pos.Line}
}

// leadOf keeps the gap before the first token of e, if there is one.
func (lc *layoutCursor) leadOf(e ast.Expr, gap *ast.Gap) {
	if gap != nil {
		layoutOf(e).Lead = gap
	}
}

// atLineStart reports whether the current token starts a line that the layout
// of a rule body records: a line break before it, other than the lines inside
// the block comments that stand between it and the token before.
func (lc *layoutCursor) atLineStart() bool {
	return lc.tok().lineBefore && !lc.inlineGap() && lc.inlined != lc.i+1
}

// afterGap keeps the inline comments before the current token, "/" or "=>", as
// the comments after e.
func (lc *layoutCursor) afterGap(e ast.Expr) {
	if gap := lc.takeInlineGap(); gap != nil {
		layoutOf(e).After = gap
	}
}

// takeGap takes the comments before the current token, which lie between it
// and the token before it, for a #repeat pragma.
func (lc *layoutCursor) takeGap() ast.RepeatGap {
	t := &lc.toks[lc.i]
	cs := t.comments
	if t.lineComment != nil {
		cs = lc.appendComments([]*ast.Comment{t.lineComment}, cs)
	}
	g := ast.RepeatGap{Comments: cs, PrevLine: lc.toks[lc.i-1].end.Line, NextLine: t.pos.Line}
	t.lineComment, t.comments = nil, nil
	return g
}

// takeActionLayout keeps trivia at an expression boundary rather than promoting
// it to the current statement's line break. Normalized ownership is independent.
func (lc *layoutCursor) takeActionLayout() *ast.SourceLayout {
	if lc.header {
		return nil
	}
	t := &lc.toks[lc.i]
	if t.lineComment == nil && len(t.comments) == 0 {
		return nil
	}
	cs := t.comments
	if t.lineComment != nil {
		cs = lc.appendComments([]*ast.Comment{t.lineComment}, cs)
	}
	previous := 0
	if lc.i > 0 {
		previous = lc.toks[lc.i-1].end.Line
	}
	layout := &ast.SourceLayout{Before: cs, PreviousLine: previous, NextLine: t.pos.Line}
	t.lineComment, t.comments = nil, nil
	return layout
}
