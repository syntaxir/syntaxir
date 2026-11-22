package libgo

import (
	"reflect"

	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// The dependency memo of left recursion, the same as the engine's (see "Evaluations of Rules" in
// spec/runtime/parsing.md and spec/proofs/memoization.md). Parser and TParser share it: the results
// it keeps hold a node or a typed value (ctxResult).
//
// The result of a call of rule A at position p is a function of the growths of A's cycle in
// progress at p, the context: the leaders that grow there and the seeds they have reached (gamma,
// gammaAt). A leader called at p while it grows there reads its seed; a leader that does not grow
// there grows from failure, with the seeds of the context visible. Only growths at p matter: a call
// at p evaluates nothing before p, and a growth at a later position ends before the parse returns
// to p. Nor do growths of other cycles: the call reads none of their seeds.
//
// A rule that is on no left-recursion cycle never reads a seed of the context of its call, and its
// results are memoized as without left recursion. The result of a rule of a cycle called in a
// context records what it depended on:
//
//   - the seeds it read, each by its identity: a number that the growth step that produced the seed
//     gives it, the same for equal results of the same leader at p (canonicalSeed);
//   - for each lookahead at p whose evaluation read a seed, its outcome (an observation), in place
//     of those seeds;
//   - which leaders were growing at p (the present set).
//
// A result that read no seed and made no observation is stored in the memo with its present set
// (memoEntry.present) and is reused in any context whose leaders it includes. Any other result is
// kept in the table of the context (ctxTable), which exists while its first growth lasts, and is
// reused in a context with the same present set where the seeds it read have the same identities
// and the lookaheads it observed have the same outcomes there. An observation is checked against
// the outcome memoized for the lookahead in the current context; when there is none, the check
// fails and the call is evaluated again, which memoizes the lookahead.
//
// A growth that starts in a context memoizes its steps the same way, with its own seed among the
// seeds a step reads, and records runs of steps that read no seed but their own (jumps): from each
// seed of such a run, the seed at its end, with the observations, the expectations and the
// examined input of the steps from there.
//
// The examined input of a result is the Document examined range [from, examined) (Parser.docLow,
// Parser.docExamined); outside a Document parse it is not tracked and is 0.

// maxLeaderBits is the number of leaders of a cycle that have a bit in present sets. A context holds
// leaders of one cycle only, so the bits are given for each cycle. The present set of a context
// with any other leader is the identity that intern gives its words of bits, tagged presentWide.
const maxLeaderBits = 31

// presentWide tags the identity of a present set that does not fit in a word of bits, which no word
// of bits equals. A memo entry holds a word of bits, so a result in such a context goes to the table
// of the context even if it read no seed (ctxPop), and no entry answers a call there (memoAllows).
const presentWide presentSet = 1 << 31

// presentSet is a present set: the bits of the leaders of a context, or presentWide and an
// identity. The engine's sets have a word of 64 bits, by Rule.LeaderBit.
type presentSet = uint32

// missingBit is the missing-silent bit of a key in the engine's parses with missing-token insertion
// (MemoKey.MissingSilent there). The library inserts no tokens, so its keys hold no such bit: the
// type has no values but its zero and takes no space.
type missingBit = struct{}

// ctxResult is a result that a context keeps: a seed, the result of a growth step, or the result of
// a call that read a seed. It holds a node (Parser) or a typed value (TParser).
//
// A result records the range of input it examined, as a memo entry does, and a call that reuses it
// in a Document parse extends its own examined range by it: the memo entry of a leader that grew
// from context steps examined what those steps examined, and no more, so an edit next to the leader
// leaves it as it leaves the engine's.
type ctxResult struct {
	node     *sxirvalue.Node
	val      any
	errs     recEntry
	expected []ExpID
	end      int
	far      int
	examined int // the examined end of a Document parse (Parser.docExamined)
	from     int // the examined start of a Document parse (Parser.docLow)
	ok       bool
	silent   bool
}

// growRec is a growth in progress: the leader's key, its cycle and index (Rule.leaderIndex), its
// current seed and the seed's identity. A growth in no context holds its seed in inl (seed is nil), which no table
// shares, so that its steps allocate nothing.
type growRec struct {
	rule, min int32
	pos       int
	cycle     int32
	idx       int32
	seed      *ctxResult
	id        uint64
	inl       ctxResult
}

// ctxSlot is a result in a context table: the result, and for a growth step the identity of its
// result and what the step itself recorded (its result is a seed shared by equal results).
type ctxSlot struct {
	e              *ctxResult
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
	end            *ctxResult
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
}

type canonRec struct {
	e  *ctxResult
	id uint64
}

// lookRec is a lookahead in progress: its site (-1 for one that records nothing) and the Document
// examined range from before it.
type lookRec struct {
	site           int32
	examined, from int
}

// ctxCallState is the context of a memoized call of a rule of a cycle (ctxEnter).
type ctxCallState struct {
	gp      int    // the index in gamma of the first growth of the context
	present uint32 // the present set
	ctx     bool   // the call is in a context
	framed  bool   // the call records its dependencies
}

// numberLeaders numbers the leaders of each cycle of rules (Rule.leaderIndex).
func numberLeaders(rules []*Rule) {
	n := map[int32]int32{}
	for _, r := range rules {
		r.leaderIndex = 0
		if r.Leader && r.Cycle != 0 {
			r.leaderIndex = n[r.Cycle]
			n[r.Cycle]++
		}
	}
}

// seedAt returns the seed of gamma[i].
func (p *Parser) seedAt(i int) *ctxResult {
	if g := &p.gamma[i]; g.seed != nil {
		return g.seed
	}
	return &p.gamma[i].inl
}

// examinedNow returns the examined input of the current evaluation: its examined end, or 0 in a parse
// that does not track it.
func (p *Parser) examinedNow() int {
	if p.track {
		return p.docExamined
	}
	return 0
}

// fromNow returns the start of the input that the current evaluation examined, or 0 in a parse that
// does not track it.
func (p *Parser) fromNow() int {
	if p.track {
		return p.docLow
	}
	return 0
}

// ctxMut reports that the check m of the dependency memo is broken in this parse. The library
// breaks none; the engine's tests break each.
func (*Parser) ctxMut(int) bool { return false }

// useMem charges n bytes to the memory used (memUsed).
func (p *Parser) useMem(n uint64) { p.memUsed += n }

// gkey returns the key of the growth's leader.
func (g *growRec) gkey() growKey { return growKey{rule: g.rule, min: g.min} }

// at returns the position of the growth.
func (g *growRec) at() int { return g.pos }

// leaderBit returns the bit of the growth's leader in present sets, or 0 if it has none: the leaders
// from maxLeaderBits on have none.
func (g *growRec) leaderBit() presentSet {
	if g.idx >= maxLeaderBits {
		return 0
	}
	return 1 << g.idx
}

// ctxEnter computes the context of a memoized call of r, a rule of a cycle, at the current position
// and level min. When r is a leader that grows there, it returns the seed, which the call reads
// whatever its variable values; the seed is valid until the next growth begins.
func (p *Parser) ctxEnter(c *ctxCallState, r *Rule, min int) *ctxResult {
	p.use(featureLeftRecursion)
	gp := p.gammaAt(p.Pos, r.Cycle)
	*c = ctxCallState{gp: gp}
	if gp == len(p.gamma) {
		return nil
	}
	if r.Leader {
		if i := p.gammaFind(gp, growKey{rule: int32(r.Id), min: int32(min)}); i >= 0 {
			p.noteSeedRead(i)
			return p.seedAt(i)
		}
	}
	c.ctx = true
	c.present = p.present(gp)
	return nil
}

// memoAllows reports whether a result in the memo with the present set present answers a call
// in the context c: every leader that grows there grew when it was evaluated, and did not matter
// to it.
func (c *ctxCallState) memoAllows(present uint32) bool {
	return !c.ctx || c.present&^present == 0
}

// ctxLookup returns the result kept in the context c for a call of rule at level min with the
// variable values env, or nil.
func (p *Parser) ctxLookup(c *ctxCallState, rule, min int, env []any) *ctxResult {
	if !c.ctx {
		return nil
	}
	s := p.ctxFind(p.Pos, c.gp, ctxHeadKey{kind: ctxCall, id: int32(rule), min: int32(min), present: c.present}, env)
	if s == nil || !p.reusable(s.e.silent) {
		return nil
	}
	p.seedRead()
	p.replayDeps(p.Pos, c.gp, c.present, s.deps)
	return s.e
}

// ctxPush starts recording the dependencies of a call in the context c.
func (p *Parser) ctxPush(c *ctxCallState) {
	if c.ctx {
		p.pushFrame(p.Pos, c.gp)
		c.framed = true
	}
}

// The ways a memoized call stores its result (ctxPop).
const (
	storeMemo = iota // in the memo, with the present set of the call
	storeCtx         // in the table of the context (ctxKeep)
)

// ctxPop ends the dependencies of a call in the context c and returns how the call stores its
// result: in the table of the context if it read a seed or made an observation or its present set
// has no word of bits (presentWide), and otherwise in the memo. The dependencies stay readable for ctxKeep until ctxDone.
func (p *Parser) ctxPop(c *ctxCallState) int {
	switch {
	case c.framed:
		_, p.popped = p.popFrame()
		if c.present&presentWide != 0 {
			return storeCtx
		}
		for _, d := range p.popped {
			if d.kind != depShadow {
				return storeCtx
			}
		}
		return storeMemo
	}
	return storeMemo
}

// ctxKeep keeps the result res of a call of rule at start and level min with the variable values
// env in the table of the context c, with the dependencies that ctxPop ended.
func (p *Parser) ctxKeep(c *ctxCallState, rule, start, min int, env []any, res *ctxResult) {
	sd, _ := p.stored(p.popped)
	p.ctxStore(start, c.gp, ctxHeadKey{kind: ctxCall, id: int32(rule), min: int32(min), present: c.present}, sd, &ctxSlot{e: res, env: env})
}

// ctxDone passes the dependencies that ctxPop ended to the enclosing frame.
func (p *Parser) ctxDone(c *ctxCallState) {
	if c.framed {
		p.mergeDeps(p.popped)
		p.popped = nil
	}
}

// pushFrame starts recording the dependencies of an evaluation at pos, in the context of the
// growths gamma[gp:] there.
func (p *Parser) pushFrame(pos, gp int) {
	p.frames = append(p.frames, depFrame{pos: pos, gp: gp, depth: len(p.gamma), base: len(p.deps)})
}

// noteSeedRead records that the evaluation read the seed of gamma[i].
func (p *Parser) noteSeedRead(i int) {
	p.seedRead()
	if len(p.frames) == 0 || i >= p.frames[len(p.frames)-1].depth {
		return
	}
	p.addDep(dep{kind: depSeed, idx: int32(i), val: p.gamma[i].id})
}

// LookBegin starts a lookahead of the given site. In a context at the current position, the
// lookahead records what it reads, so that a result that depends on it records its outcome
// instead of the seeds it read (LookEnd). Where no frame records dependencies, a lookahead records
// nothing, and the frames are the same at its end.
//
// LookBegin is for generated parsers; not a stable API.
func (p *Parser) LookBegin(site int) {
	if len(p.frames) != 0 {
		p.lookBegin(site)
	}
}

// LookEnd ends the innermost lookahead, whose expression matched if ok.
//
// LookEnd is for generated parsers; not a stable API.
func (p *Parser) LookEnd(ok bool) {
	if len(p.frames) != 0 {
		p.lookEnd(ok)
	}
}

func (p *Parser) lookBegin(site int) {
	{
		f := &p.frames[len(p.frames)-1]
		if g := len(p.gamma); f.pos == p.Pos && f.gp < g && p.gamma[g-1].pos == p.Pos && p.gamma[g-1].cycle == p.gamma[f.gp].cycle {
			// The examined range of the lookahead is part of its observation.
			p.looks = append(p.looks, lookRec{site: int32(site), examined: p.docExamined, from: p.docLow})
			if p.track {
				p.docExamined, p.docLow = p.Pos, p.Pos
			}
			p.pushFrame(p.Pos, f.gp)
			return
		}
	}
	p.looks = append(p.looks, lookRec{site: -1})
}

func (p *Parser) lookEnd(ok bool) {
	l := p.looks[len(p.looks)-1]
	p.looks = p.looks[:len(p.looks)-1]
	site := l.site
	if site < 0 {
		return
	}
	f, ds := p.popFrame()
	examined, from := f.pos, f.pos
	if p.track {
		examined, from = p.docExamined, p.docLow
		p.docExamined, p.docLow = max(l.examined, p.docExamined), min(l.from, p.docLow)
	}
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
	val, _ := p.obsOutcome(ok, f.pos, examined, from)
	present := p.present(f.gp)
	sd, _ := p.stored(ds)
	p.ctxStore(f.pos, f.gp, ctxHeadKey{kind: ctxLook, id: site, present: present}, sd, &ctxSlot{id: val})
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

// --- Growths ---

// growState is the state of the growth of a leader.
type growState struct {
	rule, min int32
	start     int
	gi        int // the index of the growth in gamma
	gp        int // the index in gamma of the first growth of the leader's cycle at the position
	// ctx reports a growth in a context, whose steps are memoized with the present set present.
	ctx     bool
	present uint32
	// evaluated reports that a growth in a context evaluated the body for a step (growIsolate); one that
	// took every step from the context memo evaluated nothing, and its call reuses that memo (growEnd). A
	// growth outside a context evaluates every step and does not set it.
	evaluated bool
	// The step being evaluated in a context: the Document examined range and the expectation record it
	// is kept apart from.
	savedExamined, savedFrom int
	exp                      ExpMark
	// run holds the steps since the last step that read a seed other than the leader's own.
	run []runStep
	// The step that growKeepStep returned last, until growAdoptStep adopts it: its identity as a seed and
	// what it recorded.
	pendID   uint64
	pendStep runStep
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

func (g *growState) growKey() growKey { return growKey{rule: g.rule, min: g.min} }

// growBegin starts the growth of the leader r at level min at the current position from a failure.
// A growth in a context, where another leader of its cycle grows, memoizes its steps (growMemoStep, growKeepStep);
// the body is evaluated repeatedly, and the result at the point where the match stops growing is
// adopted.
func (p *Parser) growBegin(r *Rule, min int) growState {
	p.use(featureLeftRecursion)
	// One compute unit before each evaluation of the body, the seed's included.
	p.Spend()
	g := growState{rule: int32(r.Id), min: int32(min), start: p.Pos, gi: len(p.gamma)}
	g.gp = p.gammaAt(p.Pos, r.Cycle)
	// The record is written in place: a growth is pushed for every call of a leader that misses.
	if g.gi < cap(p.gamma) {
		p.gamma = p.gamma[:g.gi+1]
	} else {
		p.gamma = append(p.gamma, growRec{})
	}
	rec := &p.gamma[g.gi]
	rec.rule, rec.min, rec.pos, rec.cycle, rec.idx, rec.id = g.rule, g.min, g.start, r.Cycle, r.leaderIndex, failSeed
	if g.gp < g.gi {
		// The seeds of a growth in a context can be shared by equal results (canonicalSeed).
		rec.seed = &ctxResult{end: g.start, examined: p.examinedNow(), from: g.start}
		rec.inl = ctxResult{}
	} else {
		// A growth in no context allocates nothing.
		rec.seed = nil
		rec.inl = ctxResult{end: g.start, examined: p.examinedNow(), from: g.start}
	}
	if g.gp < g.gi {
		// The present set of the steps includes the leader's own growth.
		g.present, g.ctx = p.present(g.gp), true
	}
	return g
}

// growMemoStep takes the steps of a growth in a context that its table gives, from the jumps and the
// memoized steps, replaying their dependencies and merging their expectations, and adopts each step that
// grew. It returns the first memoized step that did not grow, or nil when the table keeps no step from
// the seed.
func (p *Parser) growMemoStep(g *growState) *ctxResult {
	for {
		id := p.gamma[g.gi].id
		if j := p.findJump(g, id); j != nil {
			p.seedRead()
			p.replayDeps(g.start, g.gp, g.present, j.deps)
			p.MergeExpected(j.far, j.exp)
			// The step after the jump reads the seed it lands on, whose examined range is the jump's
			// (recordJumps), so this touch only keeps the jump a complete replay of its steps.
			p.documentTouch(j.examined)
			p.documentTouchFrom(j.from)
			p.setSeed(g, j.end, j.id)
			continue
		}
		h := ctxHeadKey{kind: ctxStep, id: g.rule, min: g.min, present: g.present}
		s := p.ctxFind(g.start, g.gp, h, nil)
		if s == nil || !p.reusable(s.silent) {
			return nil
		}
		p.seedRead()
		p.replayDeps(g.start, g.gp, g.present, s.deps)
		p.MergeExpected(s.far, s.exp)
		// A step taken from the context records what it examined, like every other reuse: the last step
		// of a growth, which does not grow, can be taken from the context, and nothing else records how
		// far it examined.
		p.documentTouch(s.examined)
		p.documentTouchFrom(s.from)
		if !p.growGrew(g, s.e) {
			return s.e
		}
		p.adoptGrownStep(g, s.e, s.id, runStep{seed: id, far: s.far, exp: s.exp, examined: s.examined, from: s.from, silent: s.silent, deps: s.deps})
	}
}

// growIsolate starts evaluating a step of a growth in a context, with its own frame, examined end and
// expectations.
func (p *Parser) growIsolate(g *growState) {
	g.evaluated = true
	p.pushFrame(g.start, g.gp)
	if p.track {
		g.savedExamined, g.savedFrom = p.docExamined, p.docLow
		p.docExamined, p.docLow = g.start, g.start
	}
	g.exp = p.Isolate(g.start)
}

// growLonger reports whether a step of a growth outside a context, with success ok and ending at the
// position, matched more than the seed.
func (p *Parser) growLonger(g *growState, ok bool) bool {
	best := &p.gamma[g.gi].inl
	return ok && (!best.ok || p.Pos > best.end)
}

// growAdopt adopts the successful longer step of a growth outside a context, a node or a typed value,
// with the errors errs that it recovered, as its seed, and returns to the start of the growth. The
// caller drops the errors after the growth's mark.
func (p *Parser) growAdopt(g *growState, node *sxirvalue.Node, val any, errs recEntry) {
	p.Spend()
	p.gamma[g.gi].inl = ctxResult{node: node, val: val, ok: true, end: p.Pos, examined: p.examinedNow(), from: p.fromNow(), errs: errs}
	p.gamma[g.gi].id = p.nextSeedID()
	p.Pos = g.start
}

// growKeepStep ends the evaluation of a step of a growth in a context that growIsolate began, with its
// result, a node or a typed value, and the errors errs that it recovered if it succeeded, keeps the result
// in the table of the context and returns it. It does not change the recovered errors: the printed growth
// lists the errors of the step with recSince and drops them after the step.
func (p *Parser) growKeepStep(g *growState, node *sxirvalue.Node, val any, ok bool, errs recEntry) *ctxResult {
	id := p.gamma[g.gi].id
	_, ds := p.popFrame()
	far, inner := p.Unisolate(g.exp)
	exp := p.Keep(inner)
	examined, from := p.examinedNow(), p.fromNow()
	p.MergeExpected(far, inner)
	if p.track {
		p.docExamined, p.docLow = max(g.savedExamined, p.docExamined), min(g.savedFrom, p.docLow)
	}
	res := &ctxResult{node: node, val: val, ok: ok, end: p.Pos, examined: examined, from: from, silent: p.Silent > 0}
	if ok {
		res.errs = errs
	} else {
		res.end = g.start
	}
	res, rid := p.canonicalSeed(g, res)
	sd, _ := p.stored(ds)
	h := ctxHeadKey{kind: ctxStep, id: g.rule, min: g.min, present: g.present}
	p.ctxStore(g.start, g.gp, h, sd, &ctxSlot{e: res, id: rid, far: far, exp: exp, examined: examined, from: from, silent: p.Silent > 0})
	p.mergeDeps(ds)
	g.pendID = rid
	g.pendStep = runStep{seed: id, far: far, exp: exp, examined: examined, from: from, silent: p.Silent > 0, deps: sd}
	return res
}

// growGrew reports whether the step s of a growth in a context matched more than the seed.
func (p *Parser) growGrew(g *growState, s *ctxResult) bool {
	best := p.gamma[g.gi].seed
	return s.ok && (!best.ok || s.end > best.end)
}

// nextSeedID returns a new seed identity.
func (p *Parser) nextSeedID() uint64 {
	p.seedSeq++
	return p.seedSeq + failSeed
}

// growAdoptStep adopts the step s of a growth in a context that grew, which growKeepStep returned last,
// as the seed.
func (p *Parser) growAdoptStep(g *growState, s *ctxResult) {
	p.adoptGrownStep(g, s, g.pendID, g.pendStep)
}

// adoptGrownStep adopts the step s of a growth in a context that grew, with identity id and what it
// recorded st, as the seed, and returns to the start of the growth. It extends the run of steps that read
// no other seed with the step, or ends it.
func (p *Parser) adoptGrownStep(g *growState, s *ctxResult, id uint64, st runStep) {
	own := g.growKey()
	other := false
	var obs []sdep
	for _, d := range st.deps {
		switch {
		case d.kind == depSeed && (growKey{rule: d.rule, min: d.min, ms: d.ms}) != own:
			other = true
		case d.kind == depShadow && (growKey{rule: d.rule, min: d.min, ms: d.ms}) == own:
			// A lookahead of the step read the growth's own seed: its outcome holds for this seed
			// only, while a jump checks its observations under the first seed of the run.
			other = true
		case d.kind == depObs || d.kind == depShadow:
			obs = append(obs, d)
		}
	}
	if !other && s.errs.none() {
		st.deps = obs
		c := cap(g.run)
		g.run = append(g.run, st)
		p.memUsed += uint64(cap(g.run)-c)*runStepBytes + uint64(cap(obs))*sdepBytes
	} else {
		p.recordJumps(g)
	}
	p.Spend()
	p.setSeed(g, s, id)
	p.Pos = g.start
}

// setSeed makes e, with identity id, the seed of the growth in a context.
func (p *Parser) setSeed(g *growState, e *ctxResult, id uint64) {
	p.gamma[g.gi].seed, p.gamma[g.gi].id = e, id
}

// growEnd ends the growth and returns its last result, which the call stores. It does not change the
// recovered errors: the printed growth drops the entries after the growth's mark and records the errors
// of the result.
func (p *Parser) growEnd(g *growState) ctxResult {
	if g.ctx && !g.evaluated {
		// Every step came from the context memo: the call reuses memoized results without evaluating
		// its body, like a memo hit (Memo=true, Evals=0), and a Document counts the reuse.
		p.Observation.memoHit()
		if p.track && p.Document != nil {
			p.Document.stats.Reused++
		}
	}
	p.recordJumps(g)
	p.dropTable(g.gi)
	best := *p.seedAt(g.gi)
	clear(p.gamma[g.gi:]) // release the seeds
	p.gamma = p.gamma[:g.gi]
	if g.gi == 0 && len(p.interned) > 0 {
		clear(p.interned)
		p.internSeq = 0
	}
	p.Pos = best.end
	return best
}

// recordJumps records, for each seed of the run, a jump to the current seed with what the steps
// from that seed on recorded, and starts a new run.
func (p *Parser) recordJumps(g *growState) {
	if len(g.run) == 0 {
		return
	}
	t := p.table(g.start, g.gp, true)
	if t.jumps == nil {
		t.jumps = map[jumpKey][]jumpRec{}
	}
	cur := &p.gamma[g.gi]
	acc := jumpRec{end: cur.seed, id: cur.id, far: -1, examined: g.start, from: g.start}
	for i := len(g.run) - 1; i >= 0; i-- {
		s := &g.run[i]
		p.Spend()
		switch {
		case s.far > acc.far:
			acc.far, acc.exp = s.far, s.exp
		case s.far == acc.far:
			acc.exp = p.unionExpected(acc.exp, s.exp)
		}
		acc.examined, acc.from = max(acc.examined, s.examined), min(acc.from, s.from)
		acc.silent = acc.silent || s.silent
		acc.deps = mergeSdeps(acc.deps, s.deps)
		k := jumpKey{g: g.growKey(), present: g.present, seed: s.seed}
		if !p.jumpShadowed(t.jumps[k], &acc) {
			t.jumps[k] = append(t.jumps[k], acc)
			t.bytes += jumpBytes + uint64(len(acc.deps))*sdepBytes + uint64(len(acc.exp))*expIDBytes
		}
	}
	g.run = g.run[:0]
}

// jumpShadowed reports whether findJump never returns the jump j after the jumps js: one of them
// is used wherever j is, since its observations are among j's and it is not inside a lookahead
// where j is not. Leaving such jumps out keeps the list of a seed from growing in each growth
// that ends its run there again, with the same observations and other shadows.
func (p *Parser) jumpShadowed(js []jumpRec, j *jumpRec) bool {
next:
	for i := range js {
		e := &js[i]
		p.Spend()
		if e.silent && !j.silent {
			continue
		}
		for _, d := range e.deps {
			if d.kind == depObs && !hasSdep(j.deps, d) {
				continue next
			}
		}
		return true
	}
	return false
}

// hasSdep reports whether ds holds d.
func hasSdep(ds []sdep, d sdep) bool {
	for _, x := range ds {
		if x == d {
			return true
		}
	}
	return false
}

// findJump returns a jump from the seed with identity id that holds in the current context.
func (p *Parser) findJump(g *growState, id uint64) *jumpRec {
	t := p.table(g.start, g.gp, false)
	if t == nil || t.jumps == nil {
		return nil
	}
	p.Spend()
	js := t.jumps[jumpKey{g: g.growKey(), present: g.present, seed: id}]
next:
	for i := range js {
		j := &js[i]
		p.Spend()
		if j.silent && p.Silent == 0 {
			continue
		}
		// Each step of the jump made its observations under its own seed, and the jump is taken
		// from the first: an observation holds for the jump only if its outcome is the same for
		// every seed of the growth.
		for _, d := range j.deps {
			if d.kind != depObs {
				continue
			}
			if v, free := p.obsFree(g.start, g.gp, g.present, d.site, g.growKey()); v != d.val || !free {
				continue next
			}
		}
		return j
	}
	return nil
}

// mergeSdeps returns the union of two lists of stored dependencies.
func mergeSdeps(a, b []sdep) []sdep {
	if len(b) == 0 {
		return a
	}
	out := append([]sdep(nil), a...)
	for _, d := range b {
		found := false
		for _, x := range out {
			if x == d {
				found = true
				break
			}
		}
		if !found {
			out = append(out, d)
		}
	}
	return out
}

// unionExpected returns the expectations of a and b, at the same farthest position.
func (p *Parser) unionExpected(a, b []ExpID) []ExpID {
	if len(b) == 0 {
		return a
	}
	out := append([]ExpID(nil), a...)
	for _, x := range b {
		p.Spend()
		found := false
		for _, y := range out {
			if y == x {
				found = true
				break
			}
		}
		if !found {
			out = append(out, x)
		}
	}
	return out
}

// canonicalSeed returns the identity of the result e of a growth step, the same as an earlier
// equal result's of the leader at the position, whose result it then returns instead of e.
func (p *Parser) canonicalSeed(g *growState, e *ctxResult) (*ctxResult, uint64) {
	id := p.nextSeedID()
	if !e.ok || !e.errs.none() {
		return e, id
	}
	t := p.table(g.start, g.gp, true)
	if t.canon == nil {
		t.canon = map[canonKey]canonRec{}
	}
	ck := canonKey{g: g.growKey(), end: e.end}
	p.Spend()
	if c, ok := t.canon[ck]; ok {
		budget := 64
		if c.e.examined == e.examined && c.e.from == e.from && c.e.errs.none() && p.nodesEqual(c.e.node, e.node, &budget) && p.valuesEqual(reflect.ValueOf(c.e.val), reflect.ValueOf(e.val), &budget) {
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
	p.Spend()
	if D(a).Kind != D(b).Kind || D(a).Start != D(b).Start || D(a).End != D(b).End || D(a).Text != D(b).Text || D(a).Fresh != D(b).Fresh || D(a).Terminal != D(b).Terminal || D(a).Labeled != D(b).Labeled || D(a).Box != nil || D(b).Box != nil ||
		len(D(a).Children) != len(D(b).Children) || len(D(a).Fields) != len(D(b).Fields) {
		return false
	}
	for i := range D(a).Children {
		if !p.nodesEqual(D(a).Children[i], D(b).Children[i], budget) {
			return false
		}
	}
	for i := range D(a).Fields {
		fa, fb := &D(a).Fields[i], &D(b).Fields[i]
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

// valuesEqual reports whether two typed values are equal, comparing at most *budget values of
// composite kinds (false beyond, and for kinds it does not compare: functions, channels, maps and
// unsafe pointers).
func (p *Parser) valuesEqual(a, b reflect.Value, budget *int) bool {
	if !a.IsValid() || !b.IsValid() {
		return a.IsValid() == b.IsValid()
	}
	if a.Type() != b.Type() {
		return false
	}
	switch a.Kind() {
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() == b.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return a.Uint() == b.Uint()
	case reflect.Float32, reflect.Float64:
		return a.Float() == b.Float()
	case reflect.String:
		return a.String() == b.String()
	}
	if *budget--; *budget < 0 {
		return false
	}
	p.Spend()
	switch a.Kind() {
	case reflect.Pointer:
		if a.Pointer() == b.Pointer() {
			return true
		}
		if a.IsNil() || b.IsNil() {
			return false
		}
		return p.valuesEqual(a.Elem(), b.Elem(), budget)
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return p.valuesEqual(a.Elem(), b.Elem(), budget)
	case reflect.Slice:
		if a.IsNil() != b.IsNil() || a.Len() != b.Len() {
			return false
		}
		if a.Len() > 0 && a.Pointer() == b.Pointer() {
			return true
		}
		fallthrough
	case reflect.Array:
		for i := range a.Len() {
			if !p.valuesEqual(a.Index(i), b.Index(i), budget) {
				return false
			}
		}
		return true
	case reflect.Struct:
		for i := range a.NumField() {
			if !p.valuesEqual(a.Field(i), b.Field(i), budget) {
				return false
			}
		}
		return true
	}
	return false
}
