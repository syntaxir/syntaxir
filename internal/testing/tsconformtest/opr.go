package tsconformtest

import (
	"zeroparse.dev/zp/internal/ir/code/exec/conform"
)

// OprSuiteOps are the operations of the Operators Rule family that the bindings provide, the
// descriptors, matches and records of the loop with the support operations that the cases call.
var OprSuiteOps = conform.OprSuiteOps()

// Opr is the family of the Operators Rule descriptors, the matches of operators and the records of the
// split forms of their loop. The registers are the position of the parser, which a match ends at.
func Opr() Family {
	return Family{
		Name:   "opr",
		Record: func(r *Recorder) conform.Impl { return frameRecorder{execRecorder{r}} },
		Replay: func(p *Replay) conform.Impl { return frameReplay{execReplay{p}} },
	}
}
