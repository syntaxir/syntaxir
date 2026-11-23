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

// maxLeaderBits is the number of leaders of a cycle that have a bit in present sets, the same on
// every backend (program.MaxLeaderBits in the engine). A context holds leaders of one cycle only, so
// the bits are given for each cycle. The present set of a context with any other leader is the
// identity that intern gives its words of bits, tagged presentWide.
const maxLeaderBits = 31

// presentSet is a present set: the bits of the leaders of a context, or presentWide and an
// identity. The engine holds the same sets in words of 64 bits.
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

// lookMark is what the end of a lookahead restores; the library restores nothing there (the printed
// lookahead drops its own errors), so the type is empty.
type lookMark = struct{}

// lookMark returns the marks of a lookahead that starts now.
func (*Parser) lookMark() lookMark { return lookMark{} }

// lookRestore restores nothing in the library.
func (*Parser) lookRestore(lookMark) {}

// examRange returns the examined range of the current evaluation in a Document parse, or zeros.
func (p *Parser) examRange() (examined, from int) { return p.examinedNow(), p.fromNow() }

// setExamRange sets the examined range of the current evaluation in a Document parse.
func (p *Parser) setExamRange(examined, from int) {
	if p.track {
		p.docExamined, p.docLow = examined, from
	}
}

// recordsIn reports whether evaluations in a context with the present set present record their
// dependencies: always in the library.
func (*Parser) recordsIn(presentSet) bool { return true }

// ctxRes is the result type of the shared context memo (context_table.go).
type ctxRes = ctxResult

// succeeded reports whether the result matched.
func (e *ctxResult) succeeded() bool { return e.ok }

// resultEnd returns the end of the result.
func (e *ctxResult) resultEnd() int { return e.end }

// errFree reports whether the result recovered no errors.
func (e *ctxResult) errFree() bool { return e.errs.none() }

// examinedEnd and examinedFrom return the range of input that the result examined in a Document
// parse; both are 0 in a parse that does not track it.
func (e *ctxResult) examinedEnd() int  { return e.examined }
func (e *ctxResult) examinedFrom() int { return e.from }

// rangeKept reports whether two results that examined different ranges differ: in a Document parse,
// whose reuse after an edit depends on the ranges. Other parses, observed ones included, compare
// growing results and outcomes without their ranges (spec/runtime/parsing.md, Evaluations of Rules).
func (p *Parser) rangeKept() bool { return usedFeatures&featureDocument != 0 && p.Document != nil }

// sameResult reports whether two results are equal, comparing at most *budget pairs of distinct
// nodes and values (nodesEqual, valuesEqual).
func (p *Parser) sameResult(a, b *ctxResult, budget *int) bool {
	return p.nodesEqual(a.node, b.node, budget) && p.valuesEqual(reflect.ValueOf(a.val), reflect.ValueOf(b.val), budget)
}

// gkey returns the key of the growth's leader.
func (g *growRec) gkey() growKey { return growKey{rule: g.rule, min: g.min} }

// at returns the position of the growth.
func (g *growRec) at() int { return g.pos }

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
	p.seedRead(c.gp)
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

// LookBegin starts a lookahead of the given site (lookBegin). In a context at the current position, the
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

func (g *growState) growKey() growKey { return growKey{rule: g.rule, min: g.min} }

// growBegin starts the growth of the leader r at level min at the current position from a failure.
// A growth in a context, where another leader of its cycle grows, memoizes its steps (growMemoStep, growKeepStep);
// the body is evaluated repeatedly, and the result at the point where the match stops growing is
// adopted.
func (p *Parser) growBegin(r *Rule, min int) growState {
	p.use(featureLeftRecursion)
	// One compute unit before each evaluation of the body, the seed's included.
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
	p.gamma[g.gi].inl = ctxResult{node: node, val: val, ok: true, end: p.Pos, examined: p.examinedNow(), from: p.fromNow(), errs: errs}
	p.gamma[g.gi].id = p.nextSeedID()
	p.Pos = g.start
}

// growKeepStep ends the evaluation of a step of a growth in a context that growIsolate began, with its
// result, a node or a typed value, and the errors errs that it recovered if it succeeded, keeps the result
// in the table of the context and returns it. It does not change the recovered errors: the printed growth
// lists the errors of the step with recSince and drops them after the step.
func (p *Parser) growKeepStep(g *growState, node *sxirvalue.Node, val any, ok bool, errs recEntry) *ctxResult {
	ds, st := p.growStepEnd(g)
	res := &ctxResult{node: node, val: val, ok: ok, end: p.Pos, examined: st.examined, from: st.from, silent: st.silent}
	if ok {
		res.errs = errs
	} else {
		res.end = g.start
	}
	return p.growKeep(g, res, ds, st)
}

// growth is the state of a growth that the shared steps read (context_grow.go).
type growth = growState

// touchRange extends the examined range of the current evaluation in a Document parse by a reused
// result's.
func (p *Parser) touchRange(examined, from int) {
	p.documentTouch(examined)
	p.documentTouchFrom(from)
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

// samePositionBasis reports whether the positions of the nodes a and b account for the same edits
// of a Document: always, since a Document moves a reused value before a result can hold it
// (documentMove), so that every node of a result holds positions of the current text. Mark is the
// number of a move there, which says nothing about positions.
func (*Parser) samePositionBasis(_, _ *sxirvalue.Node) bool { return true }
