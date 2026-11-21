package libgo

import sxirinstrument "syntaxir.dev/sxir/pkg/runtime/instrument"

// traceRecorder records the events of an observed parse: it passes each to the profiles, and to the
// trace function when the event is selected (spec/runtime/observability.md). The library's
// observation and the engine's Tracer embed it; each keeps its own stack of the calls in progress
// and makes the events.
type traceRecorder struct {
	// trace is the trace function (the functions of the options in order), or nil.
	trace func(sxirinstrument.TraceEvent)
	// profiles record every event of the parse; one holds them when there is one.
	profiles []*sxirinstrument.ProfileRecorder
	one      [1]*sxirinstrument.ProfileRecorder
	// rules, if not empty, selects the calls the trace function receives: those of the rules it
	// holds and the calls nested in them. maxDepth, if positive, leaves out the calls nested deeper.
	rules    map[string]bool
	maxDepth int
}

// addProfile makes the parse record in profile.
func (r *traceRecorder) addProfile(profile *sxirinstrument.Profile) {
	if r.profiles == nil {
		r.profiles = r.one[:0]
	}
	r.profiles = append(r.profiles, profile.Recorder())
}

// selectRules makes the trace function receive only the calls of the named rules and the calls
// nested in them; no names select every call.
func (r *traceRecorder) selectRules(names []string) {
	r.rules = nil
	if len(names) != 0 {
		r.rules = make(map[string]bool, len(names))
		for _, name := range names {
			r.rules[name] = true
		}
	}
}

// selects reports whether a call of rule is selected, nested in a selected call if nested.
func (r *traceRecorder) selects(rule string, nested bool) bool {
	return len(r.rules) == 0 || r.rules[rule] || nested
}

// record adds e to the profiles and delivers it to the trace function if the call is selected and no
// deeper than the depth limit, and reports whether it delivered it. The profiles record e first and
// resume after the trace function returns, so that their times exclude it. e is read before record
// returns and not kept.
func (r *traceRecorder) record(e *sxirinstrument.TraceEvent, selected bool) bool {
	for _, pr := range r.profiles {
		pr.Record(*e)
	}
	if r.trace == nil || !selected || r.maxDepth > 0 && e.Depth > r.maxDepth {
		return false
	}
	if r.profiles != nil {
		defer func() {
			for _, pr := range r.profiles {
				pr.Resume()
			}
		}()
	}
	r.deliver(e)
	return true
}

// deliver passes e to the trace function. A panic there leaves as a tracePanic, so that the parse's
// recovery does not take it for one of its own (or, on a bytecode backend, for a fault of the
// program) and raises it again unchanged. An interruption that the parser raised while the trace
// function used it passes as it is (raisedForTrace).
func (r *traceRecorder) deliver(e *sxirinstrument.TraceEvent) {
	defer func() {
		if x := recover(); x != nil {
			if raisedForTrace(x) {
				panic(x)
			}
			panic(tracePanic{x})
		}
	}()
	r.trace(*e)
}

// tracePanic is a panic of the trace function, with the value it panicked with.
type tracePanic struct{ Value any }

// discard tells the profiles that a stream discarded the input before pos: no rule is evaluated
// there again.
func (r *traceRecorder) discard(pos int) {
	for _, pr := range r.profiles {
		pr.Discard(pos)
	}
}

// close ends the recording of the profiles: calls that began and did not complete stay counted as
// entered only.
func (r *traceRecorder) close() {
	for _, pr := range r.profiles {
		pr.Close()
	}
}
