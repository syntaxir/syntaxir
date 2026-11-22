package compiler

import (
	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/pkg/grammar"
)

// grammarErrors returns err with the errors of a grammar that does not check, which the compiler
// returns as a diagnostic.ErrorList, as the grammar.CheckErrors that the caller owns; it returns any
// other error as it is.
func grammarErrors(err error) error {
	list, ok := err.(diagnostic.ErrorList)
	if !ok {
		return err
	}
	out := make(grammar.CheckErrors, len(list))
	for i, e := range list {
		out[i] = &grammar.CheckError{Pos: e.Pos, Msg: e.Msg}
	}
	return out
}
