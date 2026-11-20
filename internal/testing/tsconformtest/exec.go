package tsconformtest

import (
	"zeroparse.dev/zp/internal/ir/code/exec/conform"
	"zeroparse.dev/zp/internal/ir/code/exec/ref"
)

// ExecOps are the operations of the memo, the deferred memoization, the budget, the variable
// environment and the recovered errors that the TypeScript library implements: the library has no
// streams and no Document memo here (a Document parse is checked with the Document family).
var ExecOps = []string{
	"memo.lookup", "memo.store", "memo.ok", "memo.end", "memo.silent", "seen.first",
	"env.lookup", "env.bind", "env.scope", "env.restore", "env.key",
	"rec.mark", "rec.push", "rec.truncate", "rec.since", "rec.add", "rec.none",
}

// ExecBudgetOps are the operations of the budget, which a controlled parse provides: the charge of a
// memo key (budget.chargeKey) among them.
var ExecBudgetOps = []string{"budget.charge", "budget.used", "budget.chargeKey"}

// Exec is the family of the memo, the environment and the recovered errors.
func Exec(controlled bool) Family {
	return Family{
		Name:       "exec",
		Controlled: controlled,
		Record:     func(r *Recorder) conform.Impl { return execRecorder{r} },
		Replay: func(p *Replay) conform.Impl {
			if controlled {
				return execMetered{execReplay{p}}
			}
			return execReplay{p}
		},
	}
}

type execRecorder struct{ *Recorder }

func (e execRecorder) Values(id int64) any {
	return e.Make(conform.ValuesOp, []any{id}, e.Model.Values(id))
}
func (e execRecorder) Datum(id int64) any {
	return e.Make(conform.DatumOp, []any{id}, ref.Datum{ID: id})
}
func (e execRecorder) Rule(i int64) any {
	return e.Make(conform.RuleOp, []any{i}, ref.RuleRef{Index: i})
}
func (e execRecorder) Err(id int64) any { return e.Make(conform.ErrOp, []any{id}, &ref.Err{ID: id}) }

// Recovered is the model's: the recorder agrees with it. The lists of the record are not unrolled.
func (e execRecorder) Recovered() []int64          { return e.Model.Recovered() }
func (e execRecorder) Unrolled() int               { return 0 }
func (e execRecorder) List(any) (visits, errs int) { return 0, 0 }

type execReplay struct{ *Replay }

func (e execReplay) Values(id int64) any { return e.Make(conform.ValuesOp) }
func (e execReplay) Datum(id int64) any  { return e.Make(conform.DatumOp) }
func (e execReplay) Rule(i int64) any    { return e.Make(conform.RuleOp) }
func (e execReplay) Err(id int64) any    { return e.Make(conform.ErrOp) }

func (e execReplay) Recovered() []int64            { return e.RecIDs() }
func (e execReplay) Unrolled() int                 { return e.RecUnrolled() }
func (e execReplay) List(l any) (visits, errs int) { return e.ListOf(l) }

type execMetered struct{ execReplay }

func (e execMetered) Used() uint64 { return e.Units() }
