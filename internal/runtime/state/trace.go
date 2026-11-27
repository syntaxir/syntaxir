package state

import (
	"syntaxir.dev/sxir/pkg/runtime/instrument"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// Tracing reports every rule call of a parse to a function (ParseOptions.Trace). It is built
// around the one function all backends call for a rule (Parser.Call), so that an ordinary parse
// pays for it with a single nil check per call of a rule that is not plain; plain calls, which
// skip call, take that path only when the parser allows it (Parser.NoPlain), which a traced
// parse does not: it runs the call-preserving family of its backend (FamilyOf). See
// docs/design/records/014-tracing-and-profiling.md.

// TraceKind is instrument.TraceKind. A parse reports TraceEnter and TraceExit; the engine reports no
// TraceSkipped events (the calls that its backends skip are not reported).
type TraceKind = instrument.TraceKind

const (
	TraceEnter = instrument.TraceEnter
	TraceExit  = instrument.TraceExit
)

// TraceEvent is instrument.TraceEvent. Its accessors read the parse's tracing state (Tracer, the event's
// TraceSource): call them only from the trace function, while it handles the event.
type TraceEvent = instrument.TraceEvent

// TraceParser returns the parser that reported e, or nil for an event that no parse reported.
// It gives code outside the public API the parse state behind an event.
func TraceParser(e TraceEvent) *Parser {
	if t, ok := e.Source().(*Tracer); ok {
		return t.p
	}
	return nil
}

// TraceExpectations returns the expectations of the farthest failure that the exit event e, the one
// the trace function is handling, records.
func TraceExpectations(e TraceEvent) []ExpID {
	if t, ok := e.Source().(*Tracer); ok && e.Kind == TraceExit {
		return t.exp
	}
	return nil
}

// TraceValue returns the value that the call of the exit event e, the one the trace function is
// handling, produced, and whether the call produces a value of its rule: it does not for a call
// that failed, for another event, for the value-free twin of a rule and a hidden rule spliced into
// its caller, whose values are nil, and for a call of the start rule of a stream, whose top call
// builds no value. It gives tests of the values of rules the values of the calls in their
// contexts, which TraceEvent does not carry.
func TraceValue(e TraceEvent) (*value.Node, bool) {
	if t, ok := e.Source().(*Tracer); ok && e.Kind == TraceExit && e.Matched && t.valued {
		return t.val, true
	}
	return nil, false
}

// Tracer is the tracing state of a parse. It is the TraceSource of the parse's events: the state
// of the exit event being reported (far, exp, the recovered errors and the value) is valid while
// the trace function and the profile recorders handle it.
type Tracer struct {
	traceRecorder
	p *Parser
	// direct is set by tracedCall just before it calls call for the call it has reported, so that
	// call runs it instead of reporting it again.
	direct bool
	stack  []tracedCallState
	// The exit being reported: its farthest failure and expectations, and the errors recovered
	// during the call: tail, the errors of a bounded stream's tail, or the entries of the record of
	// recovered errors from rec on.
	far  int
	exp  []ExpID
	tail bool
	rec  int
	// val is the value of the call of the exit being reported, and valued whether the call
	// produces a value of its rule (TraceValue).
	val    *value.Node
	valued bool
}

// LineCol returns the 1-based line and column of the position pos. In a stream parse, it returns
// 0, 0 for a position (other than 0) in input that has been discarded or not read yet: keeping
// the line starts of discarded input would make the memory of a stream grow with its length.
func (t *Tracer) LineCol(pos int) (line, col int) {
	switch {
	case pos == 0:
		return 1, 1
	case pos < t.p.Base || pos > t.p.Loaded():
		return 0, 0
	}
	return t.p.lineCol(pos)
}

// Text returns the input [start, end); a stream parse cuts it at the last commit point, as text does.
func (t *Tracer) Text(start, end int) string {
	return t.p.Text(start, end)
}

// Failure returns the farthest failure of the exit being reported.
func (t *Tracer) Failure() *value.SyntaxError {
	if len(t.exp) == 0 {
		return nil
	}
	return t.p.MakeError(t.far, t.exp)
}

// Recovered returns the errors recovered during the call of the exit being reported.
func (t *Tracer) Recovered() []*value.SyntaxError {
	p := t.p
	if t.tail {
		// A bounded stream has released earlier committed recoveries: report the same ordered
		// tail, including a successful suffix.
		return p.DiagnosticTail.Preview(p, p.RecoveredErrors())
	}
	if len(p.Recovered) > t.rec {
		return appendRecErrors(nil, p.Recovered[t.rec:])
	}
	return nil
}

// emit records ev (traceRecorder.record). The parse observes its interruptions before the event
// and after the trace function returns, so that a cancellation that the trace function makes stops
// the parse before the next function or event.
func (t *Tracer) emit(ev TraceEvent, selected bool) {
	t.p.Control.Check()
	if t.record(&ev, selected) {
		t.p.Control.Check()
	}
}

// raisedForTrace reports whether x, a panic of the trace function, is an interruption that the
// parser raised while the trace function used it (traceRecorder.deliver): the accessors of an event
// read the parse state and charge the budget, which can interrupt the parse.
func raisedForTrace(x any) bool { _, ok := x.(Fatal); return ok }

// tracedCallState is the state of a traced call in progress.
type tracedCallState struct {
	r     *Rule
	min   int
	start int
	// evals is the number of body evaluations of the parse when the call started, and nested
	// those made by nested calls since.
	evals, nested int
	// resumed is the parse's repetition-element reuse count when the call started.
	resumed int
	// hw is the end of the examined input when the call started, and rec the number of
	// entries of the record of recovered errors.
	hw, rec int
	exp     ExpMark
	// selected is set when the trace function receives the call's events (Tracer.rules).
	selected bool
}

// SetTrace makes the parse report its rule calls to fn and record them in profiles (nothing if fn is
// nil and there are no profiles); SelectTrace then limits what fn receives. CloseTrace ends the
// recording when the parse returns.
func (p *Parser) SetTrace(fn func(TraceEvent), profiles []*instrument.Profile) {
	if fn == nil && len(profiles) == 0 {
		return
	}
	t := &Tracer{p: p}
	t.trace = fn
	for _, pr := range profiles {
		t.addProfile(pr)
	}
	p.Tr = t
	p.NoPlain = true
}

// Hotspot returns the hotspot of the first profile that records the parse (instrument.Profile.Hotspot),
// as of the events recorded so far, or the empty string when no profile records it.
func (p *Parser) Hotspot() string {
	if t := p.Tr; t != nil && len(t.profiles) > 0 {
		return t.profiles[0].Hotspot()
	}
	return ""
}

// SelectTrace limits the events that the trace function given to SetTrace receives to those that s
// selects (all if s is nil). It is apart from SetTrace so that SetTrace stays small enough to
// inline into the parse entry points.
func (p *Parser) SelectTrace(s *TraceSelection) {
	if t := p.Tr; s != nil && t != nil {
		t.maxDepth = s.MaxDepth
		t.selectRules(s.Rules)
	}
}

// CloseTrace ends the recording of the parse's profiles (calls that began and did not complete
// stay counted as entered only) and drops what the tracer refers to besides the parser, which the
// events kept by the trace function still refer to.
func (p *Parser) CloseTrace() {
	if t := p.Tr; t != nil {
		t.close()
		*t = Tracer{p: p}
	}
}

// tracedCall reports a call of r, runs it with call, and reports its result.
func (p *Parser) tracedCall(r *Rule, min int) (*value.Node, bool) {
	p.TraceEnter(r, min)
	p.Tr.direct = true
	v, ok := p.Call(r, min)
	p.TraceExit(v, ok)
	return v, ok
}

// TraceEnter reports the start of a call of r and starts measuring it. The call's examined range
// and expectations are recorded separately, as CallBegin does for a memoized call, and merged
// back by TraceExit; doing so for any call leaves the parse's own records unchanged (both only
// keep a maximum).
func (p *Parser) TraceEnter(r *Rule, min int) {
	t := p.Tr
	selected := t.selects(r.Name, len(t.stack) > 0 && t.stack[len(t.stack)-1].selected)
	t.stack = append(t.stack, tracedCallState{r: r, min: min, start: p.Pos, evals: p.Stats.Evaluated, resumed: p.Resumed, hw: p.Hw, rec: len(p.Recovered), selected: selected})
	p.Hw = p.Pos
	t.stack[len(t.stack)-1].exp = p.Isolate(p.Pos)
	t.emit(TraceEvent{Kind: TraceEnter, Rule: r.Name, Level: min, Depth: len(t.stack), Pos: p.Pos, Lookahead: p.Silent > 0}.WithSource(t), selected)
}

// TraceExit reports the end of the innermost call in progress, whose value is v and result ok.
func (p *Parser) TraceExit(v *value.Node, ok bool) {
	t := p.Tr
	c := t.stack[len(t.stack)-1]
	all := p.Stats.Evaluated - c.evals
	far, inner := p.Unisolate(c.exp)
	ev := TraceEvent{Kind: TraceExit, Rule: c.r.Name, Level: c.min, Depth: len(t.stack), Pos: c.start,
		Lookahead: p.Silent > 0, Matched: ok, End: c.start, Memo: all == 0, Evals: all - c.nested,
		Examined: p.Hw, ReusedElements: p.Resumed - c.resumed}.WithSource(t)
	t.far, t.exp, t.tail, t.rec, t.val, t.valued = far, inner, false, len(p.Recovered), nil, false
	if ok {
		t.val = v
		t.valued = !c.r.Novalue && !c.r.Splice && !(p.Input.Stream && c.r.ID == p.StreamID)
		ev.End = p.Pos
		t.tail = len(t.stack) == 1 && p.DiagnosticTail.Limit > 0
		t.rec = c.rec
	}
	t.emit(ev, c.selected)
	t.exp, t.val, t.valued = nil, nil, false
	p.MergeExpected(far, inner)
	p.Hw = max(c.hw, p.Hw)
	t.stack = t.stack[:len(t.stack)-1]
	if n := len(t.stack); n > 0 {
		t.stack[n-1].nested += all
	}
}

// ComposeTrace joins public option callbacks while checking interruption before
// starting the next individual callback. Caller-created composite functions are
// still arbitrary caller code and cannot be preempted internally.
func ComposeTrace(prev, next func(TraceEvent)) func(TraceEvent) {
	return func(ev TraceEvent) {
		prev(ev)
		if BudgetControlsEnabled {
			TraceParser(ev).Control.Check()
		}
		next(ev)
	}
}
