package tsconformtest

import (
	"zeroparse.dev/zp/internal/ir/code/exec/conform"
)

// CtxOps are the operations that a suite of the dependency memo provides: those of
// conform.CtxSuiteOps without the operations of a Document parse.
var CtxOps = conform.CtxSuiteOps()

// Ctx is the family of the dependency memo of left recursion and of the growths.
func Ctx(controlled bool) Family {
	return Family{
		Name:       "ctx",
		Controlled: controlled,
		Record:     func(r *Recorder) conform.Impl { return ctxRecorder{expRecorder{execRecorder{r}}} },
		Replay: func(p *Replay) conform.Impl {
			if controlled {
				return ctxMetered{ctxReplay{expReplay{execReplay{p}}}}
			}
			return ctxReplay{expReplay{execReplay{p}}}
		},
	}
}

type ctxRecorder struct{ expRecorder }

func (c ctxRecorder) Handle(name string) any {
	return c.Make(name, nil, c.Model.OprHandle(name))
}
func (c ctxRecorder) UseRegisters(regs map[string]any) { c.Registers(regs) }

type ctxReplay struct{ expReplay }

func (c ctxReplay) Handle(name string) any           { return c.Make(name) }
func (c ctxReplay) UseRegisters(regs map[string]any) { c.Registers(regs) }

type ctxMetered struct{ ctxReplay }

func (c ctxMetered) Used() uint64 { return c.Units() }
