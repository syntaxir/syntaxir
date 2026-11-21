package tsconformtest

import (
	"slices"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
)

// OprSuiteOps are the operations of the Operators Rule family that the library runs: the descriptors, the
// matches of operators, the operations that run the grammar code of a rule and those that make its values,
// with the support operations, the frame operations and the operations that take values apart that the cases
// call. The records of the split forms of the loop (oprLongest, oprNud and oprExpr) are left out: the
// TypeScript target has no implementation of them (the loop of the library keeps them in locals), as
// print.OperationMethods reports. It has no typed values (conform.OprTypedOps).
var OprSuiteOps = slices.Concat(conform.OprDescriptorOps, conform.OprMatchOps, conform.OprValueBuildOps, conform.OprSupportOps,
	conform.OprFrameOps, ref.OprValueOps)

// Opr is the family of the Operators Rule descriptors, the matches of operators and the records of the
// split forms of their loop. The registers are the position of the parser, which a match ends at, and the
// cut flag and the silent depth, which the grammar code of a rule runs with.
func Opr() Family {
	return Family{
		Name:   "opr",
		Record: func(r *Recorder) conform.Impl { return frameRecorder{execRecorder{r}} },
		Replay: func(p *Replay) conform.Impl { return frameReplay{execReplay{p}} },
	}
}
