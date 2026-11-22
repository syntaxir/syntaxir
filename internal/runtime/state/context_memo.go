package state

import (
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// The dependency memo of left recursion (see "Evaluations of Rules" in spec/runtime/parsing.md
// and spec/proofs/memoization.md).
//
// The result of a call of rule A at position p is a function of the growths of A's cycle in
// progress at p, the context: the leaders that grow there and the seeds they have reached (gamma,
// gammaAt). A leader called at p while it grows there reads its seed; a leader that does not grow
// there grows from failure, with the seeds of the context visible. Only growths at p matter: a call
// at p evaluates nothing before p, and a growth at a later position ends before the parse returns
// to p. Nor do growths of other cycles: the call reads none of their seeds.
//
// A rule that is on no left-recursion cycle never reads a seed of the context of its call: a
// leader that grows at p reaches the call there through left calls, so a call that read the
// leader's seed would put the rule on the leader's cycle. Its results are memoized as without
// left recursion. The result of a rule of a cycle called in a context records what it depended on:
//
//   - the seeds it read, each by its identity: a number that the growth step that produced the seed
//     gives it, the same for equal results of the same leader at p (canonicalSeed);
//   - for each lookahead at p whose evaluation read a seed, its outcome (an observation), in place
//     of those seeds;
//   - which leaders were growing at p (the present set): a leader that was not growing may have
//     grown from failure within the call, and one that was growing and was not read did not matter.
//
// A result that read no seed and made no observation is stored in the memo table with its present
// set (MemoEntry.Present) and is reused in any context whose leaders it includes. Any other result
// is stored in the table of the context (ctxTable), which exists while its first growth lasts,
// and is reused in a context with the same present set where the seeds it read have the same
// identities and the lookaheads it observed have the same outcomes there. An observation is
// checked against the outcome memoized for the lookahead in the current context; when there is
// none, the check fails and the call is evaluated again, which memoizes the lookahead.
//
// A growth that starts in a context memoizes its steps the same way, with its own seed among the
// seeds a step reads, and records runs of steps that read no seed but their own (jumps): from each
// seed of such a run, the seed at its end, with the observations, the expectations and the
// examined input of the steps from there. Steps and jumps make a growth that is evaluated again in
// another context take only the steps that read the seeds that changed.

// The checks of the dependency memo that Tuning.MemoMut breaks (ctxMut).
const (
	CtxIgnoreSeeds      = ctxMutIgnoreSeeds
	CtxIgnoreObs        = ctxMutIgnoreObs
	CtxIgnorePresent    = ctxMutIgnorePresent
	CtxNoCanonical      = ctxMutNoCanonical
	CtxKeepSeedResults  = ctxMutKeepSeedResults
	CtxJumpAnySeed      = ctxMutJumpAnySeed
	CtxStaleReplay      = ctxMutStaleReplay
	CtxNoReplayObs      = ctxMutNoReplayObs
	CtxCapDeps          = ctxMutCapDeps
	CtxNarrowPresent    = ctxMutNarrowPresent
	CtxNarrowOutcome    = ctxMutNarrowOutcome
	CtxStrictMissing    = ctxMutStrictMissing
	CtxStoreInserts     = ctxMutStoreInserts
	CtxConstWide        = ctxMutConstWide
	CtxNoReplayLookup   = ctxMutNoReplayLookup
	CtxNoStepMemo       = ctxMutNoStepMemo
	CtxKeepInPlace      = ctxMutKeepInPlace
	CtxNoJumpReplayDeps = ctxMutNoJumpReplayDeps
	CtxNoJumpMergeExp   = ctxMutNoJumpMergeExp
	CtxNoStepReplayDeps = ctxMutNoStepReplayDeps
	CtxNoStepMergeExp   = ctxMutNoStepMergeExp
	CtxNoJumpSeedRead   = ctxMutNoJumpSeedRead
	CtxNoStepSeedRead   = ctxMutNoStepSeedRead
)

// growRec is a growth in progress: the leader's key, its current seed and the seed's identity.
type growRec struct {
	key   MemoKey
	seed  *MemoEntry
	id    uint64
	bit   uint64 // Rule.LeaderBit
	idx   int32  // Rule.LeaderIndex
	cycle int32  // Rule.Cycle
}

// ctxSlot is a result in a context table: the entry, and for a growth step the identity of its
// result and what the step itself recorded (its result entry is a seed shared by equal results).
type ctxSlot struct {
	e              *MemoEntry
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
	end            *MemoEntry
	id             uint64
	far            int
	exp            []ExpID
	examined, from int
	silent         bool
	deps           []sdep
}

type canonKey struct {
	g   growKey
	end int
	ok  bool
}

type canonRec struct {
	e  *MemoEntry
	id uint64
}

// keptSeedResult is a result that read a seed, kept for CtxKeepSeedResults.
type keptSeedResult struct {
	key MemoKey
	e   *MemoEntry
}

// KeepSeedResults stores in t, under CtxKeepSeedResults, the results of the parse p that read a
// seed, as if they had not: the negative control of the Document checks, which must detect a later
// parse that reuses them.
func KeepSeedResults(t *MemoTable, p *Parser) {
	if p.tune.MemoMut != CtxKeepSeedResults {
		return
	}
	for _, k := range p.keptSeedResults {
		k.e.Present = ^uint64(0)
		t.Put(k.key, k.e)
	}
}

// strictCtx reports that results in a context are not memoized in this parse.
func (p *Parser) strictCtx() bool {
	return p.ContextMemoOff || p.tune.MemoMut == CtxStrictMissing && p.Missing != nil
}

// inserts reports that the results of r depend on the insertions before them in this parse: r
// contains #missing, directly or through the rules it calls, in a parse with missing-token
// insertion. Such a rule is evaluated again at every call, in a context as outside one, and its
// evaluations there record no dependencies of their own: a call of it is part of an evaluation
// of another such rule or of a lookahead, which takes the seeds it reads. Any other rule inserts
// nothing and reads only seeds of leaders that insert nothing, so its results do not depend on
// the insertions, and are kept in contexts as in a parse without insertion.
func (p *Parser) inserts(r *Rule) bool {
	return p.Missing != nil && r.Missing && p.tune.MemoMut != CtxStoreInserts
}

// ctxMut reports that the check m of the dependency memo is broken in this parse (Tuning.MemoMut).
func (p *Parser) ctxMut(m int) bool { return p.tune.MemoMut == m }

// useMem charges n bytes to the memory used (MemUsed).
func (p *Parser) useMem(n uint64) { p.MemUsed += n }

// gkey returns the key of the growth's leader.
func (g *growRec) gkey() growKey {
	return growKey{rule: int32(g.key.Rule), min: int32(g.key.Min), ms: g.key.MissingSilent}
}

// at returns the position of the growth.
func (g *growRec) at() int { return g.key.Pos }

// leaderBit returns the bit of the growth's leader in present sets, or 0 if it has none
// (Rule.LeaderBit).
func (g *growRec) leaderBit() presentSet { return g.bit }

// presentWide tags the present set of a context with a leader that has no bit (Rule.LeaderBit): the
// identity that intern gives the set, which no word of bits equals. A memo entry holds a word of
// bits (MemoEntry.Present), so a result in such a context goes to the table of the context even if
// it read no seed (ctxPop), and no entry answers a call there (ctxAllows).
const presentWide presentSet = 1 << 63

// presentSet is a present set: the bits of the leaders of a context (Rule.LeaderBit), or presentWide
// and an identity. The Go library's sets have 32 bits.
type presentSet = uint64

// missingBit is the missing-silent bit of a key (MemoKey.MissingSilent): the key of a seed of a
// lookahead where missing-token insertion is off. The Go library inserts no tokens and gives the
// bit a type that takes no space.
type missingBit = bool

// recordsIn reports whether evaluations in a context with the present set present record their
// dependencies, so that their results are kept.
func (p *Parser) recordsIn(present uint64) bool {
	return !p.ContextMemoOff && (p.tune.MemoMut != CtxNarrowPresent || present&presentWide == 0)
}

// pushFrame starts recording the dependencies of an evaluation at pos, in the context of the
// growths gamma[gp:] there.
func (p *Parser) pushFrame(pos, gp int) {
	p.frames = append(p.frames, depFrame{pos: pos, gp: gp, depth: len(p.gamma), base: len(p.deps)})
}

// seedRead counts a read of the seed of a growth, or a reuse of a result that depends on seeds, of
// the growths from gamma[i] on.
func (p *Parser) seedRead(i int) {
	p.SeedReads++
	if p.tune.RunMut == RunOpenGrowthsOnly {
		p.seedReadLog = append(p.seedReadLog, i)
	}
}

// noteSeedRead records that the evaluation read the seed of gamma[i].
func (p *Parser) noteSeedRead(i int) {
	p.seedRead(i)
	if len(p.frames) == 0 || i >= p.frames[len(p.frames)-1].depth {
		return
	}
	p.addDep(dep{kind: depSeed, idx: int32(i), val: p.gamma[i].id})
}

// lookRec is a lookahead in progress: its site (-1 for one that records nothing), the examined
// range from before it, and the recovered errors and inserted tokens at its start, which its end
// discards.
type lookRec struct {
	site         int32
	hw, lw       int
	rec, missing int
}

// LookBegin starts a lookahead of the given site. In a context at the current position, the
// lookahead records what it reads, so that a result that depends on it records its outcome
// instead of the seeds it read (LookEnd). A site of -1 is a lookahead that can leave captures or
// bindings in effect after it (check.LookKeepsState): what it reads stays a dependency of the
// result around it.
func (p *Parser) LookBegin(site int) {
	if n := len(p.frames); n > 0 && site >= 0 {
		f := &p.frames[n-1]
		if g := len(p.gamma); f.pos == p.Pos && f.gp < g && p.gamma[g-1].key.Pos == p.Pos && p.gamma[g-1].cycle == p.gamma[f.gp].cycle {
			// The examined range of the lookahead is part of its observation.
			p.looks = append(p.looks, lookRec{site: int32(site), hw: p.Hw, lw: p.lw, rec: len(p.Recovered), missing: p.MissingLen})
			p.Hw, p.lw = p.Pos, p.Pos
			p.pushFrame(p.Pos, f.gp)
			return
		}
	}
	p.looks = append(p.looks, lookRec{site: -1, rec: len(p.Recovered), missing: p.MissingLen})
}

// LookEnd ends the innermost lookahead, whose expression matched if ok. It discards the errors
// recovered inside the lookahead, whether it matched or not.
func (p *Parser) LookEnd(ok bool) {
	l := p.looks[len(p.looks)-1]
	p.looks = p.looks[:len(p.looks)-1]
	if p.MissingLen > l.missing {
		p.ResetMissing(l.missing)
	}
	p.Recovered = p.Recovered[:l.rec]
	site := l.site
	if site < 0 {
		return
	}
	examined, from := p.Hw, p.lw
	p.Hw, p.lw = max(l.hw, p.Hw), Min2(l.lw, p.lw)
	f, ds := p.popFrame()
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
	if p.ContextMemoUnexamined {
		examined, from = f.pos, f.pos
	}
	val, ok2 := p.obsOutcome(ok, f.pos, examined, from)
	gp := f.gp
	present := p.present(gp)
	ok2 = ok2 && p.recordsIn(present)
	sd, _ := p.stored(ds)
	if ok2 {
		p.ctxStore(f.pos, gp, ctxHeadKey{kind: ctxLook, id: site, present: present}, sd, &ctxSlot{id: val})
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

// canonicalSeed returns the identity of the result e of a growth step of the leader of g, the
// same as an earlier equal result's, whose entry it then returns instead of e.
func (p *Parser) canonicalSeed(pos, gp int, g growKey, e *MemoEntry) (*MemoEntry, uint64) {
	p.seedSeq++
	id := p.seedSeq + failSeed
	if !e.Ok || !e.Errs.None() {
		return e, id
	}
	t := p.table(pos, gp, true)
	if t.canon == nil {
		t.canon = map[canonKey]canonRec{}
	}
	ck := canonKey{g: g, end: e.End, ok: e.Ok}
	p.Spend()
	if c, ok := t.canon[ck]; ok {
		if p.tune.MemoMut == CtxNoCanonical {
			return c.e, c.id
		}
		budget := 64
		if (p.ContextMemoUnexamined || c.e.Examined == e.Examined && c.e.From == e.From) && c.e.Errs.None() && nodesEqual(p, c.e.Node, e.Node, &budget) {
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
// reuse).
func nodesEqual(p *Parser, a, b *value.Node, budget *int) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if *budget--; *budget < 0 {
		return false
	}
	p.Spend()
	da, db := nodes.D(a), nodes.D(b)
	if da.Kind != db.Kind || da.Start != db.Start || da.End != db.End || da.Terminal != db.Terminal ||
		da.Labeled != db.Labeled || da.Mark != db.Mark || da.Box != nil || db.Box != nil ||
		len(da.Children) != len(db.Children) || len(da.Fields) != len(db.Fields) || da.Text != db.Text {
		return false
	}
	for i := range da.Children {
		if !nodesEqual(p, da.Children[i], db.Children[i], budget) {
			return false
		}
	}
	for i := range da.Fields {
		fa, fb := &da.Fields[i], &db.Fields[i]
		if fa.Name != fb.Name {
			return false
		}
		na, aok := fa.Value.(*value.Node)
		nb, bok := fb.Value.(*value.Node)
		switch {
		case aok && bok:
			if !nodesEqual(p, na, nb, budget) {
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
