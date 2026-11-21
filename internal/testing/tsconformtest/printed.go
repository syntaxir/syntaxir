package tsconformtest

import (
	"slices"
	"strings"
	"sync"

	"syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
)

// ObsOps are the operations of the observation of a parse.
var ObsOps = conform.ObsOps

// printedOps are the operations of the families that the bindings run as the TypeScript printer prints
// them: those of every family but the operations that cases use to make a handle (test.*).
func printedOps() []string {
	var all []string
	for _, ops := range [][]string{ExecOps, ExecBudgetOps, FrameOps, ExpOps, CtxOps, RuleOps, OprSuiteOps, TermOps, ObsOps} {
		for _, o := range ops {
			if !strings.HasPrefix(o, "test.") && !slices.Contains(all, o) {
				all = append(all, o)
			}
		}
	}
	slices.Sort(all)
	return all
}

var (
	printedOnce sync.Once
	printedSrc  print.OperationHarness
	printedErr  error
)

// Printed returns the printed code of the operations of the families (print.OperationMethods).
func Printed() (print.OperationHarness, error) {
	printedOnce.Do(func() { printedSrc, printedErr = print.OperationMethods(printedOps()) })
	return printedSrc, printedErr
}
