package grammar

import (
	"fmt"
	"strings"

	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// SyntaxError is an error in the grammar syntax of a source: a lexical error, a syntax error, or
// an error of a check that the parser makes on the source, such as an invalid escape sequence.
type SyntaxError struct {
	// Pos is where the error is: a 1-based line and a 1-based column counted in characters, where
	// a tab is one character, as in the positions of the grammar AST.
	Pos ast.Pos
	// End is the position just after the text that the error covers, such as an invalid escape
	// sequence or a run of characters that start no token. It is the zero Pos for an error at the
	// point Pos, such as an expected token that is missing.
	End ast.Pos
	// Msg is the message, without the position.
	Msg string
}

// Error returns the position and the message as "line:column: message".
func (e *SyntaxError) Error() string { return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Col, e.Msg) }

// SyntaxErrors is the list of errors of a source in grammar syntax that Load rejects; it is never
// empty. The lexical errors come first, then the other errors in the order of the source. A source
// with more than 100 errors gives 100 of them and one more whose message is "too many errors", at
// the position where the errors left out begin.
//
// Load returns the list itself, so a program obtains it with errors.As:
//
//	var errs grammar.SyntaxErrors
//	if errors.As(err, &errs) {
//		for _, e := range errs {
//			// e.Pos, e.End, e.Msg
//		}
//	}
//
// The list does not unwrap to its elements: errors.As always gives the whole list, never one error.
type SyntaxErrors []*SyntaxError

// Error returns the errors, one per line.
func (l SyntaxErrors) Error() string {
	msgs := make([]string, len(l))
	for i, e := range l {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

// syntaxErrors returns the errors of the parser as a SyntaxErrors that the caller owns.
func syntaxErrors(list parser.ErrorList) SyntaxErrors {
	out := make(SyntaxErrors, len(list))
	for i, e := range list {
		out[i] = &SyntaxError{Pos: e.Pos, End: e.End, Msg: e.Msg}
	}
	return out
}
