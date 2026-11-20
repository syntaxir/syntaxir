package tsconformtest

import (
	"zeroparse.dev/zp/internal/ir/code/exec/conform"
	"zeroparse.dev/zp/internal/ir/code/exec/ref"
)

// Term is the family of what terms make: the elements gathered for the lists and tuples, and the
// values made. After each call that writes them, the harness compares the identifiers of what the
// library holds with the model's.
func Term(controlled bool) Family {
	return Family{
		Name:       "term",
		Controlled: controlled,
		Record:     func(r *Recorder) conform.Impl { return termRecorder{execRecorder{r}} },
		Replay: func(p *Replay) conform.Impl {
			if controlled {
				return termMetered{termReplay{execReplay{p}}}
			}
			return termReplay{execReplay{p}}
		},
	}
}

type termRecorder struct{ execRecorder }

func (t termRecorder) Act() any { return t.Make(conform.ActOp, nil, ref.Act{}) }
func (t termRecorder) Elem(id int64) any {
	return t.Make(conform.ElemOp, []any{id}, ref.Elem{ID: id})
}
func (t termRecorder) Items(ids []int64) any {
	first := int64(0)
	if len(ids) > 0 {
		first = ids[0]
	}
	return t.Make(conform.ItemsOp, []any{first, int64(len(ids))}, &ref.Items{IDs: ids})
}
func (t termRecorder) Struct(id int64) any {
	return t.Make(conform.StructOp, []any{id}, ref.Struct{ID: id})
}
func (t termRecorder) Gathered() []int64 { return t.Model.Gathered() }
func (t termRecorder) Made() []int64     { return t.Model.Made() }
func (t termRecorder) ItemIDs(items any) []int64 {
	return modelArg(items).(*ref.Items).IDs
}

type termReplay struct{ execReplay }

func (t termReplay) Act() any                  { return t.Make(conform.ActOp) }
func (t termReplay) Elem(id int64) any         { return t.Make(conform.ElemOp) }
func (t termReplay) Items(ids []int64) any     { return t.Make(conform.ItemsOp) }
func (t termReplay) Struct(id int64) any       { return t.Make(conform.StructOp) }
func (t termReplay) Gathered() []int64         { return t.GatheredIDs() }
func (t termReplay) Made() []int64             { return t.MadeIDs() }
func (t termReplay) ItemIDs(items any) []int64 { return t.ItemsOf(items) }

type termMetered struct{ termReplay }

func (e termMetered) Used() uint64 { return e.Units() }

// TermOps are the operations of what terms make that the bindings provide.
var TermOps = append(append([]string{}, conform.GatheredOps...), conform.MadeOps...)
