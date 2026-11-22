package libgo

import sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"

// The parts of the skip runs that only the library has: its element types (a Node parse collects
// nodes, a typed one any values), the examined range of a Document parse, and the entry points of
// generated parsers. The shared skip-run mechanisms (skip_walks.go, skip_records.go, skip_vals.go) call
// the hooks below, which the engine's file of the same name implements for its own state.

// RunHotSpan is the length of input that a walk of a run site may walk again, below the furthest point
// that the walks before it reached, before the site turns hot: the engine's, a constant of the charge
// model ("Run Sites" in spec/runtime/parsing.md). A walk shorter than that neither turns a site hot nor
// can walk that far again, so generated code passes only the longer ones to RunWalked. Tests that run the
// hot path on short inputs build their parsers against a copy of the library with a lower value.
const RunHotSpan = 64

// recVals are the elements of a record: nodes in a Node parse, tvals in a typed one.
type recVals struct {
	nodes valSuffix[*sxirvalue.Node]
	tvals valSuffix[any]
}

// skipVals are the elements of a walk: nodes in a Node parse, tvals in a typed one (typed).
type skipVals struct {
	nodes walkVals[*sxirvalue.Node]
	tvals walkVals[any]
	typed bool
}

// valBegin resets the elements of the walk that begins.
func (w *skipWalk) valBegin() {
	w.vals.typed = false
	w.vals.nodes.placed, w.vals.tvals.placed = false, false
	w.vals.nodes.vals, w.vals.tvals.vals = w.vals.nodes.vals[:0], w.vals.tvals.vals[:0]
}

// valCount returns the number of elements the walk collected.
func (w *skipWalk) valCount() int {
	if w.vals.typed {
		return len(w.vals.tvals.vals)
	}
	return len(w.vals.nodes.vals)
}

// valPlace places the elements of the walk, then those of the record it took (hit), for its list.
func (w *skipWalk) valPlace(p *Parser, hit bool) {
	if w.vals.typed {
		w.vals.tvals.buf, w.vals.tvals.lo = placeVals(p, w.vals.tvals.vals, w.hit.vals.tvals, hit)
		w.vals.tvals.placed = true
		return
	}
	w.vals.nodes.buf, w.vals.nodes.lo = placeVals(p, w.vals.nodes.vals, w.hit.vals.nodes, hit)
	w.vals.nodes.placed = true
}

// valLink sets the elements of the record r of the visit at vi elements: the placed list from there on.
func (w *skipWalk) valLink(r *skipRecord, vi int) {
	if i := w.vals.nodes.lo + vi; w.vals.nodes.placed && i < len(w.vals.nodes.buf.a) {
		r.vals.nodes = valSuffix[*sxirvalue.Node]{w.vals.nodes.buf, i} // (an empty suffix keeps nothing alive)
	}
	if i := w.vals.tvals.lo + vi; w.vals.tvals.placed && i < len(w.vals.tvals.buf.a) {
		r.vals.tvals = valSuffix[any]{w.vals.tvals.buf, i}
	}
}

// valCap returns the capacity of the elements of the walk, which the memory limit counts.
func (w *skipWalk) valCap() int { return cap(w.vals.nodes.vals) + cap(w.vals.tvals.vals) }

// valEnd drops the elements of the walk that ended.
func (w *skipWalk) valEnd() {
	clear(w.vals.nodes.vals)
	clear(w.vals.tvals.vals)
	w.vals.nodes.vals, w.vals.nodes.buf = w.vals.nodes.vals[:0], nil
	w.vals.tvals.vals, w.vals.tvals.buf = w.vals.tvals.vals[:0], nil
}

// skipOn reports whether the parse records skip runs: the library always does.
func (p *Parser) skipOn() bool { return true }

// skipMut reports whether the parse breaks the part of the records that k names: the library breaks none.
func (p *Parser) skipMut(int) bool { return false }

// mutateSkipRecord breaks the record r reused at pos as the parse's mutant names: none for the library.
func (p *Parser) mutateSkipRecord(*skipRecord, int) {}

// noteStored records that a skip record starts at pos, for the memory limit of a budget.
func (p *Parser) noteStored(pos int) {
	if p.countsMemory() {
		p.Control.noteStored(pos)
	}
}

// runHotSpan is the hot span of the run sites of the parse: RunHotSpan.
func (p *Parser) runHotSpan() int { return RunHotSpan }

// noteHot records that a run site turned hot. The library counts nothing.
func (p *Parser) noteHot() {}

