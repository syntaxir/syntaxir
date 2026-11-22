package libgo

import (
	"slices"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinstrument "syntaxir.dev/sxir/pkg/runtime/instrument"
)

// observeOptions are the parse options that select an observation: the trace
// function (the trace functions of the options in order), the first profile and
// the rarer options, which an unobserved parse carries as one nil pointer.
type observeOptions struct {
	trace   func(sxirinstrument.TraceEvent)
	profile *sxirinstrument.Profile
	extras  *observeExtras
}

// observeExtras are the profiles after the first, the trace filter and the
// depth limit. Options replace them instead of changing them, since a
// Document's defaults share them with each parse.
type observeExtras struct {
	more     []*sxirinstrument.Profile
	rules    []string
	maxDepth int
}

// withExtras gives o a copy of its extras to change and returns it.
func (o *observeOptions) withExtras() *observeExtras {
	x := new(observeExtras)
	if o.extras != nil {
		*x = *o.extras
	}
	o.extras = x
	return x
}

// observeOf returns the observation that the settings u of the options of package runtime
// select. The events carry no parser state: their LineCol, Text, Failure and Recovered return zero
// values.
func observeOf(u *sxirruntime.ParseOptions) (ob observeOptions) {
	for _, f := range u.Traces {
		switch prev := ob.trace; {
		case prev == nil:
			ob.trace = f
		default:
			ob.trace = func(e sxirinstrument.TraceEvent) {
				prev(e)
				f(e)
			}
		}
	}
	for i, p := range u.Profiles {
		if i == 0 {
			ob.profile = p
		} else {
			x := ob.withExtras()
			x.more = append(slices.Clip(x.more), p)
		}
	}
	if sel := u.TraceSelection; sel != nil {
		x := ob.withExtras()
		x.rules, x.maxDepth = sel.Rules, sel.MaxDepth
	}
	return ob
}

// newObservation returns the observation that o selects for one parse, or nil.
// silent is the parser's lookahead counter (Parser.Silent).
func newObservation(o *observeOptions, silent *int) *observation {
	if o.trace == nil && o.profile == nil {
		return nil
	}
	out := &observation{silent: silent}
	out.trace = o.trace
	if o.profile != nil {
		out.addProfile(o.profile)
	}
	if x := o.extras; x != nil {
		out.maxDepth = x.maxDepth
		for _, pr := range x.more {
			out.addProfile(pr)
		}
		out.selectRules(x.rules)
	}
	return out
}

// release drops what the observation refers to once its parse returns.
func (o *observation) release() {
	if o != nil {
		o.close()
		*o = observation{}
	}
}

// hotspot returns the hotspot rule of the first profile of the parse (Profile.Hotspot), or the empty
// string without one.
func (o *observation) hotspot() string {
	if o == nil || len(o.profiles) == 0 {
		return ""
	}
	return o.profiles[0].Hotspot()
}

// dropDiscarded forgets the profile's evaluation counts before pos, which a
// stream discarded: no rule is evaluated there again.
func (o *observation) dropDiscarded(pos int) {
	if o != nil {
		o.discard(pos)
	}
}

// raisedForTrace reports whether x, a panic of the trace function, is an interruption that the
// parser raised while the trace function used it (traceRecorder.deliver). The events of the library
// carry no parser state, so the trace function reaches nothing that interrupts the parse: every
// panic of it is its own, whatever its value.
func raisedForTrace(any) bool { return false }

// Of its fields and methods, Enter, Evaluated, Exit, Skipped are for generated parsers; not a stable API.
type observation struct {
	traceRecorder
	silent *int
	stack  []observationCall
}

type observationCall struct {
	event    sxirinstrument.TraceEvent
	selected bool
}

func (o *observation) selected(rule string) bool {
	return o.selects(rule, len(o.stack) > 0 && o.stack[len(o.stack)-1].selected)
}

func (o *observation) Enter(rule string, pos, level int) {
	selected := o.selected(rule)
	e := sxirinstrument.TraceEvent{Kind: sxirinstrument.TraceEnter, Rule: rule, Pos: pos, Level: level, Depth: len(o.stack) + 1,
		Lookahead: *o.silent > 0}
	o.stack = append(o.stack, observationCall{event: e, selected: selected})
	o.record(&e, selected)
}

func (o *observation) Evaluated() {
	if o != nil && len(o.stack) > 0 {
		o.stack[len(o.stack)-1].event.Evals++
	}
}

func (o *observation) memoHit() {
	if o != nil && len(o.stack) > 0 {
		o.stack[len(o.stack)-1].event.Memo = true
	}
}

func (o *observation) Exit(matched bool, end, examined int) {
	i := len(o.stack) - 1
	call := &o.stack[i]
	o.stack = o.stack[:i]
	e := &call.event
	e.Kind, e.Matched, e.End, e.Examined = sxirinstrument.TraceExit, matched, end, examined
	o.record(e, call.selected)
}

func (o *observation) Skipped(rule string, pos, level int) {
	if o != nil {
		e := sxirinstrument.TraceEvent{Kind: sxirinstrument.TraceSkipped, Rule: rule, Pos: pos, Level: level,
			Depth: len(o.stack) + 1, Lookahead: *o.silent > 0, SkipReason: sxirinstrument.SkipFirstCharacter}
		o.record(&e, o.selected(rule))
	}
}
