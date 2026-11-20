package tsconformtest

import (
	"zeroparse.dev/zp/internal/ir/code/exec/conform"
)

// FrameOps are the operations of the capture frames that the library implements, with the support
// operations that the cases call to change what a backtracking point holds (conform.FrameSuiteOps without
// the missing-token record, which the library does not have).
var FrameOps = conform.FrameSuiteOps(false)

// Frame is the family of the capture frames: the frame operations over the library's frame, trail and
// backtracking points. A case sets the position register, which the library reads as its own position.
func Frame(controlled bool) Family {
	return Family{
		Name:       "frame",
		Controlled: controlled,
		Record:     func(r *Recorder) conform.Impl { return frameRecorder{execRecorder{r}} },
		Replay: func(p *Replay) conform.Impl {
			if controlled {
				return frameMetered{frameReplay{execReplay{p}}}
			}
			return frameReplay{execReplay{p}}
		},
	}
}

type frameRecorder struct{ execRecorder }

func (f frameRecorder) Handle(name string) any {
	return f.Make(name, nil, f.Model.OprHandle(name))
}
func (f frameRecorder) UseRegisters(regs map[string]any) { f.Registers(regs) }

type frameReplay struct{ execReplay }

func (f frameReplay) Handle(name string) any           { return f.Make(name) }
func (f frameReplay) UseRegisters(regs map[string]any) { f.Registers(regs) }

type frameMetered struct{ frameReplay }

func (e frameMetered) Used() uint64 { return e.Units() }
