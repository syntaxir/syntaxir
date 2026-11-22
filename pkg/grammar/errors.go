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

// CheckError is an error that the check of a grammar finds in a grammar that loads: a name that is
// not defined, a type that does not check, an attribute with an invalid argument or target, or
// another rule of the language that the syntax does not decide.
type CheckError struct {
	// Pos is where the error is: a 1-based line and a 1-based column counted in characters, as in
	// the positions of the grammar AST. It is the zero Pos when the error has no position in the
	// source, such as an error of a grammar AST that a program built without positions.
	Pos ast.Pos
	// Msg is the message, without the position.
	Msg string
}

// Error returns the position and the message as "line:column: message", or only the message when
// the position is not known.
func (e *CheckError) Error() string {
	if e.Pos.IsValid() {
		return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Col, e.Msg)
	}
	return e.Msg
}

// CheckErrors is the list of errors of a grammar that loads but does not check; it is never empty.
// The errors are in the order in which the check reports them.
//
// Every tool that checks a grammar returns the list itself for it: the functions of package
// compiler that compile a grammar, the linter of package lint and the edit fuzzer of package
// editfuzz. A program obtains it with errors.As:
//
//	var errs grammar.CheckErrors
//	if errors.As(err, &errs) {
//		for _, e := range errs {
//			// e.Pos, e.Msg
//		}
//	}
//
// A source that does not load gives the SyntaxErrors of Load instead. The list does not unwrap to
// its elements: errors.As always gives the whole list, never one error.
type CheckErrors []*CheckError

// Error returns the errors, one per line.
func (l CheckErrors) Error() string {
	msgs := make([]string, len(l))
	for i, e := range l {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}
