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
// them in the batch Parser: those of every family but the operations that cases use to make a handle
// (test.*). The doc operations are printed in each parser class (DocClasses), since each class prints
// them differently.
func printedOps() map[string][]string {
	var all []string
	for _, ops := range [][]string{ExecOps, ExecBudgetOps, FrameOps, ExpOps, CtxOps, RuleOps, OprSuiteOps, TermOps, ObsOps} {
		for _, o := range ops {
			if !strings.HasPrefix(o, "test.") && !slices.Contains(all, o) {
				all = append(all, o)
			}
		}
	}
	slices.Sort(all)
	out := map[string][]string{"PrintedParser": slices.Concat(all, DocOps)}
	for _, c := range DocClasses[1:] {
		out[c] = DocOps
	}
	return out
}

var (
	printedOnce sync.Once
	printedSrc  print.OperationHarness
	printedErr  error
)

// Printed returns the printed code of the operations of the families (print.OperationMethods), in the
// classes PrintedParser, PrintedDocumentParser and PrintedObservedParser.
func Printed() (print.OperationHarness, error) {
	printedOnce.Do(func() { printedSrc, printedErr = print.OperationMethods(printedOps()) })
	return printedSrc, printedErr
}
