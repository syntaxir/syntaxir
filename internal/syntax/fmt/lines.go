package fmt

import "strings"

// Line terminators, as the lexical specification defines them: a line ends at
// LF, CRLF or CR. Format writes the comment strings of an AST by this
// definition, as the parser reads them, and ends each line it writes with LF.

// endsInLineTerminator reports whether text ends in a line terminator.
func endsInLineTerminator(text string) bool {
	return strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")
}

// trimLineEnd returns text without the line terminator that ends it, if any.
func trimLineEnd(text string) string {
	if t, ok := strings.CutSuffix(text, "\r\n"); ok {
		return t
	}
	if endsInLineTerminator(text) {
		return text[:len(text)-1]
	}
	return text
}

// splitLines splits text at its line terminators: line i is followed by the
// terminator ends[i], and the last line by none ("").
func splitLines(text string) (lines, ends []string) {
	for {
		i := strings.IndexAny(text, "\n\r")
		if i < 0 {
			return append(lines, text), append(ends, "")
		}
		n := 1
		if text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n' {
			n = 2
		}
		lines, ends = append(lines, text[:i]), append(ends, text[i:i+n])
		text = text[i+n:]
	}
}
