package libgo

import "unsafe"

// Skip runs: the walks of skip-run sites, which record what the rest of a walk from each position did
// (see skip_records.go; "Skip Runs" in spec/runtime/parsing.md and the proofs in
// spec/proofs/skip-runs.md).
//
// A walk of a site repetition visits positions r0 < r1 < ... with one iteration each. The slot of
// a position in the table of the site (and lookahead state) is fresh, visited or recorded. A walk
// starts plain: it marks each position it leaves after a matching iteration as visited and records
// nothing, so a walk that fails at once touches no slot. At its first iteration at a position that is
// not fresh, which an earlier walk of the site visited, it starts recording: from there on, for every
// position it visits, it records what the rest of the walk from there did (where it ended, its
// farthest failure and the expectations there, and the input range it examined). A recording walk
// that reaches a recorded position (once it has matched its minimum) takes the rest from the record:
// it moves to the end, records the expectations again and extends the examined range, as a memo hit
// does. The record of a position combines the iteration there with the record of the next position,
// so a walk fills the records of its recording visits backward when it ends: at a failed or empty
// iteration, whose record ends at its own start, or at a recorded position. A plain walk thus pays
// for nothing but its marks, and a position is walked at most once before it is marked and once more
// before it has a record, besides the last iteration of a plain walk and the minimum number of
// iterations of each walk.
//
// A run site walks as an ordinary repetition until its walks overlap (RunHot, RunWalked); the walks of
// a hot site are the walks above. The records of a parse last for that parse; a stream drops those
// before what it committed. A parse with missing-token insertion records nothing.

// runCold is the state of a run site (and lookahead state) before it keeps records: the furthest
// position that its walks passed, among the walks that RunWalked was told of. A site turns hot when a
// walk begins below that position and walks the hot span or more of the input below it. A walk of a
// site that is not hot costs its length, and the part of it below the furthest position is under the
// hot span or the site turns hot, so the walks of a cold site cost in total at most the input length
// plus the hot span for each walk: linear in the input and the number of walks, however the walks
// alternate. A site that turns hot pays for records instead, and a run site whose walks never go back,
// as in a list parsed once, allocates nothing.
type runCold struct {
	end int
	hot bool
}

// runIndex is the index of the state of the site in the table of the parser's runCold: a lookahead
// state has its own.
func (p *Parser) runIndex(site int) int {
	if p.Silent > 0 {
		return 2*site + 1
	}
	return 2 * site
}

// RunHot reports whether the walks of the run site keep records. A site turns hot only through
// RunWalked, which does nothing when records are off or the parse inserts missing tokens. A walk of a
// site that is not hot is an ordinary repetition, whose end the caller passes to RunWalked.
func (p *Parser) RunHot(site int) bool {
	i := p.runIndex(site)
	return i < len(p.runCold) && p.runCold[i].hot
}

// RunWalked ends the walk of a run site that was not hot, which began at start and ended at the
// current position. A walk that began below the furthest position that the walks of the site passed
// walks input again, and the site turns hot when it walks the hot span of it below that position.
func (p *Parser) RunWalked(site, start int) { p.walked(site, start, p.runHotSpan()) }

// walked is RunWalked for a site whose walks turn it hot at span characters below the furthest position.
func (p *Parser) walked(site, start, span int) {
	if !p.skipOn() {
		return
	}
	i := p.runIndex(site)
	if i >= len(p.runCold) {
		// One table for all the sites: grown to twice the index, it is made once for most programs.
		grown := make([]runCold, max(16, 2*(i+1)))
		copy(grown, p.runCold)
		p.runCold = grown
	}
	c := &p.runCold[i]
	if start < c.end && min(p.Pos, c.end)-start >= span {
		c.hot = true
		p.noteHot()
	}
	c.end = max(c.end, p.Pos)
}

// skipVisit is a recording visit of the walk in progress.
type skipVisit struct {
	pos, far       int
	exp            []ExpID
	from, examined int
	// key is the key the iteration started with (none for a site not keyed by the variables), and vi the
	// number of elements collected before it. e0 is the number of entries of the record of recovered
	// errors before it, and errs the errors it recovered.
	key    walkKey
	vi, e0 int
	errs   recEntry
}

