package libgo

import "unsafe"

// The records of the walks of skip-run sites (skip_walks.go; see "Skip Runs" in spec/runtime/parsing.md
// and the proofs in spec/proofs/skip-runs.md). The slot of a position in the table of a site is fresh,
// visited or recorded; a record holds what the rest of a walk from its position did.

// The parts of the records that the mutants of a parse break (the engine's Tuning.SkipMut). A parser
// that breaks none, the library's, has skipMut false for each of them.
const (
	skipKeepPos      = 1 + iota // the end: the position stays at the record
	skipNoExpected              // the farthest failure and its expectations
	skipNoExamined              // the end of the examined range
	skipNoFrom                  // the start of the examined range
	skipNeverRecord             // walks stay plain
	skipEarlyStore              // records are stored when their iteration ends and never completed
	skipIgnoreEnv               // the key leaves out the variables
	skipKeepEnv                 // a hit leaves the variables as they are, not as the walk of the record bound them
	skipAnyList                 // a walk that builds values takes the records of value-free walks
	skipKeyEnvObject            // the key holds the environment as an object besides the values of its variables
	skipScanKeys                // a lookup steps through every record of the site before it takes the one of its key
)

// runKeySlots are the slots of the variables that key the records of a run site that is not free of the
// environment: vars, those that its element reads or binds and those that the rules it calls read, and
// bind, those that it binds, which a walk that takes a record sets to the values the walk of the record
// left in them.
type runKeySlots struct {
	vars, bind []int
}

// The states of a slot.
const (
	skipFresh    = iota // no walk visited the position
	skipVisited         // a walk visited it without recording
	skipRecorded        // the record holds the rest of a walk from it
)

// skipRecord is the state of a position and, once recorded, what the rest of a walk from it did:
// where it ended and the values it left in the variables that the element binds (bind, in the order
// of runKeySlots.bind; none for a site not keyed by the variables), its farthest failure, the input it
// examined, the elements it collected (a run site that builds a value) and the errors it recovered.
// list reports that a walk that builds values made the record, so that it holds the elements: the
// value-free twin of a rule walks the same site without them, and a walk that builds values does not
// take a record of such a walk, but records the position again.
type skipRecord struct {
	end, far       int
	exp            []ExpID
	from, examined int
	state          uint8
	list           bool
	// errBytes is what skipBytes counts for the error entries that the record made (skipLink), none
	// for those it shares with the record of the rest of its walk.
	errBytes uint32
	// key holds the values of the variables of the key that the walk from the position started with.
	key, bind []any
	vals      recVals
	errs      *recEntry // nil for none, so that a record of no errors stays small
}

// skipKey indexes the records: the position and the hash of the values of the variables of the key
// that the walk from the position started with, zero for a site not keyed by them (skipWalk.key).
// Records whose keys share a hash are told apart by their values (skipSlots.more).
type skipKey struct {
	pos int
	h   uint64
}

// walkKey is the key of a visit: the values of the variables of the key, and their hash.
type walkKey struct {
	vals []any
	h    uint64
}

// skipSlots holds the slots of a site: a bit for each position from base on, set when the slot is not
// fresh, and the records of the recorded slots by position. The bits take an eighth of a byte per
// position between the first and the last position walked, and a mark costs no allocation once they
// cover the position; the records take memory only for the positions recording walks visited. The
// slots of the positions before floor are dropped: a stream parses there no more.
type skipSlots struct {
	base  int
	marks []uint64
	recs  map[skipKey]skipRecord
	// more holds the records whose key shares the hash of the one in recs but not its values.
	more  map[skipKey][]skipRecord
	floor int
	// expBytes counts the expectations of the records stored, the values of their keys and bindings,
	// and the records in more, for the memory limit.
	expBytes uint64
}

// marked reports whether the slot of pos is not fresh. s may be nil, with every slot fresh.
func (s *skipSlots) marked(pos int) bool {
	if s == nil || pos < s.floor {
		return false
	}
	i := pos - s.base
	return i >= 0 && i>>6 < len(s.marks) && s.marks[i>>6]&(1<<(i&63)) != 0
}

