package state

import "syntaxir.dev/sxir/pkg/runtime/value"

// The engine's implementations of the operations of the call protocol that its printed
// compositions call (call_exec.go, printed from internal/ir/code/exec/compose): the call state, the context
// state of a memoized call, the bit of an expectation in the filter of the current record (expBit) and
// the results that a context holds.

// expBit returns the bit of id in the filter of the expectations of the current record (ExpBits),
// which exp.maybe reads.
func expBit(id ExpID) uint64 {
	return uint64(1) << (uint32(id) * 0x9e3779b1 >> 26)
}

// KeyVals are the values of a memo key beyond its rule, position and level: the values of the
// variables the rule reads, and whether the call is in a lookahead that inserts no missing token,
// which has its own results.
type KeyVals struct {
	Env     []any
	Missing bool
}

// keyVals returns the values of the memo key of a call of rule r at the position.
func (p *Parser) keyVals(r *Rule) KeyVals {
	kv := KeyVals{Missing: p.Missing != nil && r.Missing && p.Silent > 0}
	if len(r.Vars) > 0 {
		kv.Env = p.memoEnv(r)
	}
	return kv
}

// GrowKey returns the key of the growth of leader r at binding level min at the position. A growth
// is told apart by its rule, level and missing-token state: the values of variables do not take
// part.
func (p *Parser) GrowKey(r *Rule, min int) MemoKey {
	return MemoKey{Rule: r.ID, Pos: p.Pos, Min: min, MissingSilent: p.Missing != nil && r.Missing && p.Silent > 0}
}

// memoized reports whether the results of rule r, which leads no left-recursive cycle, are memoized
// in the parse (Memoizes).
func (p *Parser) memoized(r *Rule) bool {
	return r.Memo && (!r.Transient || p.MemoAll && !r.ValuelessLexical()) && !(p.Missing != nil && r.Missing)
}

// docOnlyMemo reports whether the parse memoizes rule r and a parse from scratch does not: a transient
// rule other than a value-less lexical one in a Document parse. The memo protocol of such a rule is
// Document work.
func (p *Parser) docOnlyMemo(r *Rule) bool {
	return p.MemoAll && r.Memo && r.Transient && !r.ValuelessLexical() && !r.Leader && !(p.Missing != nil && r.Missing)
}

// SkipGuarded reports whether a choice may skip an alternative whose guard rejects the next
// character, and counts the units of the skip. calls are the calls that the evaluation of the
// alternative makes before it fails at its first terminal, outermost first, and exact reports
// whether they are known (core.GuardCalls). Under limit controls and in a Document parse (MemoAll),
// a skip must leave the memo, and under limit controls the compute units, as the evaluation leaves
// them (spec/runtime/parsing.md#evaluations-of-rules), so that neither depends on whether a skip
// happened, nor a Document's memo and statistics on its limits. A skip whose calls are known and
// none of whose rules the parse memoizes is allowed, and under limit controls counts one unit for
// each call, which is all that those calls count before the terminal fails; any other skip is
// refused, and the alternative is evaluated. Outside Documents, without limit controls, every skip
// is allowed.
func (p *Parser) SkipGuarded(calls []*Rule, exact bool) bool {
	controlled := BudgetControlsEnabled && p.Control != nil
	if !controlled && !p.MemoAll {
		return true
	}
	if !exact {
		return false
	}
	for _, r := range calls {
		if r.Leader || p.memoized(r) {
			return false
		}
	}
	if controlled && len(calls) > 0 {
		p.Control.Charge(uint64(len(calls)))
	}
	return true
}

// CallCharge charges a call of r as Call does: one unit, and the lookup of its memo key (ChargeKey)
// for a rule that a parse from scratch memoizes, whether or not this call looks it up.
func (p *Parser) CallCharge(r *Rule) {
	p.Spend()
	if r.Leader || p.memoized(r) && !p.docOnlyMemo(r) {
		p.ChargeKey(r)
	}
}

// CallPlain is a call of a plain rule (Rule.Plain) that a call site makes without the call protocol:
// the unit of the call, as Call charges it, and the unmemoized call (InvokePlain). A parse from scratch
// does not memoize a plain rule, so the call has no key to pay for.
func (p *Parser) CallPlain(r *Rule, min int) (*value.Node, bool) {
	p.Spend()
	return p.InvokePlain(r, min)
}

// The iterative machine writes the call protocol out on its hot path. In a Document parse (MemoAll)
// it calls the forms below, which run the memo protocol as Document work; a parse from scratch runs
// the plain steps without a call.

// DirectCallDoc reports whether the call of r runs its body unmemoized, as the test of Call does:
// the rule is not memoized, or its memoization is deferred. The test is Document work for a rule that
// only a Document parse memoizes.
func (p *Parser) DirectCallDoc(r *Rule) bool {
	m := p.FreeBegin(p.docOnlyMemo(r))
	direct := !p.Memoizes(r) || p.FirstCall(r)
	p.FreeEnd(m)
	return direct
}

// CallBeginDoc is CallBegin, as work of the Document for a rule that only a Document parse memoizes.
func (p *Parser) CallBeginDoc(r *Rule, min int, st *CallState) (*value.Node, bool, bool) {
	m := p.FreeBegin(p.docOnlyMemo(r))
	v, ok, hit := p.CallBegin(r, min, st)
	p.FreeEnd(m)
	return v, ok, hit
}

// CallEndDoc is CallEnd, as work of the Document for a rule that only a Document parse memoizes.
func (p *Parser) CallEndDoc(r *Rule, st *CallState, v *value.Node, ok bool) (*value.Node, bool) {
	m := p.FreeBegin(p.docOnlyMemo(r))
	v, ok = p.CallEnd(r, st, v, ok)
	p.FreeEnd(m)
	return v, ok
}