// skipWalk is a walk in progress. Walks nest: the element of a run site can call rules that walk
// sites of their own, so the walks in progress form a stack (Parser.skips), and the innermost one is
// the one in use.
type skipWalk struct {
	// on reports that the walk records (the parser's skipOn), and is not ended.
	on bool
	// key holds the slots of the variables that key the records of the site (runKeySlots), nil for a
	// site whose element reads or binds no variable, directly or in the rules it calls: its records
	// hold for every environment, which a walk keeps. A walk that reuses the record of a keyed site
	// binds the variables of the key's bind to the values the walk of the record left in them.
	key *runKeySlots
	// recording reports that the walk records its visits (it met a position that is not fresh).
	recording bool
	// table is the index of the slots in the parser's skipTables, and slots the slots, nil until a
	// walk of the site first writes one.
	table int
	slots *skipSlots
	// prev is the position of the last iteration of a plain walk, if prevOK; the walk marks it when
	// the iteration has matched.
	prev   int
	prevOK bool
	visits []skipVisit
	// open reports that the last visit's iteration is running: its expectations are isolated from mk,
	// and the examined range from before it is savedFrom and savedExamined.
	open                     bool
	mk                       ExpMark
	savedFrom, savedExamined int
	hit                      skipRecord
	hitOK                    bool
	// build reports that the walk collects the values of its iterations, whose list the end of the walk
	// makes (the walk's vals).
	build bool
	vals  skipVals
}

// walkKeyUnits returns the units of a walk's key with the values vals: one for each variable, and one
// for each byte of the UTF-8 encoding of each string value among them
// (spec/runtime/parsing.md#compute-units).
func walkKeyUnits(vals []any) uint64 {
	n := uint64(len(vals))
	for _, v := range vals {
		if s, ok := v.(string); ok {
			n += uint64(len(s))
		}
	}
	return n
}

// walk returns the walk in use, the innermost one in progress.
func (p *Parser) walk() *skipWalk { return &p.skips[p.skipN-1] }

// skipBegin starts a walk of the site at the current position. build reports that its iterations have
// values (SkipValue), which the end of the walk gathers in a list, and keyEnv that its records are keyed
// by the variables of the site's run key.
func (p *Parser) skipBegin(site int, build, keyEnv bool) {
	p.use(featureSkipRuns)
	if p.skipN == len(p.skips) {
		p.skips = append(p.skips, skipWalk{})
	}
	p.skipN++
	w := p.walk()
	w.build = build
	w.valBegin()
	w.on = p.skipOn()
	if !w.on {
		return
	}
	w.key = nil
	if keyEnv {
		w.key = p.runKeyOf(site)
		if len(w.key.bind) == 0 && !p.skipMut(skipIgnoreEnv) && p.countsWalkKeys() {
			// The element binds no variable of the key, so the key keeps its values for the whole
			// walk: it is counted once, when the walk starts (spec/runtime/parsing.md#compute-units).
			p.countWalkKey(walkKeyUnits(p.envSlotValues(w.key.vars)))
		}
	}
	i := p.runIndex(site)
	w.table, w.slots = i, nil
	if i < len(p.skipTables) {
		w.slots = p.skipTables[i]
	}
	w.visits, w.recording, w.prevOK, w.open, w.hitOK = w.visits[:0], false, false, false, false
}

// skipTable returns the slots of the walk, making them.
func (p *Parser) skipTable() *skipSlots {
	w := p.walk()
	if w.slots == nil {
		if w.table >= len(p.skipTables) {
			p.skipTables = append(p.skipTables, make([]*skipSlots, w.table+1-len(p.skipTables))...)
		}
		w.slots = &skipSlots{}
		p.skipTables[w.table] = w.slots
	}
	return w.slots
}

// walkKey returns the key of the walk at the current position: the values of the variables of the key
// and their hash for a site keyed by them, none otherwise. Reading and hashing the values are no event
// (hashWalkKey may charge budget steps); skipBegin or SkipHit counts the key (countWalkKey).
func (p *Parser) walkKey() walkKey {
	k := p.walk().key
	if k == nil || p.skipMut(skipIgnoreEnv) {
		return walkKey{}
	}
	p.chargeValueBytes(len(k.vars) * int(anyBytes))
	vals := p.envSlotValues(k.vars)
	h := p.hashWalkKey(vals)
	if p.skipMut(skipKeyEnvObject) {
		// The object is compared by identity (rawValueEqual) and hashed by its address.
		vals = append(vals, p.Env)
		h ^= uint64(uintptr(unsafe.Pointer(p.Env))) * 0x9e3779b97f4a7c15
	}
	return walkKey{vals, h}
}

