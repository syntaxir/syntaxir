package tsconformtest

import (
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
)

// Action is the family of what action expressions make: the elements gathered for the lists and tuples, and the
// values made. After each call that writes them, the harness compares the identifiers of what the
// library holds with the model's.
func Action(controlled bool) Family {
	return Family{
		Name:       "action",
		Controlled: controlled,
		Record:     func(r *Recorder) conform.Impl { return actionRecorder{execRecorder{r}} },
		Replay: func(p *Replay) conform.Impl {
			if controlled {
				return actionMetered{actionReplay{execReplay{p}}}
			}
			return actionReplay{execReplay{p}}
		},
	}
}

type actionRecorder struct{ execRecorder }

func (t actionRecorder) Act() any { return t.Make(conform.ActOp, nil, ref.Act{}) }
func (t actionRecorder) Elem(id int64) any {
	return t.Make(conform.ElemOp, []any{id}, ref.Elem{ID: id})
}
func (t actionRecorder) Items(ids []int64) any {
	first := int64(0)
	if len(ids) > 0 {
		first = ids[0]
	}
	return t.Make(conform.ItemsOp, []any{first, int64(len(ids))}, &ref.Items{IDs: ids})
}
func (t actionRecorder) Struct(id int64) any {
	return t.Make(conform.StructOp, []any{id}, ref.Struct{ID: id})
}
func (t actionRecorder) Gathered() []int64 { return t.Model.Gathered() }
func (t actionRecorder) Made() []int64     { return t.Model.Made() }
func (t actionRecorder) ItemIDs(items any) []int64 {
	return modelArg(items).(*ref.Items).IDs
}

type actionReplay struct{ execReplay }

func (t actionReplay) Act() any                  { return t.Make(conform.ActOp) }
func (t actionReplay) Elem(id int64) any         { return t.Make(conform.ElemOp) }
func (t actionReplay) Items(ids []int64) any     { return t.Make(conform.ItemsOp) }
func (t actionReplay) Struct(id int64) any       { return t.Make(conform.StructOp) }
func (t actionReplay) Gathered() []int64         { return t.GatheredIDs() }
func (t actionReplay) Made() []int64             { return t.MadeIDs() }
func (t actionReplay) ItemIDs(items any) []int64 { return t.ItemsOf(items) }

type actionMetered struct{ actionReplay }

func (e actionMetered) Used() uint64 { return e.Units() }

// ActionOps are the operations of what action expressions make that the bindings provide.
var ActionOps = append(append([]string{}, conform.GatheredOps...), conform.MadeOps...)
