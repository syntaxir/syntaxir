package parser

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// commentBoundary is lexer-owned syntax trivia. The parser claims each side
// when constructing its node; no semantic-analysis attachment pass is needed.
type commentBoundary struct {
	leading  string
	trailing []string
	detached []string
	// run is the number of comments on the line of the previous token that are all trailing
	// comments of it, because a line break or the end of a scope follows them.
	run           int
	hasTrailing   bool
	hasNext       bool
	leadingOwner  *ast.SourceInfo
	trailingOwner *ast.SourceInfo
	// header is set when the comments are in a declaration header or between the name and
	// the type of a field: the declaration or field owns them as its HeaderComments, and no
	// node does.
	header bool
}

// ownByHeader gives the comments of b to the header that reads them, taking them from the
// nodes that claimed them before: a node that ends before the header token claims the
// trailing group before the parser reads that token. b may be nil.
func (b *commentBoundary) ownByHeader() {
	if b == nil || b.header {
		return
	}
	b.header = true
	if b.leadingOwner != nil {
		b.leadingOwner.LeadingComments, b.leadingOwner.LeadingDetachedComments = "", nil
		b.leadingOwner = nil
	}
	if b.trailingOwner != nil {
		b.trailingOwner.TrailingComments = nil
		b.trailingOwner = nil
	}
}

func collectComments(previous ast.Pos, opens, opener bool, next token, comments []*ast.Comment) *commentBoundary {
	return collectCommentsWithCheck(previous, opens, opener, next, comments, nil)
}

// collectCommentsWithCheck groups the comments between two tokens. opens
// reports that the token before them is an opening delimiter or a comma, and
// opener that it is an opening delimiter. An opening delimiter is not a node, so
// the comments on its line lead the next token as if they were on lines of their
// own, unless a closing delimiter follows them.
func collectCommentsWithCheck(previous ast.Pos, opens, opener bool, next token, comments []*ast.Comment, check func()) *commentBoundary {
	b := &commentBoundary{}
	canPrevious := previous.IsValid() && !(opener && !next.endsScope())
	inlineEnd := 0
	groups := 0
	var pending []*ast.Comment
	inRun := false // flushing the comments of the line of the previous token
	flush := func() {
		if pending == nil {
			return
		}
		content := joinCommentContent(pending, check)
		if canPrevious || inRun {
			b.trailing = append(b.trailing, content)
			b.hasTrailing = true
			canPrevious = false
		} else {
			b.detached = append(b.detached, content)
		}
		pending = nil
		groups++
	}
	last := previous
	scopeEnd := next.endsScope()
	if previous.IsValid() && comments[0].Pos.Line == previous.Line {
		// The comments on the line of the previous token are all trailing when a line break or
		// the end of a scope follows them; otherwise only the first is a group of its own.
		n := 1
		for n < len(comments) && comments[n].Pos.Line == comments[n-1].End.Line && !strings.HasPrefix(comments[n-1].Text, "//") {
			n++
		}
		if !scopeEnd && next.pos.Line <= comments[n-1].End.Line {
			n = 1
		}
		if !canPrevious {
			// The comments of an opening delimiter's line lead the next token, but the layout
			// keeps them on that line as one run.
			b.run = n
		} else {
			inRun = true
			for _, c := range comments[:n] {
				pending = []*ast.Comment{c}
				flush() // Inline comments never absorb following comment-only lines.
			}
			inRun = false
			b.run = n
			last = comments[n-1].End
			inlineEnd = last.Line
			canPrevious = false
			comments = comments[n:]
		}
	}
	for i, c := range comments {
		if check != nil && i&255 == 0 {
			check()
		}
		if last.IsValid() && c.Pos.Line-last.Line >= 2 {
			flush()
			canPrevious = false
		}
		if len(pending) != 0 && (!strings.HasPrefix(pending[0].Text, "//") || !strings.HasPrefix(c.Text, "//")) {
			flush()
		}
		pending = append(pending, c)
		last = c.End
	}
	if next.pos.Line-last.Line >= 2 {
		flush()
		canPrevious = false
	}
	b.hasNext = !scopeEnd
	if scopeEnd {
		flush()
	}
	count := groups
	if pending != nil {
		count++
	}
	// A lone block comment between two tokens of one line is neither the trailing
	// comment of the first nor the leading comment of the second, unless the first
	// opens a list or separates its elements, and then it leads the second. Before a
	// closing delimiter it trails the token before it.
	if !scopeEnd && (previous.Line == next.pos.Line || inlineEnd == next.pos.Line) && count == 1 {
		if b.hasTrailing {
			if opens {
				b.leading = b.trailing[0]
			} else {
				b.detached = append([]string{b.trailing[0]}, b.detached...)
			}
			b.trailing, b.hasTrailing, b.run = nil, false, 0
		}
		canPrevious = false
		if !opens {
			flush()
		}
	}
	if !scopeEnd && pending != nil {
		b.leading = joinCommentContent(pending, check)
	}
	return b
}

