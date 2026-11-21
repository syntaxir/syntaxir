package tsconformtest

import (
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
)

// ExpOps are the operations of the expectation record and of the memo that keep the expectations of an
// entry that the library implements: those of conform.ExpOps without the operations of a Document
// memo (memo.examine, memo.splice), which a parse of a Document has.
var ExpOps = []string{
	"exp.isolate", "exp.unisolate", "exp.farthest", "exp.recording", "exp.reset", "exp.fresh",
	"exp.placeholder", "exp.maybe", "exp.count", "exp.at", "exp.add", "exp.setCount", "exp.setAt",
	"exp.keptCount", "exp.keptAt", "exp.empty", "exp.last", "exp.copy",
	"memo.far", "memo.expected", "memo.setExpected",
	"memo.lookup", "memo.store",
}

// Exp is the family of the expectation record.
func Exp(controlled bool) Family {
	return Family{
		Name:       "exp",
		Controlled: controlled,
		Record:     func(r *Recorder) conform.Impl { return expRecorder{execRecorder{r}} },
		Replay: func(p *Replay) conform.Impl {
			if controlled {
				return expMetered{expReplay{execReplay{p}}}
			}
			return expReplay{execReplay{p}}
		},
	}
}

type expRecorder struct{ execRecorder }

func (e expRecorder) Exp(id int64) any { return e.Make(conform.ExpOp, []any{id}, ref.ExpID{ID: id}) }
func (e expRecorder) Placeholder() any { return e.Make(conform.PlaceholderOp, nil, ref.Placeholder) }

type expReplay struct{ execReplay }

func (e expReplay) Exp(id int64) any { return e.Make(conform.ExpOp) }
func (e expReplay) Placeholder() any { return e.Make(conform.PlaceholderOp) }

type expMetered struct{ expReplay }

func (e expMetered) Used() uint64 { return e.Units() }
