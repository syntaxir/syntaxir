package state

import (
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/runtime/value"
	valueimpl "syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// The parts of the skip runs that only the engine has: its element types (the engine's nodes), its
// budget and its tuning (the mutants of Tuning.SkipMut, the gate of the skip runs, and the hot span),
// the examined range of a parse, which it always tracks, and the entry points of its walks. The shared
// skip-run mechanisms (skip_walks.go, skip_records.go, skip_vals.go) call the hooks below, which the
// library's file of the same name implements for its own state.

// The mutants of Tuning.SkipMut, under the names the engine's tests and tuning use. They are the shared
// mechanisms' constants (skip_records.go), which the library never sets.
const (
	SkipKeepPos      = skipKeepPos
	SkipNoExpected   = skipNoExpected
	SkipNoExamined   = skipNoExamined
	SkipNoFrom       = skipNoFrom
	SkipNeverRecord  = skipNeverRecord
	SkipEarlyStore   = skipEarlyStore
	SkipIgnoreEnv    = skipIgnoreEnv
	SkipKeepEnv      = skipKeepEnv
	SkipAnyList      = skipAnyList
	SkipKeyEnvObject = skipKeyEnvObject
	SkipScanKeys     = skipScanKeys
)

// PruneSkipRecords drops the records of the positions before pos, where a stream that committed at pos
// never parses again.
func (p *Parser) PruneSkipRecords(pos int) { p.pruneSkipRecords(pos) }

// recVals are the elements of a record: the nodes of the engine's values.
type recVals = valSuffix[*value.Node]

// valBytes returns the bytes of the buffer of the elements of r unless seen has it, and adds it to
// seen (recountSkipBytes).
func (r *skipRecord) valBytes(seen map[any]struct{}) uint64 { return r.vals.bufBytes(seen) }

// skipVals are the elements of a walk: the nodes of the engine's values.
type skipVals = walkVals[*value.Node]

// valBegin resets the elements of the walk that begins.
func (w *skipWalk) valBegin() {
	w.vals.vals = w.vals.vals[:0]
	w.vals.placed = false
}

// valCount returns the number of elements the walk collected.
func (w *skipWalk) valCount() int { return len(w.vals.vals) }

// valPlace places the elements of the walk, then those of the record it took (hit), for its list.
func (w *skipWalk) valPlace(p *Parser, hit bool) {
	w.vals.buf, w.vals.lo = placeVals(p, w.vals.vals, w.hit.vals, hit)
	w.vals.placed = true
}

// valLink sets the elements of the record r of the visit at vi elements: the placed list from there on.
func (w *skipWalk) valLink(r *skipRecord, vi int) {
	if i := w.vals.lo + vi; w.vals.placed && i < len(w.vals.buf.a) {
		r.vals = valSuffix[*value.Node]{w.vals.buf, i} // (an empty suffix keeps nothing alive)
	}
}

// valCap returns the capacity of the elements of the walk, which the memory limit counts.
func (w *skipWalk) valCap() int { return cap(w.vals.vals) }

// valEnd drops the elements of the walk that ended.
func (w *skipWalk) valEnd() {
	clear(w.vals.vals)
	w.vals.vals, w.vals.buf = w.vals.vals[:0], nil
}

// skipOn reports whether the parse records skip runs: it does not under Tuning.SkipOff, nor when it
// inserts missing tokens.
func (p *Parser) skipOn() bool { return !p.tune.SkipOff && p.Missing == nil }

// skipMut reports whether the parse breaks the part of the records that k names (Tuning.SkipMut).
func (p *Parser) skipMut(k int) bool { return p.tune.SkipMut == k }

// mutateSkipRecord breaks the record r reused at pos as the parse's mutant names: the record keeps
// nothing of the part that the mutant leaves out.
func (p *Parser) mutateSkipRecord(r *skipRecord, pos int) {
	switch p.tune.SkipMut {
	case SkipKeepPos:
		r.end = pos
	case SkipNoExpected:
		r.exp = nil
	case SkipNoExamined:
		r.examined = pos
	case SkipNoFrom:
		r.from = pos
	}
}

// noteStored records that a skip record starts at pos, for the memory limit of a budget. p may be nil.
func (p *Parser) noteStored(pos int) {
	if BudgetControlsEnabled && p != nil && p.Control != nil {
		p.Control.noteStored(pos)
	}
}

// runHotSpan is the hot span of the run sites of the parse (Tuning.RunHotSpan).
func (p *Parser) runHotSpan() int { return p.tune.RunHotSpan() }

// noteHot counts a run site that turned hot.
func (p *Parser) noteHot() {
	if st := p.tune.Stats; st != nil {
		st.RunHotTurns.Add(1)
	}
}

// examinedRange returns the input range [from, examined) that the current evaluation examined (lw, Hw).
func (p *Parser) examinedRange() (from, examined int) { return p.lw, p.Hw }

// isolateRange starts the examined range of an iteration at pos.
func (p *Parser) isolateRange(pos int) { p.lw, p.Hw = pos, pos }

// widenRange extends the examined range back to from and forward to examined.
func (p *Parser) widenRange(from, examined int) {
	p.Hw, p.lw = max(examined, p.Hw), min(from, p.lw)
}

// bindVar sets the variable slot to val.
func (p *Parser) bindVar(slot int, val any) { p.Bind(slot, val) }

// envSlotValues returns the current values of the variables in slots.
func (p *Parser) envSlotValues(slots []int) []any { return p.EnvValues(slots) }

// hashWalkKey hashes the values of the variables of a walk's key, as a memo key of a rule that is not
// silent.
func (p *Parser) hashWalkKey(vals []any) uint64 { return p.hashMemoKey(0, 0, false, vals) }

// runKeyOf returns the key of the run site, which is keyed by the variables.
func (p *Parser) runKeyOf(site int) *runKeySlots { return p.Prog.runKeySlots(site) }

// suffixCopied counts the units of a copy of n elements of a record's suffix under limit controls:
// a suffix copy is an event of one unit for each element, counted before the copy
// (spec/runtime/parsing.md#run-sites). copySpend counts nothing in the engine.
func (p *Parser) suffixCopied(n int) {
	if p.controlled() {
		p.Control.Charge(uint64(n))
	}
}

// countsWalkKeys reports whether the parse counts the keys of its walks: under limit controls.
func (p *Parser) countsWalkKeys() bool { return p.controlled() }

// countWalkKey counts the n units of a walk's key (walkKeyUnits).
func (p *Parser) countWalkKey(n uint64) { p.Control.Charge(n) }

// skipStep is a step of the bookkeeping of a walk, which is no event: it counts nothing.
func (p *Parser) skipStep() {}

// SkipBegin starts a walk of the site at the current position. keyEnv reports that the records of the
// site are keyed by the variables (skipWalk.key).
func (p *Parser) SkipBegin(site int, keyEnv bool) { p.skipBegin(site, false, keyEnv) }

// SkipBeginList starts a walk of the site, whose iterations have values (SkipValue) that SkipEndList
// gathers in a list.
func (p *Parser) SkipBeginList(site int, keyEnv bool) { p.skipBegin(site, true, keyEnv) }

// SkipValue adds the value of the iteration that matched. Nodes in records are shared by the lists
// that reuse them, so a node that the rule that made it may still change is final from here.
func (p *Parser) SkipValue(v *value.Node) {
	w := p.walk()
	if v != nil {
		nodes.D(v).Fresh = false
	}
	w.vals.vals = append(w.vals.vals, v)
}

// SkipEndList ends a walk begun with SkipBeginList as SkipEnd does, and returns the list node of the
// values of its iterations for the range from start to the current position (SkipKids), or nil if
// build is false, dropping the gathered values uncharged.
func (p *Parser) SkipEndList(start int, build bool) *value.Node {
	if build {
		kids := p.SkipKids()
		return p.NewNode(nodes.Data{Kind: valueimpl.ListKind, Start: int32(start), End: int32(p.Pos), Children: kids, Fresh: true})
	}
	p.SkipEnd()
	return nil
}

// SkipEndDrop is flow.skipEnd: it ends the walk as SkipEnd does and drops the values that a walk begun
// with SkipBeginList gathered, uncharged.
func (p *Parser) SkipEndDrop() { p.SkipEndList(0, false) }

// SkipKids is flow.skipKids: it ends a walk begun with SkipBeginList as SkipEnd does, and returns the
// values of its iterations, then those of the record it took, as a child list. The list may share its
// elements with the lists of other walks. Like Parser.Kids it charges nothing: the values that the parse
// gathers, takes off, copies into records and drops are not an item of the cost
// (spec/runtime/parsing.md#cost); each is moved a constant number of times for the iteration that
// gathered it or the record that holds it.
func (p *Parser) SkipKids() []*value.Node {
	p.skipEnd()
	w := p.walk()
	kids := endVals(p, &w.vals, p.Nodes)
	w.valEnd()
	p.skipN--
	return kids
}

// get and put are getSkip and putSkip for the slots of a site whose walks are not keyed by the
// variables, such as a scan site; env is always nil.
func (s *skipSlots) get(pos int, env *Env) skipRecord {
	if env != nil {
		panic("skipSlots.get: a keyed walk goes through Parser.getSkip")
	}
	return (*Parser)(nil).getSkip(s, pos, walkKey{})
}

func (s *skipSlots) put(pos int, env *Env, r skipRecord) {
	if env != nil {
		panic("skipSlots.put: a keyed walk goes through Parser.putSkip")
	}
	(*Parser)(nil).putSkip(s, pos, walkKey{}, r)
}