// skipMarkPrev marks the position of the last iteration of a plain walk, which matched.
func (p *Parser) skipMarkPrev() {
	w := p.walk()
	if w.prevOK {
		p.putSkip(p.skipTable(), w.prev, walkKey{}, skipRecord{state: skipVisited})
		w.prevOK = false
	}
}

// skipRecording reports whether the walk records from the current position on, which it starts to at
// the first position that is not fresh.
func (p *Parser) skipRecording() bool {
	w := p.walk()
	if !w.recording && w.slots.marked(p.Pos) && !p.skipMut(skipNeverRecord) {
		p.skipMarkPrev()
		w.recording = true
	}
	return w.recording
}

// SkipHit is called before an iteration after the minimum. It ends the iteration before and, in a
// recording walk, reports whether the current position has a record, which it then reuses. The walk
// then stops.
func (p *Parser) SkipHit() bool {
	w := p.walk()
	if !w.on || !p.skipRecording() {
		return false
	}
	p.skipClose()
	p.skipStep()
	k := p.walkKey()
	if w.key != nil && len(w.key.bind) > 0 && p.countsWalkKeys() {
		// The element binds variables of the key, whose values may differ at each position: the key
		// is counted at each lookup (spec/runtime/parsing.md#compute-units).
		p.countWalkKey(walkKeyUnits(k.vals))
	}
	r := p.getSkip(w.slots, p.Pos, k)
	if r.state != skipRecorded || w.build && !r.list && !p.skipMut(skipAnyList) {
		return false
	}
	p.skipStep()
	p.mutateSkipRecord(&r, p.Pos)
	p.touchRange(r.examined, r.from)
	p.MergeExpected(r.far, r.exp)
	p.Pos = r.end
	if w.key != nil && !p.skipMut(skipKeepEnv) {
		for i, slot := range w.key.bind {
			p.bindVar(slot, r.bind[i])
		}
	}
	if r.errs != nil {
		p.recAdd(*r.errs)
	}
	w.hit, w.hitOK = r, true
	return true
}

// SkipVisit begins an iteration at the current position: in a plain walk it marks the position of the
// iteration before, which matched, visited; in a recording walk it ends the iteration before and
// isolates this one.
func (p *Parser) SkipVisit() {
	w := p.walk()
	if !w.on {
		return
	}
	if !p.skipRecording() {
		p.skipMarkPrev()
		w.prev, w.prevOK = p.Pos, true
		return
	}
	p.skipClose()
	w.visits = append(w.visits, skipVisit{pos: p.Pos, key: p.walkKey(), vi: w.valCount(), e0: len(p.Recovered)})
	w.open = true
	w.savedFrom, w.savedExamined = p.examinedRange()
	p.isolateRange(p.Pos)
	w.mk = p.Isolate(p.Pos)
}

// skipClose ends the running iteration, if any: it keeps its expectations and examined range and
// records them as usual.
func (p *Parser) skipClose() {
	w := p.walk()
	if !w.open {
		return
	}
	w.open = false
	far, inner := p.Unisolate(w.mk)
	v := &w.visits[len(w.visits)-1]
	v.far, v.exp = far, p.Keep(inner)
	p.MergeExpected(far, inner)
	v.from, v.examined = p.examinedRange()
	v.errs = p.recSince(v.e0)
	p.widenRange(w.savedFrom, w.savedExamined)
	if p.skipMut(skipEarlyStore) {
		p.putSkip(p.skipTable(), v.pos, v.key, skipRecord{end: p.Pos, far: v.far, exp: v.exp, from: v.from, examined: v.examined, state: skipRecorded, bind: p.walkBind()})
	}
}

// SkipEnd ends the walk and records the rest of it from each position it visited recording.
func (p *Parser) SkipEnd() {
	p.skipEnd()
	p.walk().valEnd()
	p.skipN--
}

