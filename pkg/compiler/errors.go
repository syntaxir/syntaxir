package compiler

import (
	"fmt"
	"strings"

	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// GrammarError is a grammar error that compilation finds in a grammar that loads: a name that is
// not defined, a type that does not check, an attribute with an invalid argument or target, or
// another rule of the language that the syntax does not decide.
type GrammarError struct {
	// Pos is where the error is: a 1-based line and a 1-based column counted in characters, as in
	// the positions of the grammar AST. It is the zero Pos when the error has no position in the
	// source, such as an error of a grammar AST that a program built without positions.
	Pos ast.Pos
	// Msg is the message, without the position.
	Msg string
}

// Error returns the position and the message as "line:column: message", or only the message when
// the position is not known.
func (e *GrammarError) Error() string {
	if e.Pos.IsValid() {
		return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Col, e.Msg)
	}
	return e.Msg
}

// GrammarErrors is the list of grammar errors of a grammar that does not compile; it is never
// empty. The errors are in the order in which compilation reports them.
//
// Every function of this package that compiles a grammar returns the list itself when the grammar
// loads but does not compile, so a program obtains it with errors.As:
//
//	var errs compiler.GrammarErrors
//	if errors.As(err, &errs) {
//		for _, e := range errs {
//			// e.Pos, e.Msg
//		}
//	}
//
// A grammar source that does not load gives the errors of grammar.Load instead, such as
// grammar.SyntaxErrors. The list does not unwrap to its elements: errors.As always gives the
// whole list, never one error.
type GrammarErrors []*GrammarError

// Error returns the errors, one per line.
func (l GrammarErrors) Error() string {
	msgs := make([]string, len(l))
	for i, e := range l {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

// grammarErrors returns err with the errors of the compiler, which it returns as a
// diagnostic.ErrorList, as GrammarErrors that the caller owns; it returns any other error as it
// is.
func grammarErrors(err error) error {
	list, ok := err.(diagnostic.ErrorList)
	if !ok {
		return err
	}
	out := make(GrammarErrors, len(list))
	for i, e := range list {
		out[i] = &GrammarError{Pos: e.Pos, Msg: e.Msg}
	}
	return out
}
