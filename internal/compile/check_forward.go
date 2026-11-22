package compile

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/internal/frontend/check"
)

// The functions and methods of this file run the checker of internal/frontend/check over a
// program: each passes the program's checker (Program.Checker) to the function of the same name.

// CheckTypes is check.CheckTypes over p, whose Typed it sets.
func CheckTypes(prog *Program, errs *diagnostic.ErrorList) { CheckTypesControlled(prog, errs, nil) }

// CheckTypesControlled is CheckTypes polling control; a nil control never cancels.
func CheckTypesControlled(prog *Program, errs *diagnostic.ErrorList, control *cancel.Control) {
	c := prog.Checker()
	check.CheckTypesControlled(c, errs, control)
	prog.Typed = c.Typed
}
