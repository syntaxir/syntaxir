package libgo

import (
	"encoding/binary"

	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// The tables of the dependency memo of left recursion: the dependencies that evaluations in a
// context record, the context tables that keep their results by the values of those dependencies,
// and the lookup of a result there. The engine and the generated parsers share them;
// context_memo.go in each package describes the memo and holds the rest of it. Each step of their
// work calls spendMemo, which each package provides.

// failSeed is the identity of the failure that every growth starts from.
const failSeed uint64 = 1

// depKind is the kind of a dependency.
type depKind uint8

const (
	depSeed   depKind = iota // read the seed of gamma[idx], with identity val
	depObs                   // the lookahead site idx had the outcome val in the context of depth d
	depShadow                // a seed read by a lookahead that an observation at depth d stands for
)

// dep is a dependency recorded in a frame. gamma[:d] is the context of an observation; a frame
// whose context is shorter cannot check it and takes the seeds that its shadows name instead.
type dep struct {
	kind depKind
	idx  int32
	d    int32
	val  uint64
}

// depFrame records the dependencies of an evaluation at pos in the context gamma[:depth], whose
// growths at pos are gamma[gp:depth]. Its dependencies are deps[base:] while it is the innermost
// frame.
type depFrame struct {
	pos, gp, depth, base int
}

// The kinds of memoized evaluations in a context table.
const (
	ctxCall = iota
	ctxStep
	ctxLook
)

// ctxSet is the dependencies of one shape that results of a head have: the seeds they read and the
// lookaheads they observed. vals finds a result by the values of those dependencies.
type ctxSet struct {
	seeds []growKey
	obs   []int32
	vals  map[valsKey]*ctxSlot
	// wide holds the results of a set of more than maxCtxDeps dependencies.
	wide map[wideKey]*ctxSlot
}

// maxCtxDeps is the number of dependency values that a valsKey holds; a set with more dependencies
// keys its results by a wideKey.
const maxCtxDeps = 6

// valsKey is the values of the dependencies of a result, in the order of its set.
type valsKey struct {
	n uint8
	v [maxCtxDeps]uint64
}

// wideKey is the values of the dependencies of a result of a set of more than maxCtxDeps: the first
// maxCtxDeps in k and the others in rest, eight little-endian bytes each.
type wideKey struct {
	k    valsKey
	rest string
}

// add appends the value v of the next dependency to the key k, or to rest once k is full.
func (k *valsKey) add(v uint64, rest []byte) []byte {
	if k.n < maxCtxDeps {
		k.v[k.n] = v
		k.n++
		return rest
	}
	return binary.LittleEndian.AppendUint64(rest, v)
}

// first returns the first result of the set with the dependency values k and rest.
func (s *ctxSet) first(k valsKey, rest []byte) *ctxSlot {
	if len(rest) == 0 {
		return s.vals[k]
	}
	return s.wide[wideKey{k, string(rest)}]
}

// setFirst makes x the first result of the set with the dependency values k and rest, and returns
// the bytes that a key of rest takes beyond the valsKey that ctxSlotBytes counts.
func (s *ctxSet) setFirst(k valsKey, rest []byte, x *ctxSlot) uint64 {
	if len(rest) == 0 {
		s.vals[k] = x
		return 0
	}
	if s.wide == nil {
		s.wide = map[wideKey]*ctxSlot{}
	}
	s.wide[wideKey{k, string(rest)}] = x
	return wideKeyBytes + uint64(len(rest))
}

// ctxTable is the context table of the growths of one cycle at a position: what the calls and
// growth steps there memoized while such a growth is in progress. It is dropped when the first of
// those growths ends.
type ctxTable struct {
	pos   int
	depth int // the index in gamma of the first of the growths
	heads map[ctxHeadKey]*[]*ctxSet
	jumps map[jumpKey][]jumpRec
	canon map[canonKey]canonRec
	// bytes counts the storage the table has taken, for the memory limit (heldBytes). It goes with
	// the table when the table is dropped.
	bytes uint64
}

// The kinds of keys that intern holds.
const (
	internPresent = iota
	internOutcome
)

// pushFrame starts recording the dependencies of an evaluation at pos, in the context of the
// growths gamma[gp:] there.
func (p *Parser) pushFrame(pos, gp int) {
	p.frames = append(p.frames, depFrame{pos: pos, gp: gp, depth: len(p.gamma), base: len(p.deps)})
}

// popFrame ends the innermost frame and returns its dependencies, valid until the next push.
func (p *Parser) popFrame() (depFrame, []dep) {
	f := p.frames[len(p.frames)-1]
	p.frames = p.frames[:len(p.frames)-1]
	ds := p.deps[f.base:]
	p.deps = p.deps[:f.base]
	return f, ds
}

// addDep adds d to the innermost frame unless it holds it already.
func (p *Parser) addDep(d dep) {
	f := &p.frames[len(p.frames)-1]
	for i := f.base; i < len(p.deps); i++ {
		x := &p.deps[i]
		if x.kind == d.kind && x.idx == d.idx && (d.kind != depObs || x.d == d.d) {
			return
		}
	}
	p.spendMemo()
	p.deps = append(p.deps, d)
}

// mergeDeps passes the dependencies of a finished evaluation to the innermost frame: the seeds of
// its context, and the observations made in the same context. A shadow becomes a seed read when
// the frame cannot check the observation it belongs to.
func (p *Parser) mergeDeps(ds []dep) {
	if len(p.frames) == 0 {
		return
	}
	depth := int32(p.frames[len(p.frames)-1].depth)
	for _, d := range ds {
		switch d.kind {
		case depSeed:
			if d.idx < depth {
				p.addDep(d)
			}
		case depObs:
			if d.d == depth {
				p.addDep(d)
			}
		case depShadow:
			if d.d == depth {
				p.addDep(d)
			} else if d.idx < depth {
				p.addDep(dep{kind: depSeed, idx: d.idx, val: d.val})
			}
		}
	}
}

// table returns the context table of the growths gamma[gp:] at pos, creating it if create is set,
// or nil.
func (p *Parser) table(pos, gp int, create bool) *ctxTable {
	if n := len(p.ctxs); n > 0 && p.ctxs[n-1].pos == pos && p.ctxs[n-1].depth == gp {
		return p.ctxs[n-1]
	}
	if !create {
		return nil
	}
	t := &ctxTable{pos: pos, depth: gp, heads: map[ctxHeadKey]*[]*ctxSet{}, bytes: ctxTableBytes}
	p.ctxs = append(p.ctxs, t)
	return t
}

// ctxFind looks up a result of head h in the context table of the growths gamma[gp:] at pos for
// the current context.
func (p *Parser) ctxFind(pos, gp int, h ctxHeadKey, env []any) *ctxSlot {
	s, _ := p.ctxFindSet(pos, gp, h, env)
	return s
}

func equalGrowKeys(a, b []growKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalSites(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// reusable reports whether a memoized result evaluated inside a lookahead (silent) can be used by
// a call in the current kind of context, as for memo entries: a result computed outside a
// lookahead answers every call, and one computed inside a lookahead, which recorded no
// expectations, only calls inside one. The errors that a result recovered differ between the two
// kinds of context only in messages and positions, and a lookahead discards them at its end.
func (p *Parser) reusable(silent bool) bool {
	return !silent || p.Silent > 0
}

// sdep is a stored dependency: a seed (or shadow) by the leader's key, or an observation by site.
type sdep struct {
	kind depKind
	rule int32
	min  int32
	ms   missingBit
	site int32
	val  uint64
}

type ctxHeadKey struct {
	kind    uint8
	id, min int32
	ms      missingBit
	present presentSet
}

type growKey struct {
	ms        missingBit
	rule, min int32
}

type jumpKey struct {
	g       growKey
	present presentSet
	seed    uint64
}

// gammaAt returns the index of the first growth of the cycle at pos that the innermost growths
// there are (len(gamma) if the innermost growth is not one).
//
// Only these growths matter to a call of a rule of the cycle at pos. A call at pos inside the growth
// of a leader at pos is reached from the leader through calls at pos, so the leader reaches the
// rule and a seed the call reads is of a leader that the rule reaches: the leaders whose seeds the
// call can read are on the rule's cycle. Growths at pos are nested in an order of their cycles,
// since a growth inside the growth of a leader of another cycle at pos cannot reach that leader:
// the growths of the rule's cycle at pos are consecutive, and if the innermost growth at pos is of
// another cycle, there is none.
func (p *Parser) gammaAt(pos int, cycle int32) int {
	i := len(p.gamma)
	for i > 0 && p.gamma[i-1].at() == pos && p.gamma[i-1].cycle == cycle {
		i--
	}
	return i
}

// gammaFind returns the index of the growth of the leader with key k, from gp on, or -1. A leader
// that grows is read whatever the values of the variables (they are not part of the key).
func (p *Parser) gammaFind(gp int, k growKey) int {
	for i := len(p.gamma) - 1; i >= gp; i-- {
		if p.gamma[i].gkey() == k {
			return i
		}
	}
	return -1
}

// presentWide tags the present set of a context with a leader that has no bit, one of index
// maxLeaderBits or more in its cycle: the identity that intern gives the set, which no set of bits
// has, as their bits are below maxLeaderBits. A memo entry holds a set of bits, so a result in such a
// context goes to the table of the context even if it read no seed (ctxPop), and no memo entry
// answers a call there.
const presentWide presentSet = 1 << maxLeaderBits

// leaderBit returns the bit of the growth's leader in present sets, or 0 if it has none: the bit
// 1<<idx of the first maxLeaderBits leaders of its cycle (Rule.leaderIndex in the library,
// Rule.LeaderIndex in the engine). This is the one place that derives a leader's bit.
func (g *growRec) leaderBit() presentSet {
	if g.idx >= maxLeaderBits {
		return 0
	}
	return 1 << g.idx
}

// present returns the present set of the context gamma[gp:]: the bits of its leaders, or the
// identity of the set if a leader has no bit (presentWide).
func (p *Parser) present(gp int) presentSet {
	var m presentSet
	for i := gp; i < len(p.gamma); i++ {
		b := p.gamma[i].leaderBit()
		if b == 0 {
			return p.presentWide(gp)
		}
		m |= b
	}
	return m
}

// dropTable drops the context table of the growth at gamma[i] if that growth is the first at its
// position.
func (p *Parser) dropTable(i int) {
	if n := len(p.ctxs); n > 0 && p.ctxs[n-1].pos == p.gamma[i].at() && p.ctxs[n-1].depth == i {
		p.ctxs = p.ctxs[:n-1]
	}
}

// The checks of the dependency memo that the engine's tests break one at a time (Parser.ctxMut, and
// Tuning.MemoMut in the engine). The Go library breaks none.
const (
	ctxMutIgnoreSeeds      = 1 + iota // a reuse does not compare the identities of the seeds read
	ctxMutIgnoreObs                   // a reuse does not check observations
	ctxMutIgnorePresent               // a result in the memo table is reused whatever its present set
	ctxMutNoCanonical                 // equal step results get the same identity without comparing them
	ctxMutKeepSeedResults             // a Document keeps the results that read a seed for later parses
	ctxMutJumpAnySeed                 // a jump uses observations whose outcomes read the growing seed
	ctxMutStaleReplay                 // a reuse replays the seeds its observations read where they were made
	ctxMutNoReplayObs                 // a reuse passes on only the seeds its observations read where they were made
	ctxMutCapDeps                     // a result with more than maxCtxDeps dependencies is not stored
	ctxMutNarrowPresent               // a context whose present set needs more than a word records nothing
	ctxMutNarrowOutcome               // a lookahead whose examined range needs more than a word is not stored
	ctxMutStrictMissing               // a parse with missing-token insertion keeps no result in a context
	ctxMutStoreInserts                // rules that insert tokens keep their results in contexts too
	ctxMutConstWide                   // all present sets of leaders without bits have one identity
	ctxMutNoReplayLookup              // a reused result of a lookup passes no dependencies to the enclosing call
	ctxMutNoStepMemo                  // a growth in a context takes no memoized step or jump and evaluates the body
	ctxMutKeepInPlace                 // the keep of a leader's result changes the result that grow.end returned
	ctxMutNoJumpReplayDeps            // a jump passes on none of the dependencies of its steps
	ctxMutNoJumpMergeExp              // a jump passes on none of the expectations of its steps
	ctxMutNoStepReplayDeps            // a memoized step passes on none of its dependencies
	ctxMutNoStepMergeExp              // a memoized step passes on none of its expectations
	ctxMutNoJumpSeedRead              // a jump does not count as a read of the seed of the growth
	ctxMutNoStepSeedRead              // a memoized step does not count as a read of the seed of the growth
)

// The values of an observation other than outcomes (obsOutcome).
const (
	obsNone    uint64 = 1 << 63       // no outcome is memoized
	obsIgnored uint64 = 1<<63 | 1<<62 // every outcome under ctxMutIgnoreObs
	// obsWide tags the identity that intern gives an outcome whose examined range does not fit in
	// the bits of a word.
	obsWide uint64 = 1<<63 | 1<<60
)

// presentWide returns the identity of the present set of the context gamma[gp:]: its words of bits,
// by the leader index of its rules, interned.
func (p *Parser) presentWide(gp int) presentSet {
	var w []uint64
	for i := gp; i < len(p.gamma); i++ {
		p.spendMemo()
		k := int(p.gamma[i].idx)
		for len(w) <= k/64 {
			w = append(w, 0)
		}
		w[k/64] |= 1 << (k % 64)
	}
	if p.ctxMut(ctxMutConstWide) {
		return presentWide | 1
	}
	key := make([]byte, 1, 1+8*len(w))
	key[0] = internPresent
	for _, x := range w {
		key = binary.LittleEndian.AppendUint64(key, x)
	}
	return presentSet(p.intern(key, uint64(presentWide)))
}

// intern returns the identity of key: tag with a number from 1 that no other key has while a growth
// is in progress. The identities are those of present sets and outcomes too large for a word
// (presentWide, obsWide); they appear only in the context tables and dependencies of growths in
// progress, so the parser forgets them and numbers again from 1 when the last growth ends
// (the end of growth). The numbers stay below 2^31, as more keys would not fit in memory, so a tag
// above them leaves them intact in a present set of either width. Like the chunks of the allocators,
// an entry is charged to the memory used when it is made (useMem) and is not given back when it is
// forgotten: the charge is bounded by the compute units of the calls whose growths make the keys.
func (p *Parser) intern(key []byte, tag uint64) uint64 {
	if v, ok := p.interned[string(key)]; ok {
		return v
	}
	if p.interned == nil {
		p.interned = map[string]uint64{}
	}
	p.spendMemo()
	p.internSeq++
	v := tag | p.internSeq
	p.interned[string(key)] = v
	p.useMem(internBytes + uint64(len(key)))
	return v
}

// stored converts frame dependencies to stored ones and reports whether they include a seed read
// or an observation.
func (p *Parser) stored(ds []dep) ([]sdep, bool) {
	if len(ds) == 0 {
		return nil, false
	}
	out := make([]sdep, 0, len(ds))
	ctx := false
	for _, d := range ds {
		p.spendMemo()
		switch d.kind {
		case depSeed, depShadow:
			k := p.gamma[d.idx].gkey()
			out = append(out, sdep{kind: d.kind, rule: k.rule, min: k.min, ms: k.ms, val: d.val})
			ctx = ctx || d.kind == depSeed
		case depObs:
			out = append(out, sdep{kind: depObs, site: d.idx, val: d.val})
			ctx = true
		}
	}
	return out, ctx
}

// replayDeps passes the stored dependencies of a reused result at pos, whose present set is
// present, to the innermost frame, as if the evaluation that produced them had run in the current
// context gamma[gp:]: the seeds it read with their current identities, and each observation with
// the seeds that its outcome read in the current context (replayObs) as well as those it read where
// it was made. A frame that cannot check an observation depends on those seeds instead.
func (p *Parser) replayDeps(pos, gp int, present presentSet, ds []sdep) {
	if len(p.frames) == 0 || len(ds) == 0 {
		return
	}
	depth := int32(len(p.gamma))
	buf := make([]dep, 0, len(ds))
	for _, d := range ds {
		switch d.kind {
		case depSeed, depShadow:
			i := p.gammaFind(gp, growKey{rule: d.rule, min: d.min, ms: d.ms})
			if i < 0 {
				continue
			}
			v := p.gamma[i].id
			if p.ctxMut(ctxMutStaleReplay) {
				v = d.val
			}
			buf = append(buf, dep{kind: d.kind, idx: int32(i), d: depth, val: v})
		case depObs:
			buf = append(buf, dep{kind: depObs, idx: d.site, d: depth, val: d.val})
			if !p.ctxMut(ctxMutStaleReplay) && !p.ctxMut(ctxMutNoReplayObs) {
				buf = p.replayObs(buf, pos, gp, present, d.site, depth)
			}
		}
	}
	p.mergeDeps(buf)
}

// replayObs appends to buf the shadows of the seeds that the outcome memoized for the lookahead
// site at pos in the current context read, and the observations it depends on with their shadows.
func (p *Parser) replayObs(buf []dep, pos, gp int, present presentSet, site, depth int32) []dep {
	for _, s := range p.obsGuard {
		if s == site {
			return buf
		}
	}
	p.obsGuard = append(p.obsGuard, site)
	defer func() { p.obsGuard = p.obsGuard[:len(p.obsGuard)-1] }()
	s, set := p.ctxFindSet(pos, gp, ctxHeadKey{kind: ctxLook, id: site, present: present}, nil)
	if s == nil {
		return buf
	}
	for _, k := range set.seeds {
		if i := p.gammaFind(gp, growKey{rule: k.rule, min: k.min, ms: k.ms}); i >= 0 {
			buf = append(buf, dep{kind: depShadow, idx: int32(i), d: depth, val: p.gamma[i].id})
		}
	}
	for _, o := range set.obs {
		if v := p.obsValue(pos, gp, present, o); v != obsNone {
			buf = append(buf, dep{kind: depObs, idx: o, d: depth, val: v})
			buf = p.replayObs(buf, pos, gp, present, o, depth)
		}
	}
	return buf
}

// obsOutcome encodes the outcome of a lookahead at pos, whose expression matched if ok and examined
// [from, examined): equal values are equal outcomes with equal examined ranges. An outcome whose
// range reaches 2^30 units or more from pos gets an interned identity (obsWide). ok2 is false only
// under the engine's mutant that narrows outcomes (ctxMutNarrowOutcome), for such an outcome.
func (p *Parser) obsOutcome(ok bool, pos, examined, from int) (v uint64, ok2 bool) {
	hi, lo := examined-pos, pos-from
	if hi < 0 || lo < 0 || hi >= 1<<30 || lo >= 1<<30 {
		if p.ctxMut(ctxMutNarrowOutcome) {
			return obsWide, false
		}
		key := make([]byte, 2, 18)
		key[0] = internOutcome
		if ok {
			key[1] = 1
		}
		key = binary.LittleEndian.AppendUint64(key, uint64(hi))
		key = binary.LittleEndian.AppendUint64(key, uint64(lo))
		return p.intern(key, obsWide), true
	}
	v = uint64(hi)<<1 | uint64(lo)<<31
	if ok {
		v |= 1
	}
	return v, true
}

// obsValue returns the outcome memoized for the lookahead site at pos in the current context
// (obsOutcome), or obsNone.
func (p *Parser) obsValue(pos, gp int, present presentSet, site int32) uint64 {
	if p.ctxMut(ctxMutIgnoreObs) {
		return obsIgnored
	}
	for _, s := range p.obsGuard {
		if s == site {
			return obsNone
		}
	}
	p.obsGuard = append(p.obsGuard, site)
	defer func() { p.obsGuard = p.obsGuard[:len(p.obsGuard)-1] }()
	s := p.ctxFind(pos, gp, ctxHeadKey{kind: ctxLook, id: site, present: present}, nil)
	if s == nil {
		return obsNone
	}
	return s.id
}

// obsFree returns the outcome memoized for the lookahead site at pos in the current context, or
// obsNone, and reports whether it was obtained without reading the seed of the growth g, directly
// or through the observations it depends on. Such an outcome is the outcome of the lookahead for
// every seed of g: an evaluation follows the same path, and reads the same seeds, as long as the
// seeds it reads are equal.
func (p *Parser) obsFree(pos, gp int, present presentSet, site int32, g growKey) (uint64, bool) {
	for _, s := range p.obsGuard {
		if s == site {
			return obsNone, false
		}
	}
	p.obsGuard = append(p.obsGuard, site)
	defer func() { p.obsGuard = p.obsGuard[:len(p.obsGuard)-1] }()
	s, set := p.ctxFindSet(pos, gp, ctxHeadKey{kind: ctxLook, id: site, present: present}, nil)
	if s == nil {
		return obsNone, false
	}
	for _, k := range set.seeds {
		if k == g {
			return s.id, false
		}
	}
	for _, o := range set.obs {
		if _, free := p.obsFree(pos, gp, present, o, g); !free {
			return s.id, false
		}
	}
	return s.id, true
}

// ctxFindSet is ctxFind that also returns the set of the result, which lists what it depends on.
func (p *Parser) ctxFindSet(pos, gp int, h ctxHeadKey, env []any) (*ctxSlot, *ctxSet) {
	t := p.table(pos, gp, false)
	if t == nil {
		return nil, nil
	}
	p.spendMemo()
	sets := t.heads[h]
	if sets == nil {
		return nil, nil
	}
next:
	for _, s := range *sets {
		p.spendMemo()
		var k valsKey
		var rest []byte
		for _, g := range s.seeds {
			i := p.gammaFind(gp, g)
			if i < 0 {
				continue next
			}
			v := p.gamma[i].id
			if p.ctxMut(ctxMutIgnoreSeeds) {
				v = 0
			}
			rest = k.add(v, rest)
		}
		for _, o := range s.obs {
			v := p.obsValue(pos, gp, h.present, o)
			if v == obsNone {
				continue next
			}
			rest = k.add(v, rest)
		}
		for x := s.first(k, rest); x != nil; x = x.next {
			p.spendMemo()
			if env == nil && x.env == nil || p.memoKeyEqual(x.env, env) {
				return x, s
			}
		}
	}
	return nil, nil
}

// ctxStore stores slot as the result of head h in the context of the growths gamma[gp:] at pos,
// with the stored dependencies ds.
func (p *Parser) ctxStore(pos, gp int, h ctxHeadKey, ds []sdep, slot *ctxSlot) {
	var seeds []growKey
	var obs []int32
	var k valsKey
	var rest []byte
	for _, d := range ds {
		if d.kind == depShadow {
			continue
		}
		if p.ctxMut(ctxMutCapDeps) && len(seeds)+len(obs) == maxCtxDeps {
			return
		}
		if d.kind == depSeed {
			seeds = append(seeds, growKey{rule: d.rule, min: d.min, ms: d.ms})
		} else {
			obs = append(obs, d.site)
		}
	}
	// The values follow the order of the set: seeds first, then observations.
	for _, d := range ds {
		if d.kind == depSeed {
			v := d.val
			if p.ctxMut(ctxMutIgnoreSeeds) {
				v = 0
			}
			rest = k.add(v, rest)
		}
	}
	for _, d := range ds {
		if d.kind == depObs {
			v := d.val
			if p.ctxMut(ctxMutIgnoreObs) {
				v = obsIgnored
			}
			rest = k.add(v, rest)
		}
	}
	t := p.table(pos, gp, true)
	p.spendMemo()
	sets := t.heads[h]
	if sets == nil {
		sets = new([]*ctxSet)
		t.heads[h] = sets
		t.bytes += ctxHeadBytes
	}
	var set *ctxSet
	for _, s := range *sets {
		p.spendMemo()
		if equalGrowKeys(s.seeds, seeds) && equalSites(s.obs, obs) {
			set = s
			break
		}
	}
	if set == nil {
		set = &ctxSet{seeds: seeds, obs: obs, vals: map[valsKey]*ctxSlot{}}
		*sets = append(*sets, set)
		t.bytes += ctxSetBytes + uint64(len(seeds))*growKeyBytes + uint64(len(obs))*4
	}
	slot.deps = ds
	t.bytes += ctxSlotBytes + uint64(len(ds))*sdepBytes + uint64(len(slot.env))*anyBytes + uint64(len(slot.exp))*expIDBytes
	link := set.first(k, rest)
	for x := link; x != nil; x = x.next {
		p.spendMemo()
		if x.env == nil && slot.env == nil || p.memoKeyEqual(x.env, slot.env) {
			// Replace the result in place: the other kind of context (inside or outside a lookahead)
			// evaluated it again.
			nx := x.next
			*x = *slot
			x.next = nx
			return
		}
	}
	slot.next = link
	if added := set.setFirst(k, rest, slot); link == nil {
		t.bytes += added
	}
}

// ctxSlot is a result in a context table: the result, and for a growth step the identity of its
// result and what the step itself recorded (its result is a seed shared by equal results).
type ctxSlot struct {
	e              *ctxRes
	id             uint64 // the identity of a step result; the outcome of a lookahead (obsOutcome)
	far            int
	exp            []ExpID
	examined, from int
	silent         bool
	env            []any
	deps           []sdep
	next           *ctxSlot
}

// jumpRec takes a growth from a seed to the seed at the end of a run of steps that read no other
// seed, with what those steps recorded.
type jumpRec struct {
	end            *ctxRes
	id             uint64
	far            int
	exp            []ExpID
	examined, from int
	silent         bool
	deps           []sdep
}

// canonKey keys the canonical results of growth steps (canonicalSeed): the leader and the end of
// a successful result.
type canonKey struct {
	g   growKey
	end int
}

// canonRec is a canonical result of a growth step and its identity as a seed.
type canonRec struct {
	e  *ctxRes
	id uint64
}

// runStep is a step of a run of a growth: the seed it started from and what it recorded.
type runStep struct {
	seed           uint64
	far            int
	exp            []ExpID
	examined, from int
	silent         bool
	deps           []sdep
}

// nextSeedID returns a new seed identity.
func (p *Parser) nextSeedID() uint64 {
	p.seedSeq++
	return p.seedSeq + failSeed
}

// canonicalSeed returns the identity of the result e of a growth step of the leader g at pos, in the
// context of the growths gamma[gp:] there: the same as an earlier equal result's, whose result it
// then returns instead of e. Only a successful result without errors is shared; results are equal
// when they examined the same range (where the parse compares ranges, rangeKept) and sameResult shows
// them equal.
func (p *Parser) canonicalSeed(pos, gp int, g growKey, e *ctxRes) (*ctxRes, uint64) {
	id := p.nextSeedID()
	if !e.succeeded() || !e.errFree() {
		return e, id
	}
	t := p.table(pos, gp, true)
	if t.canon == nil {
		t.canon = map[canonKey]canonRec{}
	}
	ck := canonKey{g: g, end: e.resultEnd()}
	p.spendMemo()
	if c, ok := t.canon[ck]; ok {
		if p.ctxMut(ctxMutNoCanonical) {
			return c.e, c.id
		}
		budget := 64
		if (!p.rangeKept() || c.e.examinedEnd() == e.examinedEnd() && c.e.examinedFrom() == e.examinedFrom()) && c.e.errFree() && p.sameResult(c.e, e, &budget) {
			return c.e, c.id
		}
	}
	if _, ok := t.canon[ck]; !ok {
		t.bytes += canonBytes
	}
	t.canon[ck] = canonRec{e: e, id: id}
	return e, id
}

// nodesEqual reports whether two trees are equal, comparing at most *budget pairs of distinct nodes
// (false beyond: two results that are not shown equal get different identities, which costs only
// reuse). Trees are equal by their values: kinds, positions, text, the terminal and labeled flags,
// children and fields; a box node equals only itself. Positions compare only on the same basis
// (samePositionBasis). Fresh, which says who may modify a node, is no part of its value.
func (p *Parser) nodesEqual(a, b *sxirvalue.Node, budget *int) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if *budget--; *budget < 0 {
		return false
	}
	p.spendMemo()
	da, db := D(a), D(b)
	if da.Kind != db.Kind || da.Start != db.Start || da.End != db.End || da.Text != db.Text ||
		da.Terminal != db.Terminal || da.Labeled != db.Labeled || da.Box != nil || db.Box != nil ||
		len(da.Children) != len(db.Children) || len(da.Fields) != len(db.Fields) || !p.samePositionBasis(a, b) {
		return false
	}
	for i := range da.Children {
		if !p.nodesEqual(da.Children[i], db.Children[i], budget) {
			return false
		}
	}
	for i := range da.Fields {
		fa, fb := &da.Fields[i], &db.Fields[i]
		if fa.Name != fb.Name {
			return false
		}
		na, aok := fa.Value.(*sxirvalue.Node)
		nb, bok := fb.Value.(*sxirvalue.Node)
		switch {
		case aok && bok:
			if !p.nodesEqual(na, nb, budget) {
				return false
			}
		case aok || bok:
			return false
		default:
			switch fa.Value.(type) {
			case nil, int, string, bool:
			default:
				return false
			}
			if fa.Value != fb.Value {
				return false
			}
		}
	}
	return true
}

// lookRec is a lookahead in progress: what its end restores (lookMark), its site (-1 for one that
// records nothing), and the examined range from before it.
type lookRec struct {
	mark           lookMark // first, so that the library's empty mark adds no padding
	site           int32
	examined, from int
}

// noteSeedRead records that the evaluation read the seed of gamma[i].
func (p *Parser) noteSeedRead(i int) {
	p.seedRead(i)
	if len(p.frames) == 0 || i >= p.frames[len(p.frames)-1].depth {
		return
	}
	p.addDep(dep{kind: depSeed, idx: int32(i), val: p.gamma[i].id})
}

// lookBegin starts a lookahead of the given site. In a context at the current position, the
// lookahead records what it reads, so that a result that depends on it records its outcome instead
// of the seeds it read (lookEnd). A site of -1 is a lookahead that can leave captures or bindings in
// effect after it (check.LookKeepsState): what it reads stays a dependency of the result around it.
func (p *Parser) lookBegin(site int) {
	if n := len(p.frames); n > 0 && site >= 0 {
		f := &p.frames[n-1]
		if g := len(p.gamma); f.pos == p.Pos && f.gp < g && p.gamma[g-1].at() == p.Pos && p.gamma[g-1].cycle == p.gamma[f.gp].cycle {
			// The examined range of the lookahead is part of its observation.
			examined, from := p.examRange()
			p.looks = append(p.looks, lookRec{mark: p.lookMark(), site: int32(site), examined: examined, from: from})
			p.setExamRange(p.Pos, p.Pos)
			p.pushFrame(p.Pos, f.gp)
			return
		}
	}
	p.looks = append(p.looks, lookRec{mark: p.lookMark(), site: -1})
}

// lookEnd ends the innermost lookahead, whose expression matched if ok. A lookahead that read a seed
// records its outcome as an observation of its site, keeps the outcome in the table of its context,
// and turns the seeds it read into shadows.
func (p *Parser) lookEnd(ok bool) {
	l := p.looks[len(p.looks)-1]
	p.looks = p.looks[:len(p.looks)-1]
	p.lookRestore(l.mark)
	site := l.site
	if site < 0 {
		return
	}
	f, ds := p.popFrame()
	examined, from := p.examRange()
	p.setExamRange(max(l.examined, examined), min(l.from, from))
	read := false
	for _, d := range ds {
		if d.kind == depSeed {
			read = true
			break
		}
	}
	if !read {
		p.mergeDeps(ds)
		return
	}
	if !p.rangeKept() {
		examined, from = f.pos, f.pos
	}
	val, keep := p.obsOutcome(ok, f.pos, examined, from)
	present := p.present(f.gp)
	sd, _ := p.stored(ds)
	if keep && p.recordsIn(present) {
		p.ctxStore(f.pos, f.gp, ctxHeadKey{kind: ctxLook, id: site, present: present}, sd, &ctxSlot{id: val})
	}
	out := make([]dep, 0, len(ds)+1)
	out = append(out, dep{kind: depObs, idx: site, d: int32(f.depth), val: val})
	for _, d := range ds {
		if d.kind == depSeed {
			out = append(out, dep{kind: depShadow, idx: d.idx, d: int32(f.depth), val: d.val})
		} else {
			out = append(out, d)
		}
	}
	p.mergeDeps(out)
}
