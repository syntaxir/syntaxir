package ast

import "strings"

// Line terminators, as the lexical specification defines them: a line ends at
// LF, CRLF or CR. The checks of comment strings follow the same definition as
// the parser and Format.

// hasLineTerminator reports whether text holds a line terminator.
func hasLineTerminator(text string) bool { return strings.ContainsAny(text, "\n\r") }

// endsLine reports whether text ends in a line terminator.
func endsLine(text string) bool {
	return strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")
}

// lineTerminators returns the number of line terminators in text, a CRLF
// counting once.
func lineTerminators(text string) int {
	return strings.Count(text, "\n") + strings.Count(text, "\r") - strings.Count(text, "\r\n")
}