// getSkip returns the slot of pos for walks that start with the key k; a fresh slot is the zero
// record. Comparing the values of the key is no event: the walk counts its key (countWalkKey). p may
// be nil for a site not keyed by the variables.
func (p *Parser) getSkip(s *skipSlots, pos int, k walkKey) skipRecord {
	if !s.marked(pos) {
		return skipRecord{}
	}
	sk := skipKey{pos, k.h}
	if p != nil && p.skipMut(skipScanKeys) {
		for range s.recs {
			p.skipStep()
		}
	}
	if r, ok := s.recs[sk]; ok {
		if len(k.vals) == 0 || p.memoKeyEqual(r.key, k.vals) {
			return r
		}
		for _, r := range s.more[sk] {
			if p.memoKeyEqual(r.key, k.vals) {
				return r
			}
		}
	}
	return skipRecord{state: skipVisited}
}

// putSkip sets the slot of pos to r, which is visited or recorded; a record without values leaves one
// with values in place. Room made for bits before base is as large as the bits, so walks that start
// farther and farther back copy them a logarithmic number of times. p may be nil for a site not keyed
// by the variables.
func (p *Parser) putSkip(s *skipSlots, pos int, k walkKey, r skipRecord) {
	p.noteStored(pos)
	switch {
	case len(s.marks) == 0:
		s.base = pos &^ 63
	case pos < s.base:
		base := max(0, min(pos, s.base-64*len(s.marks))) &^ 63
		marks := make([]uint64, (s.base-base)>>6+len(s.marks))
		copy(marks[(s.base-base)>>6:], s.marks)
		s.marks, s.base = marks, base
	}
	i := pos - s.base
	if i>>6 >= len(s.marks) {
		s.marks = append(s.marks, make([]uint64, i>>6+1-len(s.marks))...)
	}
	s.marks[i>>6] |= 1 << (i & 63)
	if r.state == skipRecorded {
		if s.recs == nil {
			s.recs = make(map[skipKey]skipRecord)
		}
		sk := skipKey{pos, k.h}
		r.key = k.vals
		old, ok := s.recs[sk]
		if ok && len(k.vals) > 0 && !p.memoKeyEqual(old.key, k.vals) {
			// Another key with the same hash holds the slot: the record goes with the others.
			p.putMore(s, sk, r)
			return
		}
		if ok && old.list && !r.list {
			// A record with values serves the walks without values too, and holds what this one
			// holds besides them (they rest on the same evaluations), so it stays: a key holds a
			// record without values only until a walk with values records it, and never after.
			return
		}
		if ok {
			s.expBytes -= old.countedBytes()
		}
		s.recs[sk] = r
		s.expBytes += r.countedBytes()
	}
}

// countedBytes is what expBytes counts for r besides the record itself: its expectations and the values
// of its key and bindings.
func (r *skipRecord) countedBytes() uint64 {
	return uint64(len(r.exp))*expIDBytes + uint64(len(r.key)+len(r.bind))*anyBytes
}

// putMore stores r in the records of sk whose key shares the hash of the one in recs, keeping a
// record with values over one without, as putSkip does.
func (p *Parser) putMore(s *skipSlots, sk skipKey, r skipRecord) {
	if s.more == nil {
		s.more = map[skipKey][]skipRecord{}
	}
	rs := s.more[sk]
	for i, old := range rs {
		if p.memoKeyEqual(old.key, r.key) {
			if !old.list || r.list {
				s.expBytes += r.countedBytes() - old.countedBytes()
				rs[i] = r
			}
			return
		}
	}
	if len(rs) == 0 {
		s.expBytes += uint64(unsafe.Sizeof(sk) + unsafe.Sizeof(rs))
	}
	s.more[sk] = append(rs, r)
	s.expBytes += r.countedBytes() + uint64(unsafe.Sizeof(skipRecord{}))
}

// drop drops the slots of the positions before pos: the words of bits wholly before it and every
// record of a position before it, so after a drop all records held are at or after its floor. Its
// cost is that of the records held, which a stream pays once a window, when it prunes its memo.
// expBytes forgets what the removed records held.
func (s *skipSlots) drop(pos int) {
	if pos <= s.floor {
		return
	}
	s.floor = pos
	if k := min((pos-s.base)>>6, len(s.marks)); k > 0 {
		s.marks, s.base = s.marks[k:], s.base+64*k
	}
	for k, r := range s.recs {
		if k.pos < pos {
			s.expBytes -= r.countedBytes()
			delete(s.recs, k)
		}
	}
	for k, rs := range s.more {
		if k.pos < pos {
			s.expBytes -= uint64(unsafe.Sizeof(k) + unsafe.Sizeof(rs))
			for i := range rs {
				s.expBytes -= rs[i].countedBytes() + uint64(unsafe.Sizeof(skipRecord{}))
			}
			delete(s.more, k)
		}
	}
}