// examinedRange returns the input range [from, examined) that the active rule call examined in a
// Document parse (docLow, docExamined). Each is tracked only where the parse tracks it.
func (p *Parser) examinedRange() (from, examined int) { return p.docLow, p.docExamined }

// isolateRange starts the examined range of an iteration at pos, which the Document parse tracks.
func (p *Parser) isolateRange(pos int) {
	if p.Document != nil {
		p.docLow = pos
	}
	if p.track {
		p.docExamined = pos
	}
}

// widenRange extends the examined range back to from and forward to examined.
func (p *Parser) widenRange(from, examined int) {
	p.docLow, p.docExamined = min(from, p.docLow), max(examined, p.docExamined)
}

// bindVar sets the variable slot to val (a walk that reuses a record binds the variables it left).
func (p *Parser) bindVar(slot int, val any) { p.bind(slot, val) }

// envSlotValues returns the current values of the variables in slots.
func (p *Parser) envSlotValues(slots []int) []any { return p.envValues(slots) }

// hashWalkKey hashes the values of the variables of a walk's key.
func (p *Parser) hashWalkKey(vals []any) uint64 { return p.hashMemoKey(0, 0, vals) }

// runKeyOf returns the key of the run site, which is keyed by the variables.
func (p *Parser) runKeyOf(site int) *runKeySlots { return p.g.runKey(site) }

// suffixCopied counts the units of a copy of n elements of a record's suffix, under logical units.
func (p *Parser) suffixCopied(n int) {
	if p.units.on {
		p.unitsSized(n) // a suffix copy (design record 076)
	}
}

// countsWalkKeys reports whether the parse counts the keys of its walks: under logical units.
func (p *Parser) countsWalkKeys() bool { return p.units.on }

// countWalkKey counts the n units of a walk's key (walkKeyUnits) and checks the counter.
func (p *Parser) countWalkKey(n uint64) { p.unitsSized(int(n)) }

// skipStep is a step of the bookkeeping of a walk, which is no logical event: it counts nothing.
func (p *Parser) skipStep() {}

// SkipBegin starts a walk of the skip-run site at the current position.
func (p *Parser) SkipBegin(site int) { p.skipBegin(site, false, false) }

// RunBegin starts a walk of the hot run site at the current position. build reports that its
// iterations have values (SkipValue), which SkipKids gathers in a list, and keyEnv that its records
// are keyed by the values of the variables of the site's run key (Grammar.SetRunKey).
func (p *Parser) RunBegin(site int, build, keyEnv bool) { p.skipBegin(site, build, keyEnv) }

// RunBegin is the Parser's RunBegin in a typed parse, whose values SkipValue and SkipKids take.
func (p *TParser) RunBegin(site int, build, keyEnv bool) {
	p.skipBegin(site, build, keyEnv)
	p.walk().vals.typed = true
}

// SkipValue adds the value of the iteration that matched. Nodes in records are shared by the lists
// that reuse them, so a node that the rule that made it may still change is final from here.
func (p *Parser) SkipValue(v *sxirvalue.Node) {
	w := p.walk()
	if v != nil {
		D(v).Fresh = false
	}
	w.vals.nodes.vals = append(w.vals.nodes.vals, v)
}

// SkipValue is the Parser's SkipValue for a typed value.
func (p *TParser) SkipValue(v any) {
	w := p.walk()
	if n := AsTval(v); n != nil {
		n.TSetFresh(false)
	}
	w.vals.tvals.vals = append(w.vals.tvals.vals, v)
}

// SkipKids is flow.skipKids: it ends a walk begun with build as SkipEnd does, and returns the values of
// its iterations, then those of the record it took, as a child list. The list may share its elements
// with the lists of other walks. Like Kids it charges nothing: the values that the parse gathers,
// takes off, places in records and drops are not an item of the cost (spec/runtime/parsing.md#cost);
// each is moved a constant number of times for the iteration that gathered it or the record that holds it.
func (p *Parser) SkipKids() []*sxirvalue.Node {
	p.skipEnd()
	w := p.walk()
	kids := endVals(p, &w.vals.nodes, p.Nodes)
	w.valEnd()
	p.skipN--
	return kids
}

// SkipKids is the Parser's SkipKids in a typed parse.
func (p *TParser) SkipKids() []any {
	p.skipEnd()
	w := p.walk()
	kids := endVals(p.Parser, &w.vals.tvals, p.NewVals)
	w.valEnd()
	p.skipN--
	return kids
}
