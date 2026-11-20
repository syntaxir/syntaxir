package tsconformtest

import (
	"slices"

	"zeroparse.dev/zp/internal/ir/code/exec/conform"
)

// OprSuiteOps are the operations of the Operators Rule family that the library runs: the descriptors and the
// matches of operators, with the support operations that the cases call. The records of the split forms of the
// loop (oprLongest, oprNud and oprExpr) are left out: the TypeScript target has no implementation of them
// (the loop of the library keeps them in locals), as print.OperationMethods reports.
var OprSuiteOps = append(slices.Clone(conform.OprDescriptorOps), conform.OprSupportOps...)

// Opr is the family of the Operators Rule descriptors, the matches of operators and the records of the
// split forms of their loop. The registers are the position of the parser, which a match ends at.
func Opr() Family {
	return Family{
		Name:   "opr",
		Record: func(r *Recorder) conform.Impl { return frameRecorder{execRecorder{r}} },
		Replay: func(p *Replay) conform.Impl { return frameReplay{execReplay{p}} },
	}
}