// skipEnd ends the walk and records the rest of it from each position it visited recording.
func (p *Parser) skipEnd() {
	w := p.walk()
	if !w.on {
		return
	}
	p.skipClose()
	w.on = false
	if p.skipMut(skipEarlyStore) {
		w.visits = w.visits[:0]
		return
	}
	vs := w.visits
	endBind := p.walkBind()
	var next skipRecord
	if w.hitOK && !p.skipMut(skipKeepEnv) {
		endBind = w.hit.bind
	}
	if w.build && (len(vs) > 0 || w.hitOK) {
		w.valPlace(p, w.hitOK)
	}
	switch {
	case w.hitOK:
		next = w.hit
	case len(vs) > 0:
		// The last iteration failed or matched nothing: the walk ended at its start.
		v := vs[len(vs)-1]
		vs = vs[:len(vs)-1]
		next = skipRecord{end: v.pos, far: v.far, exp: v.exp, from: v.from, examined: v.examined, state: skipRecorded, list: w.build, bind: endBind}
		p.skipLink(w, &next, v, nil)
		p.skipPut(v.pos, v.key, next)
	}
	for i := len(vs) - 1; i >= 0; i-- {
		v := vs[i]
		r := skipRecord{end: next.end, far: next.far, exp: next.exp, from: min(v.from, next.from), examined: max(v.examined, next.examined), state: skipRecorded, list: w.build, bind: endBind}
		switch {
		case v.far > next.far:
			r.far, r.exp = v.far, v.exp
		case v.far == next.far:
			r.exp = mergeExp(v.exp, next.exp)
		}
		p.skipLink(w, &r, v, next.errs)
		p.skipPut(v.pos, v.key, r)
		next = r
	}
	w.visits = vs[:0]
}

// skipLink sets the elements and errors of the record r of the visit v: the elements of the walk from
// the iteration on, and the errors of the iteration before those of the rest, rest.
func (p *Parser) skipLink(w *skipWalk, r *skipRecord, v skipVisit, rest *recEntry) {
	w.valLink(r, v.vi)
	switch {
	case v.errs.none():
		r.errs = rest
	case rest == nil:
		e := v.errs
		r.errs = &e
		r.errBytes = uint32(recEntryBytes)
		p.skipBytes += recEntryBytes
	default:
		// The list of an iteration's errors and the rest's: a hit records the rest of the walk as one
		// entry, and each list holds an error of its own iteration, so listing k errors still visits
		// at most 2k lists.
		l := &errList{entries: []recEntry{v.errs, *rest}, n: v.errs.len() + rest.len()}
		r.errs = &recEntry{list: l}
		r.errBytes = uint32(errListBytes + 3*recEntryBytes)
		p.skipBytes += errListBytes + 3*recEntryBytes
	}
}

// mergeExp returns the expectations a, then those of b not in a.
func mergeExp(a, b []ExpID) []ExpID {
	if len(a) == 0 {
		return b
	}
	out := a
	for _, x := range b {
		found := false
		for _, y := range a {
			if x == y {
				found = true
				break
			}
		}
		if !found {
			if len(out) == len(a) {
				out = append(make([]ExpID, 0, len(a)+len(b)), a...)
			}
			out = append(out, x)
		}
	}
	return out
}

// skipPut stores the record r of the position pos in the slots of the walk.
func (p *Parser) skipPut(pos int, k walkKey, r skipRecord) {
	p.skipStep()
	p.putSkip(p.skipTable(), pos, k, r)
}

// walkBind returns the values of the variables that the element of the walk's site binds, in the
// order of the run key's bind, none for a site not keyed by the variables.
func (p *Parser) walkBind() []any {
	k := p.walk().key
	if k == nil || len(k.bind) == 0 {
		return nil
	}
	p.chargeValueBytes(len(k.bind) * int(anyBytes))
	return p.envSlotValues(k.bind)
}

// pruneSkipRecords drops the records of the positions before pos, where a stream that committed at
// pos never parses again, and recounts what the records that remain hold (recountSkipBytes).
func (p *Parser) pruneSkipRecords(pos int) {
	for _, s := range p.skipTables {
		if s != nil {
			s.drop(pos)
		}
	}
	p.recountSkipBytes()
}

// recountSkipBytes replaces skipBytes, under a memory limit, with what the records of the skip tables
// hold: the buffers of elements that they share, each once, and the error entries that each made.
// skipBytes counts each buffer and entry where a walk makes it, and nothing takes them off when the
// records that hold them are dropped or stored over; a stream prunes its records with no walk in
// progress, so the records hold all that skipBytes counts that is still held.
func (p *Parser) recountSkipBytes() {
	if !p.countsMemory() {
		return
	}
	var n uint64
	bufs := make(map[any]struct{})
	count := func(r *skipRecord) {
		n += uint64(r.errBytes) + r.valBytes(bufs)
	}
	for _, s := range p.skipTables {
		if s == nil {
			continue
		}
		for _, r := range s.recs {
			count(&r)
		}
		for _, rs := range s.more {
			for i := range rs {
				count(&rs[i])
			}
		}
	}
	p.skipBytes = n
}