// tracing reports whether a call takes the traced path: in a traced parse, except for the call
// that tracedCall makes back, whose mark it clears.
func (p *Parser) tracing() bool {
	if !p.Tr.direct {
		return true
	}
	p.Tr.direct = false
	return false
}

// CallState is the state of a memoized call between CallBegin and CallEnd, which its caller holds.
type CallState struct {
	start, min int
	key        KeyVals
	ctx        CtxState
	rec        int
	exp        ExpMark
	// examined and low are the examined range of the enclosing call; missing is the mark of the
	// missing-token record.
	examined, low int
	missing       int
}

// CtxState is the context state of a memoized call (see context_memo.go).
type CtxState struct {
	// gp is the index in gamma of the first growth of the rule's cycle at the position. ctx reports
	// a call in a context, with the present set present; framed reports that the call records its
	// dependencies.
	gp      int
	present uint64
	ctx     bool
	framed  bool
	// ds are the dependencies that the call's evaluation recorded, and sd those that a result kept
	// in its context stores (ctxPop).
	ds []dep
	sd []sdep
}

// Where ctxPop sends the result of a call.
const (
	storeNowhere = iota
	storeMemo
	storeCtx
)

// ctxEnter begins the memoized call of rule r, a rule of a left-recursive cycle, at binding level
// min at the position, in a parse where leaders grow: it fills c and returns the seed of r if r is
// a leader that grows here, recording the read of the seed.
func (p *Parser) ctxEnter(c *CtxState, r *Rule, min int) *MemoEntry {
	gp := p.gammaAt(p.Pos, int32(r.Cycle))
	c.gp = gp
	if gp == len(p.gamma) {
		return nil
	}
	if r.Leader {
		// A leader that grows here reads its seed, whatever its variable values.
		if i := p.gammaFind(gp, growKey{rule: int32(r.ID), min: int32(min), ms: p.Missing != nil && r.Missing && p.Silent > 0}); i >= 0 {
			p.noteSeedRead(i)
			return p.gamma[i].seed
		}
	}
	c.ctx = true
	c.present = p.present(gp)
	c.framed = !p.strictCtx() && !p.inserts(r) && p.recordsIn(c.present)
	return nil
}

// ctxAllows reports whether the call in a context with the state c may reuse the memo entry e:
// when every leader that grows here grew when e was evaluated, and did not matter to it. No entry
// answers a call in a context whose present set has no word of bits (presentWide).
func (p *Parser) ctxAllows(c *CtxState, e *MemoEntry) bool {
	return p.tune.MemoMut == CtxIgnorePresent || !p.strictCtx() && c.present&^e.Present == 0
}

// ctxLookup returns the result kept in the context of the call of rule r at binding level min with
// the key values kv at the position, which the call may reuse, and records what it depends on for
// the enclosing call; otherwise none.
func (p *Parser) ctxLookup(c *CtxState, r *Rule, min int, kv KeyVals) *MemoEntry {
	if !c.framed {
		return nil
	}
	h := ctxHeadKey{kind: ctxCall, id: int32(r.ID), min: int32(min), ms: kv.Missing, present: c.present}
	s := p.ctxFind(p.Pos, c.gp, h, kv.Env)
	if s == nil || !p.reusable(s.e.silent) {
		return nil
	}
	p.seedRead(c.gp)
	if p.tune.MemoMut != CtxNoReplayLookup {
		p.replayDeps(p.Pos, c.gp, c.present, s.deps)
	}
	return s.e
}

// ctxPop ends recording the dependencies of the evaluation of a call in a context and returns
// where its result goes: storeMemo for a result that read no seed, storeCtx for one that did or
// whose context's present set has no word of bits (presentWide), and storeNowhere for a call whose
// context records no dependencies. A result that goes nowhere is dropped: the last result of a
// leader's growth is not retained.
func (p *Parser) ctxPop(c *CtxState) int {
	if !c.framed {
		p.grown = nil
		return storeNowhere
	}
	_, c.ds = p.popFrame()
	sd, read := p.stored(c.ds)
	if !read && c.present&presentWide == 0 {
		return storeMemo
	}
	c.sd = sd
	return storeCtx
}

// ctxKeep keeps the entry e of the call of rule r at pos, binding level min and key values kv in
// the table of its context, with the dependencies that ctxPop ended.
func (p *Parser) ctxKeep(c *CtxState, r *Rule, pos, min int, kv KeyVals, e *MemoEntry) {
	p.ctxStore(pos, c.gp, ctxHeadKey{kind: ctxCall, id: int32(r.ID), min: int32(min), ms: kv.Missing, present: c.present},
		c.sd, &ctxSlot{e: e, env: kv.Env})
	if p.tune.MemoMut == CtxKeepSeedResults {
		p.keptSeedResults = append(p.keptSeedResults, keptSeedResult{MemoKey{Rule: r.ID, Pos: pos, Min: min, Env: kv.Env, MissingSilent: kv.Missing}, e})
	}
}

// leaderEntry returns the entry that the keep of a leader's result in a context stores: a copy of the result
// that GrowEnd made (Parser.grown), as a result does not change.
func (p *Parser) leaderEntry() *MemoEntry {
	e := p.grown
	p.grown = nil
	if p.tune.MemoMut == CtxKeepInPlace {
		return e
	}
	cp := *e
	return &cp
}

// growEnd ends the growth g of leader r and returns its last result, which CallEnd stores.
func (p *Parser) growEnd(r *Rule, g *GrowState) *MemoEntry {
	p.GrowEnd(r, g)
	return p.grown
}
