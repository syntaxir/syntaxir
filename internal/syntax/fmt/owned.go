package fmt

import "strings"

import "syntaxir.dev/sxir/pkg/grammar/ast"

// A grammar decoded from JSON, or built by hand, has no LineBreak values, but
// its nodes may own comments (SourceInfo.LeadingComments, TrailingComments and
// LeadingDetachedComments). Format writes those comments for the nodes that
// stand on lines of their own: the package clause, the declarations, the fields
// of a struct, and the lines of an operators rule. A node that has a LineBreak
// keeps its layout comment tokens instead, so the comments of parsed source
// are never written twice.

// ownedComments returns the comment tokens of text, a normalized comment: the
// lines of a text that ends in a line break are line comments, and any other
// text is one block comment. A space ends text that does not end with white
// space, as the line comment at the end of a file has none.
func ownedComments(text string) []*ast.Comment {
	if text == "" {
		return nil
	}
	if !strings.HasSuffix(text, "\n") {
		body := continuationLines(blockCommentText(text))
		if !strings.HasSuffix(body, " ") {
			body += " "
		}
		return []*ast.Comment{{Text: "/*" + body + "*/"}}
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	cs := make([]*ast.Comment, len(lines))
	for i, line := range lines {
		cs[i] = &ast.Comment{Text: "//" + strings.TrimRight(line, " \t\r")}
	}
	return cs
}

// blockCommentText returns text as the body of one block comment. Block
// comments nest, so the "/*" and "*/" of text that pair up as the lexer reads
// them are kept, which writes the nested comments of parsed source as they
// were. A "*/" that closes no "/*" of text would end the comment, and a "/*"
// that no "*/" closes would leave it open, so they are written "* /" and "/ *",
// with a space after them when the next character would join their last one
// into a delimiter again.
func blockCommentText(text string) string {
	// unpaired holds the offsets of the delimiters that pair with none.
	unpaired := map[int]bool{}
	var open []int
	for i := 0; i+1 < len(text); i++ {
		switch text[i : i+2] {
		case "/*":
			open = append(open, i)
			i++
		case "*/":
			if len(open) > 0 {
				open = open[:len(open)-1]
			} else {
				unpaired[i] = true
			}
			i++
		}
	}
	for _, i := range open {
		unpaired[i] = true
	}
	if len(unpaired) == 0 {
		return text
	}
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		if !unpaired[i] {
			out.WriteByte(text[i])
			continue
		}
		first, second := text[i], text[i+1]
		out.WriteByte(first)
		out.WriteByte(' ')
		out.WriteByte(second)
		if i+2 < len(text) && text[i+2] == first {
			out.WriteByte(' ')
		}
		i++
	}
	return out.String()
}

// continuationLines starts the lines of text after the first with " *", which a
// block comment drops from its text again, so that the white space at the start
// of those lines is kept. A line that starts with "/" is written as it is, as
// " *" would join it into "*/" and it has no white space to keep.
func continuationLines(text string) string {
	lines := strings.Split(text, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" && !strings.HasPrefix(lines[i], "/") {
			lines[i] = " *" + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// ownedLead returns b, or when b is nil the line break that writes the leading
// and detached comments s owns. blank says whether a blank line precedes the
// first comment, as it does between declarations.
func ownedLead(s *ast.SourceInfo, b *ast.LineBreak, blank bool) *ast.LineBreak {
	if b != nil || s.LeadingComments == "" && len(s.LeadingDetachedComments) == 0 {
		return b
	}
	return ownedLeadFrom(s, blank)
}

// ownedLeadFrom builds the break of ownedLead: the detached paragraphs, each
// after a blank line, then the leading comments directly before the node.
func ownedLeadFrom(s *ast.SourceInfo, blank bool) *ast.LineBreak {
	b := &ast.LineBreak{}
	for _, paragraph := range s.LeadingDetachedComments {
		cs := ownedComments(paragraph)
		if len(cs) == 0 {
			continue
		}
		cs[0].Blank = true // a blank line makes the paragraph detached
		b.Comments = append(b.Comments, cs...)
	}
	leading := ownedComments(s.LeadingComments)
	if len(leading) > 0 {
		leading[0].Blank = len(b.Comments) > 0 || blank
		b.Comments = append(b.Comments, leading...)
	} else {
		b.Blank = len(b.Comments) > 0
	}
	return b
}

// ownedTrailing returns the break that writes the trailing comment s owns at
// the end of its line, or nil. Each string is one comment, written after the
// one before it; a comment over several lines is one block comment.
func ownedTrailing(s *ast.SourceInfo, b *ast.LineBreak) *ast.LineBreak {
	if b != nil || len(s.TrailingComments) == 0 {
		return nil
	}
	var texts []string
	for _, text := range s.TrailingComments {
		cs := ownedComments(text)
		if len(cs) == 0 {
			continue
		}
		if len(cs) > 1 {
			// A group over several lines is one block comment.
			cs = []*ast.Comment{{Text: "/*" + continuationLines(blockCommentText(text)) + "*/"}}
		}
		texts = append(texts, cs[0].Text)
	}
	if len(texts) == 0 {
		return nil
	}
	c := &ast.Comment{Text: strings.Join(texts, " ")}
	return &ast.LineBreak{Trailing: c, TrailingEnds: strings.HasPrefix(texts[len(texts)-1], "//")}
}

// ownsComments reports whether Format writes comments that s owns, because b
// is nil.
func ownsComments(s *ast.SourceInfo, b *ast.LineBreak) bool {
	return b == nil && (s.LeadingComments != "" || len(s.TrailingComments) > 0 || len(s.LeadingDetachedComments) > 0)
}

// inlineOwnedComment returns the block comment that s owns when it is only one
// detached block comment without a line break at its end, the comment that
// the parser detaches from two tokens of one line, or nil.
func inlineOwnedComment(s *ast.SourceInfo) *ast.Comment {
	if s.LeadingComments != "" || len(s.LeadingDetachedComments) != 1 || strings.HasSuffix(s.LeadingDetachedComments[0], "\n") {
		return nil
	}
	cs := ownedComments(s.LeadingDetachedComments[0])
	if len(cs) == 0 {
		return nil
	}
	return cs[0]
}

// inlineHeaderComments returns the comments of texts, normalized comments, written to stand on
// the line of a declaration or field with a space after each, or "". A comment that ends in a line
// break is written as a block comment.
func inlineHeaderComments(texts []string) string {
	var out strings.Builder
	for _, text := range texts {
		out.WriteString(headerComment(strings.TrimSuffix(text, "\n")).Text + " ")
	}
	return out.String()
}

// headerComment returns the comment of text, one normalized comment of a header, which holds no
// line break: a block comment, and /**/ for the empty text, which the parser reads back as it.
func headerComment(text string) *ast.Comment {
	if text == "" {
		return &ast.Comment{Text: "/**/"}
	}
	return ownedComments(text)[0]
}
