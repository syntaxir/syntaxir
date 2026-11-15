package libgo

import "zeroparse.dev/zp/core"

// WithTrace observes actual generated calls synchronously. Callback panics
// propagate unchanged after cleanup; an aborted parse can omit exit events.
// The events carry no parser state: their LineCol, Text, Failure and Recovered
// return zero values.
func WithTrace(trace func(core.TraceEvent)) ParseOption {
	return func(o *parseOptions) { o.observe.trace = trace }
}

// WithProfile adds the rule calls of the parse to profile. It is independent of
// trace delivery filters, and its times exclude the trace function. A Profile
// must not be shared by concurrent parses.
func WithProfile(profile *core.Profile) ParseOption {
	return func(o *parseOptions) { o.observe.profile = profile }
}

// WithTraceFilter selects named rules and their nested calls. Unknown names
// match nothing. With no names, every entered rule is selected.
func WithTraceFilter(rules ...string) ParseOption {
	names := append([]string(nil), rules...)
	return func(o *parseOptions) { o.observe.rules = names }
}

// WithTraceMaxDepth limits delivered events, counting the start rule as one.
// Zero and negative values leave delivery unlimited; parsing is unaffected.
func WithTraceMaxDepth(n int) ParseOption {
	return func(o *parseOptions) { o.observe.maxDepth = n }
}

// observeOptions are the parse options that select an observation.
type observeOptions struct {
	trace    func(core.TraceEvent)
	profile  *core.Profile
	rules    []string
	maxDepth int
}

// newObservation returns the observation that o selects for one parse, or nil.
// silent is the parser's lookahead counter (Parser.Silent).
func newObservation(o *observeOptions, silent *int) *observation {
	if o.trace == nil && o.profile == nil {
		return nil
	}
	out := &observation{trace: o.trace, maxDepth: o.maxDepth, profile: o.profile.Recorder(), silent: silent}
	if len(o.rules) != 0 {
		out.rules = make(map[string]bool, len(o.rules))
		for _, name := range o.rules {
			out.rules[name] = true
		}
	}
	return out
}

// release drops what the observation refers to once its parse returns.
func (o *observation) release() {
	if o != nil {
		o.profile.Close()
		*o = observation{}
	}
}

// dropDiscarded forgets the profile's evaluation counts before pos, which a
// stream discarded: no rule is evaluated there again.
func (o *observation) dropDiscarded(pos int) {
	if o != nil {
		o.profile.Discard(pos)
	}
}

// Of its fields and methods, Enter, Evaluated, Exit, Skipped are for generated parsers; not a stable API.
type observation struct {
	trace    func(core.TraceEvent)
	profile  *core.ProfileRecorder
	rules    map[string]bool
	maxDepth int
	silent   *int
	stack    []observationCall
}

type observationCall struct {
	event    core.TraceEvent
	selected bool
}

// Only callback panics are wrapped. The parse recovery boundary unwraps before
// handling its own private interruption types, preserving every callback value.
type tracePanic struct{ value any }

// record adds e to the profile and delivers it to the trace function. The
// profile records e first and resumes after the trace function returns, so
// that profile times exclude it. e is read before record returns and not kept.
func (o *observation) record(e *core.TraceEvent, selected bool) {
	if o.profile != nil {
		o.profile.Record(*e)
	}
	if o.trace == nil || !selected || o.maxDepth > 0 && e.Depth > o.maxDepth {
		return
	}
	o.deliver(e)
	if o.profile != nil {
		o.profile.Resume()
	}
}

func (o *observation) deliver(e *core.TraceEvent) {
	defer func() {
		if x := recover(); x != nil {
			panic(tracePanic{x})
		}
	}()
	o.trace(*e)
}

func (o *observation) selected(rule string) bool {
	return len(o.rules) == 0 || o.rules[rule] || len(o.stack) > 0 && o.stack[len(o.stack)-1].selected
}

func (o *observation) Enter(rule string, pos, level int) {
	selected := o.selected(rule)
	e := core.TraceEvent{Kind: core.TraceEnter, Rule: rule, Pos: pos, Level: level, Depth: len(o.stack) + 1,
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
	e.Kind, e.Matched, e.End, e.Examined = core.TraceExit, matched, end, examined
	o.record(e, call.selected)
}

func (o *observation) Skipped(rule string, pos, level int) {
	if o != nil {
		e := core.TraceEvent{Kind: core.TraceSkipped, Rule: rule, Pos: pos, Level: level,
			Depth: len(o.stack) + 1, Lookahead: *o.silent > 0, SkipReason: core.SkipFirstCharacter}
		o.record(&e, o.selected(rule))
	}
}