func joinCommentContent(comments []*ast.Comment, check func()) string {
	if len(comments) == 1 {
		if check != nil {
			check()
		}
		return comments[0].Content
	}
	length := 0
	for i, c := range comments {
		if check != nil && i&255 == 0 {
			check()
		}
		length += len(c.Content)
	}
	var out strings.Builder
	out.Grow(length)
	for i, c := range comments {
		if check != nil && i&255 == 0 {
			check()
		}
		writeCommentContent(&out, c.Content, check)
	}
	return out.String()
}

func commentContent(raw, ending string) string {
	return commentContentWithCheck(raw, ending, nil)
}

func commentContentWithCheck(raw, ending string, check func()) string {
	if strings.HasPrefix(raw, "//") {
		if check == nil {
			return raw[2:] + ending
		}
		var out strings.Builder
		out.Grow(len(raw) - 2 + len(ending))
		writeCommentContent(&out, raw[2:], check)
		out.WriteString(ending)
		return out.String()
	}
	content := strings.TrimPrefix(raw, "/*")
	content = strings.TrimSuffix(content, "*/")
	var out strings.Builder
	first := true
	for {
		i := commentLineBreak(content, check)
		line, newline := content, ""
		if i >= 0 {
			line, newline = content[:i], content[i:i+1]
			if newline == "\r" && i+1 < len(content) && content[i+1] == '\n' {
				newline = "\r\n"
			}
		}
		if !first {
			line = trimCommentLeft(line, check)
			if !strings.HasPrefix(line, "*/") {
				line = strings.TrimPrefix(line, "*")
			}
		}
		if check != nil {
			out.Grow(len(line) + len(newline))
		}
		writeCommentContent(&out, line, check)
		out.WriteString(newline)
		if i < 0 {
			break
		}
		content = content[i+len(newline):]
		first = false
	}
	return out.String()
}

func commentLineBreak(s string, check func()) int {
	if check == nil {
		return strings.IndexAny(s, "\r\n")
	}
	for i := 0; i < len(s); i += 256 {
		check()
		end := min(i+256, len(s))
		if j := strings.IndexAny(s[i:end], "\r\n"); j >= 0 {
			return i + j
		}
	}
	return -1
}

func trimCommentLeft(s string, check func()) string {
	if check == nil {
		return strings.TrimLeftFunc(s, unicode.IsSpace)
	}
	for i, n := 0, 0; i < len(s); n++ {
		if n&255 == 0 {
			check()
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsSpace(r) {
			return s[i:]
		}
		i += size
		if i == len(s) {
			return ""
		}
	}
	return s
}

func writeCommentContent(out *strings.Builder, s string, check func()) {
	if check == nil {
		out.WriteString(s)
		return
	}
	for len(s) > 0 {
		check()
		n := min(len(s), 256)
		out.WriteString(s[:n])
		s = s[n:]
	}
}

// source records the tokens consumed by a production and transfers ownership
// to the enclosing production when source boundaries are shared. Diagnostic
// token positions remain independent of the complete syntax span.
func (p *parser) sourceNode(node any, start int) {
	if n, ok := node.(ast.SourceNode); ok {
		p.source(n, start)
	}
}

func (p *parser) source(node ast.SourceNode, start int) {
	s := node.Source()
	pos := p.toks[start].pos
	old := s.Diagnostic()
	if old.IsValid() && old != pos {
		s.DiagnosticPos = old
	} else {
		s.DiagnosticPos = ast.Pos{}
	}
	s.Pos = pos
	s.End = pos
	if p.i > start {
		s.End = p.toks[p.i-1].end
	}
	if b := p.toks[start].source; b != nil && b.hasNext && !b.header {
		if b.leadingOwner != s {
			if old := b.leadingOwner; old != nil {
				old.LeadingComments, old.LeadingDetachedComments = "", nil
			}
			s.LeadingComments = b.leading
			s.LeadingDetachedComments = b.detached
			b.leadingOwner = s
		}
	}
	p.sourceTrailing(s)
}

func (p *parser) sourceTrailing(s *ast.SourceInfo) {
	if b := p.toks[p.i].source; b != nil && b.hasTrailing && !b.header && b.trailingOwner != s {
		if old := b.trailingOwner; old != nil {
			old.TrailingComments = nil
		}
		// The comments after a semicolon follow the ones before it, when the node owns them.
		if p.i > 0 && p.toks[p.i-1].kind == tPunct && p.toks[p.i-1].text == ";" {
			if before := p.toks[p.i-1].source; before != nil && before.trailingOwner == s {
				s.TrailingComments = append(s.TrailingComments, b.trailing...)
				b.trailingOwner = s
				return
			}
		}
		s.TrailingComments = append([]string(nil), b.trailing...)
		b.trailingOwner = s
	}
}

// layoutOf returns the layout of the rule expression e, which it creates.
func layoutOf(e any) *ast.SourceLayout {
	s := e.(ast.SourceNode).Source()
	if s.Layout == nil {
		s.Layout = &ast.SourceLayout{}
	}
	return s.Layout
}
