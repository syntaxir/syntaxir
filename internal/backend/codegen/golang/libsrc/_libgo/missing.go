package libgo

import (
	"fmt"
	"unsafe"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// The missing-token mode (spec/language/pragmas.md#missing): a parse with a positive limit of missing
// tokens (WithMissingTokens) of a grammar with #missing inserts the literal of a #missing site where it
// does not match, as a recovered error. The insertions on the current path of the parse are a path that
// backtracking shortens (ResetMissing); the attempts they took stay taken. A site inserts at most once at
// a position on the path, so that a repetition or a left-recursive growth cannot insert forever.
//
// The results of a rule that can insert (Rule.Missing) depend on the insertions before them, so the
// missing-token mode evaluates them again at every call instead of reusing them from the memo or from a
// context of left recursion. Only the seed of a growing leader is reused, as the growth adopted it, with
// the insertions that it made (missingSeed), which a reuse records again (replayMissing).

// missingUnsupported returns the error of an entry point that does not insert missing tokens, asked to.
func missingUnsupported(operation string) error {
	return fmt.Errorf("missing-token recovery is not supported for %s; use whole-input parsing or recognition", operation)
}

// missingToken is an insertion: the site of the grammar that inserted, the position, and the syntax error
// it recovered.
type missingToken struct {
	site, pos int
	err       *sxirvalue.SyntaxError
}

// missingSite is a site at a position, which inserts at most once on the path.
type missingSite struct{ site, pos int }

// missingState is the state of a parse in the missing-token mode.
type missingState struct {
	limit, attempts int
	// path is the insertions on the current path, in order; installed holds their sites.
	path      []missingToken
	installed map[missingSite]bool
	// label is the message of the outermost @error label entered around #missing sites (MissingScope),
	// or 0 outside one.
	label ExpID
	// seedBytes counts the bytes of the insertions that seeds hold (missingSeed), for the memory limit.
	seedBytes uint64
}

// missingTokenBytes is the size of an insertion, for the memory limit.
const missingTokenBytes = uint64(unsafe.Sizeof(missingToken{}))

// missingSeed is the insertions that a seed of a growing leader made, which a reuse records again.
type missingSeed []missingToken

// inserting reports whether the parse runs in the missing-token mode.
func (p *Parser) inserting() bool { return usedFeatures&featureMissing != 0 && p.missing != nil }

// SetInsertion makes each rule of rules run as the rule of the same position of ins, the table of the
// insertion program of their grammar, in the parses with missing-token insertion enabled (forMissing).
// The insertion program is the grammar compiled again with the rule flags of insertion, so its tables
// have the rules of rules, in the same order.
//
// SetInsertion is for generated parsers; not a stable API.
func SetInsertion(rules, ins []*Rule) {
	if len(rules) != len(ins) {
		panic(fmt.Sprintf("internal error: the insertion table has %d rules for %d", len(ins), len(rules)))
	}
	for i, r := range rules {
		if r.Name != ins[i].Name {
			panic(fmt.Sprintf("internal error: the insertion table has the rule %s for %s", ins[i].Name, r.Name))
		}
		r.ins = ins[i]
	}
}

// forMissing returns the rule that a parse with the options o runs for r: the rule of the table of the
// insertion program when o enables missing-token insertion of a grammar that uses #missing, as every
// insertion parse runs the insertion program, and r otherwise, so that the parses without insertion
// run tables that hold no insertion.
func (r *Rule) forMissing(o parseOptions) *Rule {
	if usedFeatures&featureMissing != 0 && o.missingTokens > 0 && r.ins != nil {
		return r.ins
	}
	return r
}

// startMissing sets up the missing-token mode of a parse with the limit of the options o.
func (p *Parser) startMissing(o parseOptions) {
	if usedFeatures&featureMissing != 0 && o.missingTokens > 0 && p.g.HasMissing {
		p.use(featureMissing)
		p.missing = &missingState{limit: o.missingTokens}
	}
}

// MissingLen returns the number of insertions on the current path, 0 outside the missing-token mode.
//
// MissingLen is for generated parsers; not a stable API.
func (p *Parser) MissingLen() int {
	if !p.inserting() {
		return 0
	}
	return len(p.missing.path)
}

// ResetMissing drops the insertions on the current path after the first n, so that their sites may
// insert at their positions again. It does nothing outside the missing-token mode.
//
// ResetMissing is for generated parsers; not a stable API.
func (p *Parser) ResetMissing(n int) {
	if !p.inserting() {
		return
	}
	s := p.missing
	if n >= len(s.path) {
		return
	}
	for _, t := range s.path[n:] {
		delete(s.installed, missingSite{t.site, t.pos})
	}
	clear(s.path[n:])
	s.path = s.path[:n]
}

// install puts the insertion t on the path, taking an attempt of the limit if charge is set, and reports
// false, changing nothing, if its site inserted at its position on the path.
func (p *Parser) installMissing(t missingToken, charge bool) bool {
	s := p.missing
	k := missingSite{t.site, t.pos}
	if s.installed[k] {
		return false
	}
	if charge {
		if s.attempts >= s.limit {
			panic(fatal{err: &sxirruntime.MissingTokenLimitError{Limit: s.limit, Position: t.pos}})
		}
		s.attempts++
	}
	if s.installed == nil {
		s.installed = make(map[missingSite]bool)
	}
	s.installed[k] = true
	s.path = append(s.path, t)
	return true
}

// InsertMissing inserts the literal of the #missing site where it did not match, at the position, with
// the expectations exps of the failed match: in the missing-token mode, outside a lookahead, where the
// site has not inserted at the position on the path, it builds the syntax error of the insertion (with
// the message of the @error label that MissingScope entered in place of exps, in a parse that records
// expectations), takes an attempt of the limit, which ends the parse with a MissingTokenLimitError where
// none remains, puts the insertion on the path, records the error as recovered and returns it and true.
// Otherwise it returns false and changes nothing.
//
// InsertMissing is for generated parsers; not a stable API.
func (p *Parser) InsertMissing(site int, exps []ExpID) (*sxirvalue.SyntaxError, bool) {
	return p.insertMissing(site, exps, false)
}

// InsertMissingLogical is InsertMissing that counts the syntax error it builds, an event of one compute
// unit, and checks the counter first (flow.insertMissing).
//
// InsertMissingLogical is for generated parsers; not a stable API.
func (p *Parser) InsertMissingLogical(site int, exps []ExpID) (*sxirvalue.SyntaxError, bool) {
	return p.insertMissing(site, exps, true)
}

// insertMissing is InsertMissing, which counts the error it builds if logical is set.
func (p *Parser) insertMissing(site int, exps []ExpID, logical bool) (*sxirvalue.SyntaxError, bool) {
	if !p.inserting() || p.Silent > 0 || p.missing.installed[missingSite{site, p.Pos}] {
		return nil, false
	}
	if logical {
		p.unitsSized(1)
	}
	if p.missing.label != 0 {
		exps = []ExpID{p.missing.label}
	}
	e := p.MakeError(p.Pos, exps)
	p.installMissing(missingToken{site: site, pos: p.Pos, err: e}, true)
	p.PushRecovered(e)
	return e, true
}

// NewMissingNode returns the missing node of the insertion of the literal expected whose error is e, at
// start: an empty terminal of type missing with the fields expected and message.
//
// NewMissingNode is for generated parsers; not a stable API.
func (p *Parser) NewMissingNode(start int, expected string, e *sxirvalue.SyntaxError) *sxirvalue.Node {
	return p.NewNode(Data{Kind: valueimpl.MissingKind, Start: int32(start), End: int32(start),
		Fields: valueimpl.Fields{{Name: "expected", Value: expected}, {Name: "message", Value: e.Error()}}, Fresh: true, Terminal: true})
}

// MissingScope enters an @error label around #missing sites with the message msg: in the missing-token
// mode of a parse that records expectations, where no label is entered, msg becomes the expectation of the
// insertions inside. It returns the label entered before, which MissingScopeEnd restores, as an int, the
// type of the Flow IR operation (flow.missingScope).
//
// MissingScope is for generated parsers; not a stable API.
func (p *Parser) MissingScope(msg ExpID) int {
	if !p.inserting() {
		return 0
	}
	prev := p.missing.label
	if p.Expectations && prev == 0 {
		p.missing.label = msg
	}
	return int(prev)
}

// MissingScopeEnd leaves the @error label that MissingScope entered, restoring the label prev that it
// returned.
//
// MissingScopeEnd is for generated parsers; not a stable API.
func (p *Parser) MissingScopeEnd(prev int) {
	if p.inserting() {
		p.missing.label = ExpID(prev)
	}
}

// missingSince returns a copy of the insertions on the path after the first n, nil if there are none,
// counting one compute unit for each insertion it copies before it copies them (spec/runtime/parsing.md,
// Compute Units).
func (p *Parser) missingSince(n int) missingSeed {
	if !p.inserting() {
		return nil
	}
	s := p.missing
	if n >= len(s.path) {
		return nil
	}
	p.unitsSized(len(s.path) - n)
	return missingSeed(append([]missingToken(nil), s.path[n:]...))
}

// missingSeedOf returns the insertions on the path after the first n as missingSince does, for a seed
// of a growth, which the parse keeps: their bytes count against the memory limit.
func (p *Parser) missingSeedOf(n int) missingSeed {
	seed := p.missingSince(n)
	if seed != nil {
		p.missing.seedBytes += uint64(cap(seed)) * missingTokenBytes
	}
	return seed
}

// replayMissing records again the insertions seed that a reused result made and the errors errs that it
// recovered. An insertion whose site already inserted at its position on the path is not put on it
// again, and its error is left out: the errors are then recorded one by one. The restore counts one
// compute unit per insertion of seed, and under such a duplicate one per error of errs, including those
// left out (spec/runtime/parsing.md#compute-units). Recording an insertion again takes an attempt of the
// limit if charge is set, as a reuse from the memo does; the selection of an operator candidate does
// not, since the candidate's insertions took theirs.
func (p *Parser) replayMissing(seed missingSeed, errs recEntry, charge bool) {
	if len(seed) == 0 {
		if !errs.none() {
			p.recAdd(errs)
		}
		return
	}
	p.unitsSized(len(seed))
	var dup map[*sxirvalue.SyntaxError]bool
	for _, t := range seed {
		if !p.installMissing(t, charge) {
			if dup == nil {
				dup = make(map[*sxirvalue.SyntaxError]bool)
			}
			dup[t.err] = true
		}
	}
	if dup == nil {
		if !errs.none() {
			p.recAdd(errs)
		}
		return
	}
	all := appendRecErrors(nil, []recEntry{errs})
	p.unitsSized(len(all))
	for _, e := range all {
		if !dup[e] {
			p.PushRecovered(e)
		}
	}
}

// insertsRule reports that the results of r depend on the insertions before them in this parse: r
// contains #missing, directly or through the rules it calls, in the missing-token mode. Such a rule is
// evaluated again at every call, in a context of left recursion as outside one.
func (p *Parser) insertsRule(r *Rule) bool { return r.Missing && p.inserting() }

// missingSilent reports that a call of r runs in a lookahead of the missing-token mode, where no site
// inserts: a growth of r there has seeds of its own.
func (p *Parser) missingSilent(r *Rule) bool { return p.Silent > 0 && p.insertsRule(r) }
