package tsconformtest

import (
	"errors"
	"fmt"

	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/code/exec/conform"
	"zeroparse.dev/zp/internal/ir/code/exec/ref"
)

// Entry is one call of the trace of a case: an operation of the parser VM, or one of the operations
// that cases use to make handles (conform.ValuesOp and the like).
type Entry struct {
	Op string `json:"op"`
	// Args are the arguments: a number, a boolean, a string, null for a null handle, {"h": id} for the
	// handle that the entry with the result identifier id made, and {"errs": true} for
	// conform.RecoveredErrors.
	Args []any `json:"args"`
	// Res holds, for each result of a handle type, the identifier of the handle in the trace, and null
	// for the other results.
	Res []any `json:"res"`
	// Obs are the state components whose observation the harness compares after the call.
	Obs []string `json:"obs,omitempty"`
}

// Answer is what the library did for one entry.
type Answer struct {
	// Res are the results. A handle is {"h": id} for the identity id that the library gave the value.
	Res []any `json:"res"`
	// Int names the interruption that ended the call: "limit" or "nesting".
	Int string `json:"int,omitempty"`
	// Err is an exception of the library that is not an interruption.
	Err string `json:"err,omitempty"`
	// Used is the budget used after the call.
	Used float64 `json:"used"`
	// Rec are the identifiers of the recovered errors after the call, and Unrolled the number of lists that
	// listing them visits (the family has recovery).
	Rec      []int64 `json:"rec,omitempty"`
	Unrolled int     `json:"unrolled,omitempty"`
	// Lists are, for each result that is a list of recovered errors, its visits and errors.
	Lists [][2]int `json:"lists,omitempty"`
	// Obs are the observations of the components of Entry.Obs.
	Obs map[string][]string `json:"obs,omitempty"`
}

// Tok is a handle of the implementation under conformance: a value that the harness compares by
// identity. In a Recorder it stands for a handle of the model; in a Replay for a value of the library.
type Tok struct {
	id           int64
	m            any
	visits, errs int
}

// Recorder is an implementation that is a second copy of the model: it makes the calls of a trace
// and logs them. The harness finds no difference between it and the model.
type Recorder struct {
	Model *ref.Model
	trace []Entry
	toks  map[any]*Tok
	next  int64
}

// NewRecorder returns a recorder of the case configured by cfg.
func NewRecorder(cfg ref.Config) *Recorder {
	return &Recorder{Model: ref.New(cfg), toks: map[any]*Tok{}}
}

// Trace returns the calls recorded so far.
func (r *Recorder) Trace() []Entry { return r.trace }

// tok returns the handle of the implementation for the model's handle m: one for each distinct handle.
func (r *Recorder) tok(m any) *Tok {
	if m == nil {
		r.next++
		return &Tok{id: r.next}
	}
	t, ok := r.toks[m]
	if !ok {
		r.next++
		t = &Tok{id: r.next, m: m}
		r.toks[m] = t
	}
	return t
}

func encodeArg(a any) any {
	switch a := a.(type) {
	case *Tok:
		return map[string]any{"h": a.id}
	case int64, bool, uint64, string, nil:
		return a
	}
	if a == any(conform.RecoveredErrors) {
		return map[string]any{"errs": true}
	}
	panic(fmt.Sprintf("tsconformtest: no encoding of the argument %T", a))
}

func modelArg(a any) any {
	if t, ok := a.(*Tok); ok {
		return t.m
	}
	return a
}

// Make logs a call of an operation that cases use to make a handle, with the arguments args, whose
// handle on the model is m, and returns the handle of the implementation.
func (r *Recorder) Make(op string, args []any, m any) *Tok {
	t := r.tok(m)
	e := Entry{Op: op, Args: []any{}, Res: []any{t.id}}
	for _, a := range args {
		e.Args = append(e.Args, encodeArg(a))
	}
	r.trace = append(r.trace, e)
	return t
}

