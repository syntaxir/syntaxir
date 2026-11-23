package state

import (
	"unsafe"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

type MissingTokenSite struct{ ID, Pos int }
type MissingTokenEvent struct {
	Site MissingTokenSite
	Err  *value.SyntaxError
}

// MissingTokenState belongs to one invocation. The path rolls back; attempts do
// not. Completed results depending on this state must never be memo-reused.
type MissingTokenState struct {
	Limit, Attempts int
	Path            []MissingTokenEvent
	installed       map[MissingTokenSite]bool
	Seeds           map[*MemoEntry][]MissingTokenEvent // no slice added to every ordinary memo entry
	label           ExpID                              // outermost enclosing @error, or zero
	// seedBytes counts the events of the seeds recorded, for the memory limit.
	seedBytes uint64
}

func (p *Parser) missingMark() int {
	return p.MissingLen
}

func (p *Parser) ResetMissing(mark int) {
	if p.Missing == nil {
		return
	}
	s := p.Missing
	for _, event := range s.Path[mark:] {
		delete(s.installed, event.Site)
	}
	clear(s.Path[mark:])
	s.Path = s.Path[:mark]
	p.MissingLen = mark
}

// MissingEvents returns a copy of the insertions on the current path after the first mark, which a
// store keeps: a seed of left recursion (RecordMissingSeed) or an attempt of an Operators Rule. The
// copy counts one compute unit for each insertion it copies (spec/runtime/parsing.md#compute-units).
func (p *Parser) MissingEvents(mark int) []MissingTokenEvent {
	if p.Missing == nil {
		return nil
	}
	events := p.Missing.Path[mark:]
	p.countUnits(len(events))
	return append([]MissingTokenEvent(nil), events...)
}

func (p *Parser) InsertMissing(event MissingTokenEvent) bool {
	return p.installMissing(event, true)
}

func (p *Parser) installMissing(event MissingTokenEvent, charge bool) bool {
	s := p.Missing
	if s == nil || s.installed[event.Site] {
		return false
	}
	if charge && s.Attempts >= s.Limit {
		panic(Fatal{&runtime.MissingTokenLimitError{Limit: s.Limit, Position: event.Site.Pos}})
	}
	if charge {
		s.Attempts++
	}
	if s.installed == nil {
		s.installed = make(map[MissingTokenSite]bool)
	}
	s.installed[event.Site] = true
	s.Path = append(s.Path, event)
	p.MissingLen = len(s.Path)
	return true
}

// Only active/selected LR seeds need insertion snapshots. Keep these in the
// opt-in invocation state rather than enlarging every ordinary memo entry.
func (p *Parser) MissingSeedEvents(e *MemoEntry) []MissingTokenEvent {
	if p.Missing == nil {
		return nil
	}
	return p.Missing.Seeds[e]
}

func (p *Parser) RecordMissingSeed(e *MemoEntry, mark int) {
	if p.Missing == nil {
		return
	}
	events := p.MissingEvents(mark)
	if len(events) == 0 {
		// Memo slabs recycle pointers; never inherit a retired seed's events.
		delete(p.Missing.Seeds, e)
		return
	}
	if p.Missing.Seeds == nil {
		p.Missing.Seeds = make(map[*MemoEntry][]MissingTokenEvent)
	}
	p.Missing.Seeds[e] = events
	p.Missing.seedBytes += uint64(cap(events)) * uint64(unsafe.Sizeof(MissingTokenEvent{}))
}

// ReplayMissing restores a chosen seed/candidate. Repeated events already on
// the path neither cost attempts nor duplicate their insertion diagnostic.
// Consuming recovery diagnostics remain subject to their existing replay rules.
func (p *Parser) ReplayMissing(events []MissingTokenEvent, errs RecEntry) {
	p.restoreMissing(events, errs, true)
}

// Selecting an operator candidate restores an already charged synthesis; it is
// not a memo replay and does not take an attempt for that synthesis a second time.
// Either way, it counts one compute unit for each insertion of events, and, when it
// records the errors one by one, one for each of errs (spec/runtime/parsing.md#compute-units).
func (p *Parser) restoreMissing(events []MissingTokenEvent, errs RecEntry, charge bool) {
	p.countUnits(len(events))
	var duplicate map[*value.SyntaxError]bool
	for _, event := range events {
		if p.Missing.installed[event.Site] {
			if duplicate == nil {
				duplicate = make(map[*value.SyntaxError]bool)
			}
			duplicate[event.Err] = true
		} else {
			p.installMissing(event, charge)
		}
	}
	if duplicate == nil {
		p.recAdd(errs)
		return
	}
	// The errors are recorded one by one, without those of the repeated insertions.
	all := errs.AppendErrors(nil)
	p.countUnits(len(all))
	for _, err := range all {
		if !duplicate[err] {
			p.PushRecovered(err)
		}
	}
}

// MissingErrorScope is emitted only for grammars using this feature; ordinary
// grammars retain the existing errorAttr path, without scope/defer machinery.
func MissingErrorScope(m Matcher, msg ExpID) Matcher {
	return func(p *Parser) (*value.Node, bool) {
		prev := p.EnterMissingScope(msg)
		defer p.LeaveMissingScope(prev)
		return m(p)
	}
}

// Inserting reports that the parse inserts missing tokens (flow.inserting).
func (p *Parser) Inserting() bool { return p.Missing != nil }

// EnterMissingScope enters an @error label whose message msg (an ExpID with MsgBit) the insertions
// inside take as their expectation, in a parse that inserts missing tokens and records
// expectations, where no label is entered. It returns the label entered before, which
// LeaveMissingScope restores (flow.missingScope).
func (p *Parser) EnterMissingScope(msg ExpID) ExpID {
	if p.Missing == nil {
		return 0
	}
	prev := p.Missing.label
	if p.Expectations && prev == 0 {
		p.Missing.label = msg
	}
	return prev
}

// LeaveMissingScope restores the label prev that EnterMissingScope returned (flow.missingScopeEnd).
func (p *Parser) LeaveMissingScope(prev ExpID) {
	if p.Missing != nil {
		p.Missing.label = prev
	}
}

// InsertMissingAt inserts the literal of the #missing site at the position, where it did not
// match with the expectations exp (flow.insertMissing). In a parse that inserts missing tokens,
// outside a lookahead, where the site has not inserted at the position on the current path, it
// builds the syntax error of the insertion with exp, or with the label that EnterMissingScope
// entered, which counts and checks a compute unit; takes an attempt, which ends the parse with a
// MissingTokenLimitError where none remains; puts the insertion on the path; records the error as
// a recovered error; and returns it and true. Otherwise it returns false and changes nothing.
func (p *Parser) InsertMissingAt(site int, exp []ExpID) (*value.SyntaxError, bool) {
	if p.Missing == nil || p.Silent > 0 {
		return nil, false
	}
	key := MissingTokenSite{site, p.Pos}
	if p.Missing.installed[key] {
		return nil, false
	}
	if p.Missing.label != 0 {
		exp = []ExpID{p.Missing.label}
	}
	e := p.RecoveryError(p.Pos, exp)
	p.InsertMissing(MissingTokenEvent{key, e})
	p.PushRecovered(e)
	return e, true
}

// MissingNode returns the missing node of the insertion whose error is e at start, for a literal
// whose text is expected (flow.newMissingNode).
func (p *Parser) MissingNode(start int, expected string, e *value.SyntaxError) *value.Node {
	return p.NewNode(nodes.Data{Kind: valueimpl.MissingKind, Start: int32(start), End: int32(start),
		Fields: valueimpl.Fields{{Name: "expected", Value: expected}, {Name: "message", Value: p.RecoveryErrorText(e)}}, Terminal: true, Fresh: true})
}

// MissingLiteral preserves the entire ordinary literal match before trying an
// insertion. Its stable compile-time site is independent of invocation state.
func MissingLiteral(literal Matcher, site int, expected string, build bool) Matcher {
	return func(p *Parser) (*value.Node, bool) {
		if p.Missing == nil || p.Silent > 0 {
			return literal(p)
		}
		m0 := p.Mark()
		v, ok, far, exp := Tracked(p, literal)
		if ok {
			p.MergeExpected(far, exp)
			return v, true
		}
		p.Reset(m0)
		e, ok := p.InsertMissingAt(site, exp)
		if !ok {
			p.MergeExpected(far, exp)
			return nil, false
		}
		if !build {
			return nil, true
		}
		return p.MissingNode(m0.Pos, expected, e), true
	}
}
