package libgo

// The probes of the conformance test of the Flow operations (flow_conform_test.go), added to a copy
// of the library: generated code calls ConfPre before each call of a primitive operation and one of
// the pops after it, which record the call with the state of the parser around it.

import (
	"fmt"
	"reflect"

	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// ConfEvent is a call of a primitive operation. K identifies the call in the generated code; Used
// counts the units of budget charged, and Nested those charged by the probed calls inside the call.
type ConfEvent struct {
	// ArgFrameSet reports that a frame argument has a value set in one of its slots.
	ArgFrameSet         bool
	K                   int
	Pos, PosAfter       int
	Used, UsedAfter     uint64
	Nested              uint64
	Kids, KidsAfter     int
	Rec, RecAfter       int
	Silent, SilentAfter int
	// Far and Exps are the farthest failure and the number of expectations of the current record;
	// Frame and Env identify the current capture frame and variable environment ("nil" or "ptr:" and
	// the address), and Vals are the recorded forms of the values of the frame.
	Far, FarAfter     int
	Exps, ExpsAfter   int
	Frame, FrameAfter string
	Env, EnvAfter     string
	Vals, ValsAfter   []any
	// Created is the number of structs that the action being evaluated has made (the length of the
	// parser's list of them), and Fresh whether the first argument, if a node, is still owned by its
	// maker ("true" or "false", "" for any other value).
	Created, CreatedAfter int
	Fresh, FreshAfter     string
	Args                  map[int]any
	Results               []any
	Done                  bool
	Parent                int
	p                     any
	first                 any
}

// ConfEvents are the calls in the order they began; ConfReset clears them.
var (
	ConfEvents []*ConfEvent
	confStack  []*ConfEvent
)

// ConfReset clears the record for a new parse.
func ConfReset() { ConfEvents, confStack = nil, nil }

// confSnap is the state of the parser that a probe records around a call.
type confSnap struct {
	pos        int
	used       uint64
	kids, rec  int
	silent     int
	far, exps  int
	frame, env string
	vals       []any
	created    int
}

func confPtr(x any) string {
	if r := reflect.ValueOf(x); !r.IsValid() || r.IsNil() {
		return "nil"
	}
	return fmt.Sprintf("ptr:%p", x)
}

func confState(p any) (s confSnap) {
	var q *Parser
	switch x := p.(type) {
	case *Parser:
		q, s.kids, s.created = x, len(x.KidStack), len(x.created)
		s.frame = confPtr(x.Frame)
		if x.Frame != nil {
			for _, v := range x.Frame.Vals {
				s.vals = append(s.vals, confVal(v))
			}
		}
	case *TParser:
		q, s.kids, s.created = x.Parser, len(x.KidStack), len(x.Created)
		s.frame = confPtr(x.Frame)
		if x.Frame != nil {
			for _, v := range x.Frame.Vals {
				s.vals = append(s.vals, confVal(v))
			}
		}
	default:
		panic(fmt.Sprintf("probe: parser %T", p))
	}
	if q.Control != nil {
		s.used = q.Control.budgetUsed()
	}
	s.pos, s.rec, s.silent = q.Pos, len(q.Recovered), q.Silent
	s.far, s.exps = q.farthest, len(q.exp)-q.expBase
	s.env = confPtr(q.Env)
	return s
}

// ConfPre begins the call k on the parser p and returns p.
func ConfPre[P any](p P, k int) P {
	e := &ConfEvent{K: k, Args: map[int]any{}, p: p, Parent: -1}
	s := confState(p)
	e.Pos, e.Used, e.Kids, e.Rec, e.Silent = s.pos, s.used, s.kids, s.rec, s.silent
	e.Far, e.Exps, e.Frame, e.Env, e.Vals = s.far, s.exps, s.frame, s.env, s.vals
	e.Created = s.created
	if n := len(confStack); n > 0 {
		e.Parent = confStack[n-1].K
	}
	ConfEvents = append(ConfEvents, e)
	confStack = append(confStack, e)
	return p
}

// ConfArg records x as the argument i of the call in progress and returns it.
func ConfArg[T any](i int, x T) T {
	e := confStack[len(confStack)-1]
	e.Args[i] = confVal(x)
	switch f := any(x).(type) {
	case *CaptureFrame:
		if f == nil {
			break
		}
		for _, v := range f.Vals {
			e.ArgFrameSet = e.ArgFrameSet || v != nil
		}
	case *TCaptureFrame:
		if f == nil {
			break
		}
		for _, v := range f.Vals {
			e.ArgFrameSet = e.ArgFrameSet || v != nil
		}
	}
	if i == 0 {
		e.first, e.Fresh = x, confFresh(x)
	}
	return x
}

// ConfPost ends the call in progress, with the results xs.
func ConfPost(xs ...any) {
	n := len(confStack) - 1
	e := confStack[n]
	confStack = confStack[:n]
	s := confState(e.p)
	e.PosAfter, e.UsedAfter, e.KidsAfter, e.RecAfter, e.SilentAfter = s.pos, s.used, s.kids, s.rec, s.silent
	e.FarAfter, e.ExpsAfter, e.FrameAfter, e.EnvAfter, e.ValsAfter = s.far, s.exps, s.frame, s.env, s.vals
	e.CreatedAfter = s.created
	if e.first != nil {
		e.FreshAfter = confFresh(e.first)
	}
	for _, x := range xs {
		e.Results = append(e.Results, confVal(x))
	}
	e.Done = true
	if n > 0 {
		confStack[n-1].Nested += e.UsedAfter - e.Used
	}
}

// Conf1 ends the call in progress, which ConfPre began in its first argument, with the result x.
func Conf1[T any](_ any, x T) T {
	ConfPost(x)
	return x
}

// ConfNot ends the call in progress, whose negation x is, and returns x.
func ConfNot(_ any, x bool) bool {
	ConfPost(!x)
	return x
}

// ConfPop2 and ConfPop3 end the call in progress with its results.
func ConfPop2[A, B any](a A, b B) (A, B) {
	ConfPost(a, b)
	return a, b
}

func ConfPop3[A, B, C any](a A, b B, c C) (A, B, C) {
	ConfPost(a, b, c)
	return a, b, c
}

// confFresh reports whether the node x is still owned by its maker, as "true" or "false"; "" for a
// value that is not a node.
func confFresh(x any) string {
	switch v := x.(type) {
	case *sxirvalue.Node:
		if v != nil {
			return fmt.Sprint(D(v).Fresh)
		}
	case interface{ TFresh() bool }:
		if r := reflect.ValueOf(x); r.Kind() != reflect.Pointer || !r.IsNil() {
			return fmt.Sprint(v.TFresh())
		}
	}
	return ""
}

// confVal returns the recorded form of a value: integers, runes and booleans as they are, a string
// as "s:" and its text, a list as "len:" and its length, a mark as "mark:" and its position, the
// context of an action as "ctx:" and the start of the values it made, followed for a typed one by
// ":made:" and their number, an error
// as "err:" and its message, a node value as "val:" and its type, start, end, number of children
// and, for a string or an error, its text (separated by colons, the text last), and any other value
// as its type, or nil.
func confVal(x any) any {
	switch v := x.(type) {
	case nil:
		return nil
	case bool:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case string:
		return "s:" + v
	case Mark:
		return fmt.Sprintf("mark:%d", v.Pos)
	case *CaptureFrame:
		return confPtr(v)
	case *TCaptureFrame:
		return confPtr(v)
	case *Env:
		return confPtr(v)
	case *TActCtx:
		// The context of an action, as the start of the values it made and their number.
		return fmt.Sprintf("ctx:%d:made:%d", v.Cbase, len(v.P.Created)-v.Cbase)
	case *ActCtx:
		return fmt.Sprintf("ctx:%d", v.cbase)
	case *sxirvalue.Node:
		if v == nil {
			return nil
		}
		kids := 0
		for range v.Elems() {
			kids++
		}
		text := ""
		if typ := v.Type(); typ == "string" || typ == "error" {
			text = v.Text()
		}
		return fmt.Sprintf("val:%s:%d:%d:%d:%s", v.Type(), v.Start(), v.End(), kids, text)
	case interface {
		TSpan() (int, int)
		TName() string
		TText() (string, bool)
		TKids() []any
	}:
		if r := reflect.ValueOf(x); r.Kind() == reflect.Pointer && r.IsNil() {
			return nil
		}
		start, end := v.TSpan()
		text, _ := v.TText()
		return fmt.Sprintf("val:%s:%d:%d:%d:%s", v.TName(), start, end, len(v.TKids()), text)
	case error:
		if r := reflect.ValueOf(x); r.Kind() == reflect.Pointer && r.IsNil() {
			return nil
		}
		return "err:" + v.Error()
	}
	r := reflect.ValueOf(x)
	switch r.Kind() {
	case reflect.Slice:
		return fmt.Sprintf("len:%d", r.Len())
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func:
		if r.IsNil() {
			return nil
		}
	}
	return fmt.Sprintf("%T", x)
}

// ConfAppendLoses is append that drops the elements it appends: the broken operation that the mutants
// of the conformance test of flow.append put in the printed form.
func ConfAppendLoses[T any](s []T, xs ...T) []T { return append(s, xs...)[:len(s)] }
