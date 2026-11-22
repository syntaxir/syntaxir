// Package print is a fake printer that breaks the import rules of the printers: it imports the
// driver, a pass package, the planning code under another name and the action lowering, and uses the
// data packages and the result package that a printer may import.
package print

import (
	"example.com/print/internal/driver"
	"example.com/print/internal/driver/result"
	"example.com/print/internal/middleend/actionlower"
	"example.com/print/internal/middleend/codepass"
	"example.com/print/internal/middleend/flowlower"
	"example.com/print/internal/middleend/flowlower/lowerdata"
	plan "example.com/print/internal/middleend/flowplan"
	"example.com/print/internal/middleend/flowplan/plandata"
)

var (
	_ = driver.Compile
	_ = result.IR
	_ = codepass.Specialize
	_ = actionlower.Lower
	_ = flowlower.Term
	_ lowerdata.Options
	_ = plan.Planning
	_ plandata.Program
)