// Call makes a call of the trace on the model.
func (r *Recorder) Call(o *code.Op, args []any) (res []any) {
	e := Entry{Op: o.Name, Args: []any{}, Res: make([]any, len(o.Results))}
	margs := make([]any, len(args))
	for i, a := range args {
		e.Args = append(e.Args, encodeArg(a))
		margs[i] = modelArg(a)
	}
	r.trace = append(r.trace, e)
	at := len(r.trace) - 1
	defer func() {
		// An interruption of the model is the interruption that the library must answer with.
		if x := recover(); x != nil {
			if _, ok := x.(code.Interrupt); ok {
				r.trace[at].Obs = nil
			}
			panic(x)
		}
	}()
	out := r.Model.Call(o, margs)
	res = make([]any, len(out))
	for i, v := range out {
		if o.Results[i].Kind == code.KHandle {
			t := r.tok(v)
			r.trace[at].Res[i] = t.id
			res[i] = t
		} else {
			res[i] = v
		}
	}
	for _, c := range o.Writes {
		if r.Model.Observe(c) != nil {
			r.trace[at].Obs = append(r.trace[at].Obs, c)
		}
	}
	return res
}

// Replay is an implementation that returns what the library did, call by call.
type Replay struct {
	trace   []Entry
	answers []Answer
	at      int
	toks    map[int64]*Tok
	last    Answer
}

// NewReplay returns the implementation of the library's answers to a trace.
func NewReplay(trace []Entry, answers []Answer) *Replay {
	return &Replay{trace: trace, answers: answers, toks: map[int64]*Tok{}}
}

// tokOf returns the handle of the library's value with identity id.
func (p *Replay) tokOf(id int64) *Tok {
	t := p.toks[id]
	if t == nil {
		t = &Tok{id: id}
		p.toks[id] = t
	}
	return t
}

// advance takes the answer to the next entry, which must be a call of op.
func (p *Replay) advance(op string) (Entry, Answer) {
	if p.at >= len(p.trace) || p.at >= len(p.answers) {
		panic(fmt.Sprintf("the trace has no call %d (%s): the library did not answer it", p.at, op))
	}
	e, a := p.trace[p.at], p.answers[p.at]
	if e.Op != op {
		panic(fmt.Sprintf("call %d is %s in the trace and %s in the replay: the harness made different calls", p.at, e.Op, op))
	}
	p.at++
	p.last = a
	return e, a
}

// Make takes the answer to a call of an operation that makes a handle.
func (p *Replay) Make(op string) *Tok {
	_, a := p.advance(op)
	if a.Err != "" {
		panic(errors.New(a.Err))
	}
	return p.tokOf(handleID(a.Res[0]))
}

func handleID(v any) int64 {
	m, ok := v.(map[string]any)
	if !ok {
		return 0
	}
	f, _ := m["h"].(float64)
	return int64(f)
}

// Call returns the answer of the library to the call of o.
func (p *Replay) Call(o *code.Op, args []any) []any {
	_, a := p.advance(o.Name)
	if a.Err != "" {
		panic(errors.New(a.Err))
	}
	if a.Int != "" {
		panic(code.Interrupt{Reason: a.Int})
	}
	res := make([]any, len(o.Results))
	for i, r := range o.Results {
		if i >= len(a.Res) {
			panic(fmt.Sprintf("the library returned %d results of %s, not %d", len(a.Res), o.Name, len(o.Results)))
		}
		v := a.Res[i]
		switch r.Kind {
		case code.KHandle:
			id := handleID(v)
			if id == 0 {
				// A result that is no value of the library, as the entry of a failed lookup.
				res[i] = &Tok{}
				continue
			}
			t := p.tokOf(id)
			if i < len(a.Lists) {
				t.visits, t.errs = a.Lists[i][0], a.Lists[i][1]
			}
			res[i] = t
		case code.KInt:
			res[i] = int64(v.(float64))
		case code.KU64:
			res[i] = uint64(v.(float64))
		case code.KBool:
			res[i] = v.(bool)
		default:
			panic(fmt.Sprintf("tsconformtest: no decoding of a result of kind %v of %s", r.Kind, o.Name))
		}
	}
	return res
}

// The accessors of the answer to the last call. A family's implementation provides the interfaces of
// package conform that it checks (Metered, Recovery, Observer) through them, so that the harness
// makes only the checks the family states.

// Units returns the budget used after the last call.
func (p *Replay) Units() uint64 { return uint64(p.last.Used) }

// RecIDs returns the identifiers of the recovered errors after the last call.
func (p *Replay) RecIDs() []int64 { return p.last.Rec }

// RecUnrolled returns the number of lists that listing the recovered errors visits.
func (p *Replay) RecUnrolled() int { return p.last.Unrolled }

// ListOf returns the visits and the errors of a list of recovered errors.
func (p *Replay) ListOf(l any) (visits, errs int) {
	t := l.(*Tok)
	return t.visits, t.errs
}

// Observation returns the observation of a component after the last call.
func (p *Replay) Observation(c string) []string { return p.last.Obs[c] }
