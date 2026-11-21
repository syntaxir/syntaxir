package conform

import (
	"fmt"
	"math/rand/v2"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
)

// The cases of the dependency memo of left recursion (the ctx operations) and of the growth outside a
// context (grow.begin, grow.ctx, grow.longer and grow.adopt). They also install growths and begin and
// end lookaheads with conformance operations (ref.CtxGrowOp and the others) instead of running the
// growth in a context, so that the ctx operations are checked apart from how its steps compute seed
// identities. A Value handle comes from OprNodeOp, so an implementation of the family is also an
// Operators that makes that handle.

// CtxOps are the operations of the dependency memo that cases for the reference model cover.
var CtxOps = []string{
	"ctx.enter", "ctx.found", "ctx.ok", "ctx.value", "ctx.end", "ctx.errs", "ctx.far", "ctx.expected",
	"ctx.allows", "ctx.active", "ctx.lookup", "ctx.push", "ctx.pop", "ctx.storeMemo", "ctx.storeCtx", "memo.setPresent",
	"ctx.keep", "ctx.done", "grow.begin", "grow.ctx", "grow.longer", "grow.adopt",
	"grow.next", "grow.step", "grow.end",
}

// CtxGrowStepOps are the operations of the growth in a context: the model declares no cost for them yet,
// so a suite that controls charges leaves them out.
var CtxGrowStepOps = []string{"grow.next", "grow.step", "grow.end"}

// CtxTestOps are the conformance operations that install growths and lookaheads.
var CtxTestOps = []string{ref.CtxGrowOp, ref.CtxReseedOp, ref.CtxUngrowOp, ref.CtxLookBeginOp, ref.CtxLookEndOp}

// CtxSupportOps are the operations that cases of the family call to make the entries, errors and
// expectations they pass: the implementation provides them besides CtxOps and CtxTestOps.
var CtxSupportOps = []string{"memo.store", "rec.mark", "rec.push", "rec.since", "exp.isolate", "exp.add",
	"exp.unisolate", "exp.copy", "exp.empty", "exp.count", "exp.keptCount", "rec.none", "rec.truncate", "rec.add", "exp.reset"}

// CtxSuiteOps returns the operations that a suite of the family provides.
func CtxSuiteOps() []string {
	return slices.Concat(CtxOps, CtxTestOps, CtxSupportOps)
}

// CtxModule returns the declarations of ExpModule with OprNodeOp and the conformance operations that
// install growths and lookaheads.
func CtxModule() *code.Module {
	m := ExpModule()
	rule := code.THandle(exec.Rule)
	m.AddOp(&code.Op{Name: OprNodeOp, Component: exec.Rules, Results: []code.Type{code.THandle(exec.Value)},
		Contract: "Returns a new handle for a conformance case."})
	i, b := code.TInt, code.TBool
	m.AddOp(&code.Op{Name: ref.CtxGrowOp, Component: exec.Growth, Writes: []string{exec.Growth},
		Params: []code.Param{code.P("r", rule), code.P("min", i), code.P("ok", b), code.P("end", i), code.P("id", i)},
		Contract: `Begins the growth of the leader r at the binding level min at the position with the seed
that ended at end, matched if ok, with the identity id.`,
		Requires: `r is a leader that does not grow at the position; a growth of another cycle at the position
is not the innermost.`})
	m.AddOp(&code.Op{Name: ref.CtxReseedOp, Component: exec.Growth, Writes: []string{exec.Growth},
		Params:   []code.Param{code.P("ok", b), code.P("end", i), code.P("id", i)},
		Contract: `Gives the innermost growth the seed that ended at end, matched if ok, with the identity id.`,
		Requires: `A growth is in progress.`})
	m.AddOp(&code.Op{Name: ref.CtxUngrowOp, Component: exec.Growth, Writes: []string{exec.Growth},
		Contract: `Ends the innermost growth, and the table of its context if it is the first growth there.`,
		Requires: `A growth is in progress, and every frame that began inside it ended.`})
	m.AddOp(&code.Op{Name: ref.CtxLookBeginOp, Component: exec.Growth, Writes: []string{exec.Growth},
		Params:   []code.Param{code.P("site", i)},
		Contract: `Begins a lookahead of the site at the position.`, Requires: `site >= 0.`})
	m.AddOp(&code.Op{Name: ref.CtxLookEndOp, Component: exec.Growth, Writes: []string{exec.Growth},
		Params: []code.Param{code.P("ok", b)},
		Contract: `Ends the innermost lookahead, which matched if ok. The case discards the errors recovered
inside it before, as a lookahead does.`,
		Requires: `Every frame that began inside the lookahead ended.`})
	return m
}

// CtxConfig returns the rules of the cases: a cycle of two leaders and a rule that is not one, a rule
// of no cycle, a second cycle, and a cycle of more leaders than a word of bits of present sets holds.
func CtxConfig() ref.Config {
	rules := []ref.CtxRule{{Cycle: 1, Leader: true}, {Cycle: 1, Leader: true}, {Cycle: 1}, {}, {Cycle: 2, Leader: true}, {Cycle: 2}}
	for range 33 {
		rules = append(rules, ref.CtxRule{Cycle: 3, Leader: true})
	}
	rules = append(rules, ref.CtxRule{Cycle: 3})
	return ref.Config{Rules: len(rules), CtxRules: rules}
}

// The rules of CtxConfig.
const (
	ctxLeaderA   = 0
	ctxLeaderB   = 1
	ctxMember    = 2
	ctxPlain     = 3
	ctxOtherLead = 4
	ctxOther     = 5
	// ctxNarrowLead is the first leader of the third cycle, which has a bit in a word of bits of present
	// sets, ctxWideLead a leader of that cycle without one, and ctxWide a rule of that cycle.
	ctxNarrowLead = 6
	// ctxEdgeLead is the leader of index 31 of the third cycle, the first that a word of 31 bits
	// leaves out.
	ctxEdgeLead = 37
	ctxWideLead = 38
	ctxWide     = 39
)

// CtxCases returns the handwritten conformance cases of the dependency memo, in one module.
func CtxCases() []Case {
	m := CtxModule()
	var cs []Case
	cfg := CtxConfig()
	add := func(name string, body func(b *code.Builder)) {
		cs = append(cs, Case{Module: m, Func: m.AddFunc(name, nil, nil, []string{"budget", "stream", "document"}, body), Config: cfg})
	}
	i, t, f := code.Int, code.BoolC(true), code.BoolC(false)
	rule := func(n int64) code.Expr { return code.C(RuleOp, i(n)) }
	eq := code.Eq
	grow := func(b *code.Builder, r, min int64, ok bool, end, id int64) {
		b.Do(ref.CtxGrowOp, rule(r), i(min), code.BoolC(ok), i(end), i(id))
	}
	enter := func(b *code.Builder, name string, r int64) (code.Expr, code.Expr) {
		cs := b.LetN([]string{name, name + "s"}, code.C("ctx.enter", rule(r), i(0)))
		return cs[0], cs[1]
	}
	noValue, noErrs := code.Null(code.THandle(exec.Value)), code.Null(code.THandle(exec.Errs))
	keep := func(b *code.Builder, c code.Expr, r, key int64, ok bool, end int64, value, errs, k code.Expr) {
		b.Do("ctx.keep", c, rule(r), i(0), i(0), code.C(ValuesOp, i(key)), code.BoolC(ok), i(end), f, value, errs, i(end), k)
	}
	// readSeed evaluates a call of the rule that reads the seed of the leader r: its result depends on it.
	readSeed := func(b *code.Builder, name string, r int64) {
		_, s := enter(b, name, r)
		b.Assert(code.C("ctx.found", s), "a leader that grows at the position reads its seed")
	}

	add("ctx_outside", func(b *code.Builder) {
		c, s := enter(b, "c", ctxPlain)
		b.Assert(code.Not(code.C("ctx.found", s)), "a rule of no cycle reads no seed")
		b.Assert(code.Not(code.C("ctx.active", c)), "and is in no context")
		b.Do("ctx.push", c)
		b.Assert(eq(code.C("ctx.pop", c), code.C("ctx.storeMemo")), "its result goes to the memo")
		b.Do("ctx.done", c)
		grow(b, ctxLeaderA, 0, true, 2, 7)
		c2, _ := enter(b, "c2", ctxOther)
		b.Assert(code.Not(code.C("ctx.active", c2)), "the growth of another cycle is no context of the call")
		b.SetReg(exec.Pos, i(1))
		c3, _ := enter(b, "c3", ctxMember)
		b.Assert(code.Not(code.C("ctx.active", c3)), "nor is a growth at another position")
	})
	add("ctx_seed", func(b *code.Builder) {
		grow(b, ctxLeaderA, 0, true, 3, 7)
		_, s := enter(b, "c", ctxLeaderA)
		b.Assert(code.C("ctx.found", s), "the leader that grows reads its seed")
		b.Assert(code.C("ctx.ok", s), "which matched")
		b.Let("v", code.C("ctx.value", s))
		b.Assert(eq(code.C("ctx.end", s), i(3)), "and ends at its end")
		b.Assert(eq(code.C("ctx.far", s), i(0)), "with no expectations")
		cb, sb := enter(b, "cb", ctxLeaderB)
		b.Assert(code.Not(code.C("ctx.found", sb)), "another leader of the cycle does not grow")
		b.Assert(code.C("ctx.active", cb), "but is in the context")
		_, s1 := enter(b, "c1", ctxLeaderA)
		b.Assert(code.Not(code.C("ctx.found", code.C("ctx.lookup", cb, rule(ctxLeaderB), i(0), code.C(ValuesOp, i(0))))),
			"nothing is kept yet")
		b.Assert(eq(code.C("ctx.end", s1), i(3)), "the seed is the same at every call")
		b.Do(ref.CtxReseedOp, f, i(0), i(8))
		b.SetReg(exec.Pos, i(0))
		_, s2 := enter(b, "c2", ctxLeaderA)
		b.Assert(code.Not(code.C("ctx.ok", s2)), "the new seed failed")
		b.Assert(eq(code.C("ctx.end", s2), i(0)), "and ends at its position")
		b.Do(ref.CtxUngrowOp)
		_, s3 := enter(b, "c3", ctxLeaderA)
		b.Assert(code.Not(code.C("ctx.found", s3)), "after the growth there is no seed")
	})
	add("ctx_keep_lookup", func(b *code.Builder) {
		grow(b, ctxLeaderA, 0, true, 3, 7)
		c, _ := enter(b, "c", ctxMember)
		b.Assert(code.C("ctx.active", c), "a rule of the cycle is in the context")
		b.Assert(code.Not(code.C("ctx.found", code.C("ctx.lookup", c, rule(ctxMember), i(0), code.C(ValuesOp, i(1))))), "nothing kept")
		b.Do("ctx.push", c)
		readSeed(b, "in", ctxLeaderA)
		b.Assert(eq(code.C("ctx.pop", c), code.C("ctx.storeCtx")), "a result that read a seed goes to the context")
		v := b.Let("v", code.C(OprNodeOp))
		b.Do("rec.push", code.C(ErrOp, i(3)))
		errs := b.Let("errs", code.C("rec.since", i(0)))
		keep(b, c, ctxMember, 1, true, 4, v, errs, code.C("exp.empty"))
		b.Do("ctx.done", c)
		c2, _ := enter(b, "c2", ctxMember)
		h := b.Let("h", code.C("ctx.lookup", c2, rule(ctxMember), i(0), code.C(ValuesOp, i(1))))
		b.Assert(code.C("ctx.found", h), "the kept result holds while the seed is the same")
		b.Assert(code.C("ctx.ok", h), "it matched")
		b.Let("hv", code.C("ctx.value", h))
		b.Assert(eq(code.C("ctx.end", h), i(4)), "it ends where it was kept")
		b.Assert(code.Not(code.C("rec.none", code.C("ctx.errs", h))), "with its errors")
		b.Assert(eq(code.C("ctx.far", h), i(4)), "its expectations are as far as kept")
		b.Assert(eq(code.C("exp.keptCount", code.C("ctx.expected", h)), i(0)), "and there are none")
		c3, _ := enter(b, "c3", ctxMember)
		b.Assert(code.Not(code.C("ctx.found", code.C("ctx.lookup", c3, rule(ctxMember), i(0), code.C(ValuesOp, i(2))))),
			"another key has no result")
		b.Do(ref.CtxReseedOp, t, i(5), i(9))
		c4, _ := enter(b, "c4", ctxMember)
		b.Assert(code.Not(code.C("ctx.found", code.C("ctx.lookup", c4, rule(ctxMember), i(0), code.C(ValuesOp, i(1))))),
			"nor a seed with another identity")
		b.Do(ref.CtxReseedOp, t, i(3), i(7))
		c5, _ := enter(b, "c5", ctxMember)
		b.Assert(code.C("ctx.found", code.C("ctx.lookup", c5, rule(ctxMember), i(0), code.C(ValuesOp, i(1)))),
			"the identity it read holds again")
	})
	add("ctx_present", func(b *code.Builder) {
		grow(b, ctxLeaderA, 0, true, 3, 7)
		c, _ := enter(b, "c", ctxMember)
		b.Do("ctx.push", c)
		b.Assert(eq(code.C("ctx.pop", c), code.C("ctx.storeMemo")), "a result that read no seed goes to the memo")
		e := b.Let("e", code.C("memo.store", i(ctxMember), i(0), i(0), code.C(ValuesOp, i(0)), t, i(1), f, noValue, noErrs, f))
		b.Do("memo.setPresent", e, c)
		b.Do("ctx.done", c)
		b.Assert(code.C("ctx.allows", c, e), "the context where it was stored reuses it")
		grow(b, ctxLeaderB, 0, false, 0, 1)
		c2, _ := enter(b, "c2", ctxMember)
		b.Assert(code.Not(code.C("ctx.allows", c2, e)), "not where another leader grows")
		b.Do(ref.CtxUngrowOp)
		c3, _ := enter(b, "c3", ctxPlain)
		b.Assert(code.C("ctx.allows", c3, e), "a call in no context reuses it")
		c4, _ := enter(b, "c4", ctxMember)
		b.Do("ctx.push", c4)
		b.Assert(eq(code.C("ctx.pop", c4), code.C("ctx.storeMemo")), "a result in both leaders' context")
		e2 := b.Let("e2", code.C("memo.store", i(ctxMember), i(0), i(0), code.C(ValuesOp, i(1)), t, i(1), f, noValue, noErrs, f))
		b.Do("memo.setPresent", e2, c4)
		b.Do("ctx.done", c4)
		b.Assert(code.C("ctx.allows", c4, e2), "is reused there")
	})
	// A lookup that hits a kept result inside another call replays the dependencies of that result into the
	// call: the call read the seed through it, so its own result goes to the context, not to the memo.
	add("ctx_keep_replay", func(b *code.Builder) {
		grow(b, ctxLeaderA, 0, true, 3, 7)
		c, _ := enter(b, "c", ctxMember)
		b.Do("ctx.push", c)
		readSeed(b, "in", ctxLeaderA)
		b.Assert(eq(code.C("ctx.pop", c), code.C("ctx.storeCtx")), "a result that read a seed goes to the context")
		keep(b, c, ctxMember, 1, true, 4, noValue, noErrs, code.C("exp.empty"))
		b.Do("ctx.done", c)
		outer, _ := enter(b, "outer", ctxMember)
		b.Do("ctx.push", outer)
		c2, _ := enter(b, "c2", ctxMember)
		h := b.Let("h", code.C("ctx.lookup", c2, rule(ctxMember), i(0), code.C(ValuesOp, i(1))))
		b.Assert(code.C("ctx.found", h), "the call finds the kept result")
		b.Assert(eq(code.C("ctx.pop", outer), code.C("ctx.storeCtx")),
			"and its own result goes to the context: it depends on the seed that the kept result read")
		keep(b, outer, ctxMember, 0, true, 4, noValue, noErrs, code.C("exp.empty"))
		b.Do("ctx.done", outer)
		b.Do(ref.CtxReseedOp, t, i(5), i(9))
		o2, _ := enter(b, "o2", ctxMember)
		b.Assert(code.Not(code.C("ctx.found", code.C("ctx.lookup", o2, rule(ctxMember), i(0), code.C(ValuesOp, i(0))))),
			"nor does it hold under a seed of another identity")
	})
	add("ctx_lookahead", func(b *code.Builder) {
		grow(b, ctxLeaderA, 0, true, 3, 7)
		c, _ := enter(b, "c", ctxMember)
		b.Do("ctx.push", c)
		b.Do(ref.CtxLookBeginOp, i(5))
		b.SetReg(exec.Silent, i(1))
		readSeed(b, "in", ctxLeaderA)
		b.Do("rec.push", code.C(ErrOp, i(2)))
		b.Do("rec.truncate", i(0))
		b.SetReg(exec.Silent, i(0))
		b.Do(ref.CtxLookEndOp, t)
		b.Assert(eq(code.C("ctx.pop", c), code.C("ctx.storeCtx")), "a result that observed a lookahead goes to the context")
		keep(b, c, ctxMember, 0, false, 0, noValue, noErrs, code.C("exp.empty"))
		b.Do("ctx.done", c)
		c2, _ := enter(b, "c2", ctxMember)
		b.Assert(code.C("ctx.found", code.C("ctx.lookup", c2, rule(ctxMember), i(0), code.C(ValuesOp, i(0)))),
			"the outcome of the lookahead holds in the context")
		b.Do(ref.CtxReseedOp, t, i(4), i(8))
		c3, _ := enter(b, "c3", ctxMember)
		b.Assert(code.Not(code.C("ctx.found", code.C("ctx.lookup", c3, rule(ctxMember), i(0), code.C(ValuesOp, i(0))))),
			"a new seed has no memoized outcome")
		// The lookahead again, with the new seed: its outcome is the same.
		b.Do("ctx.push", c3)
		b.Do(ref.CtxLookBeginOp, i(5))
		readSeed(b, "in2", ctxLeaderA)
		b.Do(ref.CtxLookEndOp, t)
		b.Assert(eq(code.C("ctx.pop", c3), code.C("ctx.storeCtx")), "observed again")
		keep(b, c3, ctxMember, 0, false, 0, noValue, noErrs, code.C("exp.empty"))
		b.Do("ctx.done", c3)
		c4, _ := enter(b, "c4", ctxMember)
		b.Assert(code.C("ctx.found", code.C("ctx.lookup", c4, rule(ctxMember), i(0), code.C(ValuesOp, i(0)))),
			"the result holds under the same outcome")
	})
	add("ctx_expectations", func(b *code.Builder) {
		mk := b.Let("mk", code.C("exp.isolate", i(0)))
		b.Do("exp.add", code.C(ExpOp, i(3)))
		b.Do("exp.add", code.C(ExpOp, i(4)))
		fs := b.LetN([]string{"far", "set"}, code.C("exp.unisolate", mk))
		k := b.Let("k", code.C("exp.copy", fs[1]))
		b.Do("exp.add", code.C(ExpOp, i(4)))
		grow(b, ctxLeaderA, 0, true, 3, 7)
		c, _ := enter(b, "c", ctxMember)
		b.Do("ctx.push", c)
		readSeed(b, "in", ctxLeaderA)
		b.Assert(eq(code.C("ctx.pop", c), code.C("ctx.storeCtx")), "kept")
		b.Do("ctx.keep", c, rule(ctxMember), i(0), i(0), code.C(ValuesOp, i(0)), t, i(2), f, noValue, noErrs, i(0), k)
		b.Do("ctx.done", c)
		c2, _ := enter(b, "c2", ctxMember)
		h := b.Let("h", code.C("ctx.lookup", c2, rule(ctxMember), i(0), code.C(ValuesOp, i(0))))
		b.Assert(eq(code.C("ctx.far", h), i(0)), "a kept result has the farthest position of its expectations")
		b.Assert(eq(code.C("exp.keptCount", code.C("ctx.expected", h)), i(2)), "and its expectations")
	})
	add("ctx_wide_edge", func(b *code.Builder) {
		grow(b, ctxEdgeLead, 0, true, 1, 7)
		c, _ := enter(b, "c", ctxWide)
		b.Do("ctx.push", c)
		b.Assert(eq(code.C("ctx.pop", c), code.C("ctx.storeCtx")), "the leader of index 31 has no bit: a word holds 31 leaders")
	})
	add("ctx_wide_present", func(b *code.Builder) {
		grow(b, ctxWideLead, 0, true, 1, 7)
		c, _ := enter(b, "c", ctxWide)
		b.Assert(code.C("ctx.active", c), "the call is in the context")
		e := b.Let("e", code.C("memo.store", i(ctxWide), i(0), i(0), code.C(ValuesOp, i(0)), t, i(1), f, noValue, noErrs, f))
		b.Assert(code.Not(code.C("ctx.allows", c, e)), "a memo entry answers no call in a context with a leader that has no bit")
		b.Do("ctx.push", c)
		b.Assert(eq(code.C("ctx.pop", c), code.C("ctx.storeCtx")), "a result that read no seed goes to the context too")
		keep(b, c, ctxWide, 0, true, 1, noValue, noErrs, code.C("exp.empty"))
		b.Do("ctx.done", c)
		c2, _ := enter(b, "c2", ctxWide)
		b.Assert(code.C("ctx.found", code.C("ctx.lookup", c2, rule(ctxWide), i(0), code.C(ValuesOp, i(0)))),
			"the context finds the result under the same present set")
		grow(b, ctxNarrowLead, 0, true, 1, 8)
		c3, _ := enter(b, "c3", ctxWide)
		b.Assert(code.Not(code.C("ctx.found", code.C("ctx.lookup", c3, rule(ctxWide), i(0), code.C(ValuesOp, i(0))))),
			"another set of leaders has another present set")
		b.Do(ref.CtxUngrowOp)
		c4, _ := enter(b, "c4", ctxWide)
		b.Assert(code.C("ctx.found", code.C("ctx.lookup", c4, rule(ctxWide), i(0), code.C(ValuesOp, i(0)))),
			"the first set names the same present set again")
		b.Do(ref.CtxUngrowOp)
		grow(b, ctxWideLead, 0, true, 1, 7)
		c5, _ := enter(b, "c5", ctxWide)
		b.Assert(code.Not(code.C("ctx.found", code.C("ctx.lookup", c5, rule(ctxWide), i(0), code.C(ValuesOp, i(0))))),
			"a new growth starts with an empty table")
	})
	// adopt adopts a step of the growth g that ends at end with the errors recovered after the mark rec,
	// as the grow composition does.
	adopt := func(b *code.Builder, g code.Expr, end int64, v code.Expr, rec code.Expr) {
		b.SetReg(exec.Pos, i(end))
		b.Assert(code.C("grow.longer", g, t), "the step matched more than the seed")
		b.Do("grow.adopt", g, v, code.C("rec.since", rec))
		b.Do("rec.truncate", rec)
	}
	// step receives the result v of a step of the growth g in a context as the grow composition does: with the
	// errors recovered after the mark rec if it succeeded, and none if it failed, and dropping them from the
	// record after.
	step := func(b *code.Builder, g, v code.Expr, ok bool, rec code.Expr) {
		if !ok {
			b.Do("rec.truncate", rec)
		}
		b.Do("grow.step", g, v, code.BoolC(ok), code.C("rec.since", rec))
		b.Do("rec.truncate", rec)
	}
	// finish ends the growth g in a context as the grow composition does: the errors of its last seed replace
	// the entries after the mark rec. It returns the result.
	finish := func(b *code.Builder, name string, g, rec code.Expr) code.Expr {
		res := b.Let(name, code.C("grow.end", g))
		b.Do("rec.truncate", rec)
		b.Do("rec.add", code.C("ctx.errs", res))
		return res
	}
	add("grow_outside", func(b *code.Builder) {
		b.SetReg(exec.Pos, i(1))
		rec := b.Let("rec", code.C("rec.mark"))
		g := b.Let("g", code.C("grow.begin", rule(ctxLeaderA), i(0)))
		b.Assert(code.Not(code.C("grow.ctx", g)), "a growth alone at its position is in no context")
		_, s := enter(b, "c", ctxLeaderA)
		b.Assert(code.C("ctx.found", s), "the leader reads its seed")
		b.Assert(code.Not(code.C("ctx.ok", s)), "which failed")
		b.Assert(eq(code.C("ctx.end", s), i(1)), "at the start of the growth")
		b.Assert(code.Not(code.C("grow.longer", g, f)), "a failed step is not longer")
		v := b.Let("v", code.C(OprNodeOp))
		b.Do("rec.push", code.C(ErrOp, i(4)))
		b.SetReg(exec.Pos, i(1))
		b.Assert(code.C("grow.longer", g, t), "an empty match is longer than a failed seed")
		adopt(b, g, 3, v, rec)
		b.Assert(eq(code.R(exec.Pos), i(1)), "the position returns to the start")
		b.Assert(eq(code.C("rec.mark"), rec), "the errors of the step are dropped from the record")
		_, s2 := enter(b, "c2", ctxLeaderA)
		b.Assert(code.C("ctx.ok", s2), "the seed is the step")
		b.Let("v2", code.C("ctx.value", s2))
		b.Assert(eq(code.C("ctx.end", s2), i(3)), "and ends where it did")
		b.Assert(code.Not(code.C("rec.none", code.C("ctx.errs", s2))), "with its errors")
		b.SetReg(exec.Pos, i(3))
		b.Assert(code.Not(code.C("grow.longer", g, t)), "a step that ends where the seed does is not longer")
		b.SetReg(exec.Pos, i(2))
		b.Assert(code.Not(code.C("grow.longer", g, t)), "nor one that ends before")
		adopt(b, g, 5, v, rec)
		_, s3 := enter(b, "c3", ctxLeaderA)
		b.Assert(eq(code.C("ctx.end", s3), i(5)), "the next step is the seed")
		b.Assert(code.C("rec.none", code.C("ctx.errs", s3)), "without errors when it recovered none")
		b.Do(ref.CtxUngrowOp)
	})
	// A growth outside a context ends with grow.end too: the position moves to the end of the last seed. The
	// case then drops the entries after the mark and records the errors of the seed with rec.truncate and
	// rec.add, as the grow composition does.
	add("grow_outside_end", func(b *code.Builder) {
		b.SetReg(exec.Pos, i(1))
		rec := b.Let("rec", code.C("rec.mark"))
		g := b.Let("g", code.C("grow.begin", rule(ctxLeaderA), i(0)))
		b.Assert(code.Not(code.C("grow.ctx", g)), "a growth alone at its position is in no context")
		v := b.Let("v", code.C(OprNodeOp))
		b.Do("rec.push", code.C(ErrOp, i(4)))
		adopt(b, g, 3, v, rec)
		b.SetReg(exec.Pos, i(3))
		b.Assert(code.Not(code.C("grow.longer", g, t)), "a step that ends where the seed does is not longer")
		res := b.Let("res", code.C("grow.end", g))
		b.Assert(eq(code.R(exec.Pos), i(3)), "the position is the end of the last seed")
		b.Do("rec.truncate", rec)
		b.Do("rec.add", code.C("ctx.errs", res))
		b.Assert(code.C("ctx.ok", res), "the last seed matched")
		b.Assert(eq(code.C("ctx.end", res), i(3)), "and ended where the step did")
		b.Assert(eq(code.C("ctx.value", res), v), "with its value")
		b.Assert(code.Not(code.C("rec.none", code.C("ctx.errs", res))), "and its errors")
		b.Assert(eq(code.C("rec.mark"), code.Add(rec, i(1))), "which are recorded as one entry")
		_, s := enter(b, "c", ctxLeaderA)
		b.Assert(code.Not(code.C("ctx.found", s)), "the growth has ended")
		// A growth with no longer step ends with its failed seed.
		b.SetReg(exec.Pos, i(0))
		g2 := b.Let("g2", code.C("grow.begin", rule(ctxLeaderA), i(0)))
		b.SetReg(exec.Pos, i(2))
		b.Assert(code.Not(code.C("grow.longer", g2, f)), "a failed step is not longer")
		res2 := b.Let("res2", code.C("grow.end", g2))
		b.Assert(eq(code.R(exec.Pos), i(0)), "the position is the start of the growth")
		b.Assert(code.Not(code.C("ctx.ok", res2)), "the seed failed")
		b.Assert(code.C("rec.none", code.C("ctx.errs", res2)), "without errors")
	})
	add("grow_context", func(b *code.Builder) {
		g := b.Let("g", code.C("grow.begin", rule(ctxLeaderA), i(0)))
		gb := b.Let("gb", code.C("grow.begin", rule(ctxLeaderB), i(0)))
		b.Assert(code.C("grow.ctx", gb), "a growth where another leader of its cycle grows is in a context")
		c, _ := enter(b, "c", ctxMember)
		b.Assert(code.C("ctx.active", c), "a call of the cycle is in their context")
		b.Do("ctx.push", c)
		readSeed(b, "in", ctxLeaderA)
		readSeed(b, "inb", ctxLeaderB)
		b.Assert(eq(code.C("ctx.pop", c), code.C("ctx.storeCtx")), "a result that read the failed seeds goes to the context")
		keep(b, c, ctxMember, 0, true, 2, code.C(OprNodeOp), noErrs, code.C("exp.empty"))
		b.Do("ctx.done", c)
		b.Do(ref.CtxUngrowOp)
		b.Assert(code.Not(code.C("grow.ctx", g)), "the first growth is in no context")
		rec := b.Let("rec", code.C("rec.mark"))
		gb2 := b.Let("gb2", code.C("grow.begin", rule(ctxLeaderB), i(0)))
		c2, _ := enter(b, "c2", ctxMember)
		b.Assert(code.C("ctx.found", code.C("ctx.lookup", c2, rule(ctxMember), i(0), code.C(ValuesOp, i(0)))),
			"every failed seed has the same identity")
		b.Assert(code.C("grow.ctx", gb2), "the growth is in the context again")
		b.Do(ref.CtxUngrowOp)
		adopt(b, g, 2, code.C(OprNodeOp), rec)
		b.Let("gb3", code.C("grow.begin", rule(ctxLeaderB), i(0)))
		c3, _ := enter(b, "c3", ctxMember)
		b.Assert(code.Not(code.C("ctx.found", code.C("ctx.lookup", c3, rule(ctxMember), i(0), code.C(ValuesOp, i(0))))),
			"an adopted seed has a new identity")
		b.Do(ref.CtxUngrowOp)
		b.Do(ref.CtxUngrowOp)
	})
	// The steps of a growth in a context come from the table of the context when another growth of the
	// leader took them at the same position: the steps are not evaluated, and the call that holds the growth
	// still has what they read and expected. Without errors the steps form a run, which a second growth
	// takes as one jump; with errors each step is taken alone.
	for _, c := range []struct {
		name   string
		errors bool
	}{{"grow_jump", false}, {"grow_memoized_steps", true}} {
		add(c.name, func(b *code.Builder) {
			b.Let("ga", code.C("grow.begin", rule(ctxLeaderA), i(0)))
			rec := b.Let("rec", code.C("rec.mark"))
			// body is a step that ends at end: it reads the seed of the growth, observes the seed of the outer growth in a lookahead, which gives
			// the same outcome under every seed of this one, and expects end.
			body := func(b *code.Builder, g code.Expr, name string, end int64, look bool) {
				// A step that reads its own seed holds for that seed only, so that each step is taken after the last.
				readSeed(b, name+"own", ctxLeaderB)
				if look {
					b.Do(ref.CtxLookBeginOp, i(5))
					readSeed(b, name, ctxLeaderA)
					b.Do(ref.CtxLookEndOp, t)
				}
				b.SetReg(exec.Pos, i(end))
				b.Do("exp.reset", i(end))
				b.Do("exp.add", code.C(ExpOp, i(end)))
				if c.errors && look {
					b.Do("rec.push", code.C(ErrOp, i(end)))
				}
				step(b, g, code.C(OprNodeOp), true, rec)
			}
			g := b.Let("g", code.C("grow.begin", rule(ctxLeaderB), i(0)))
			b.Assert(code.C("grow.ctx", g), "the growth is in the context of the outer one")
			b.Assert(code.C("grow.next", g), "the first step is evaluated")
			body(b, g, "s1", 2, true)
			b.Assert(code.C("grow.next", g), "the second step is evaluated")
			body(b, g, "s2", 3, true)
			b.Assert(code.C("grow.next", g), "so is the step from the second seed")
			body(b, g, "s3", 1, false)
			b.Assert(code.Not(code.C("grow.next", g)), "the step that is not longer ends the growth")
			res := finish(b, "res", g, rec)
			b.Assert(eq(code.C("ctx.end", res), i(3)), "it ends at the second step")
			// The second growth at the same position, inside a call.
			b.SetReg(exec.Pos, i(0))
			call, _ := enter(b, "call", ctxMember)
			b.Do("ctx.push", call)
			mk := b.Let("mk", code.C("exp.isolate", i(0)))
			rec2 := b.Let("rec2", code.C("rec.mark"))
			g2 := b.Let("g2", code.C("grow.begin", rule(ctxLeaderB), i(0)))
			b.Assert(code.Not(code.C("grow.next", g2)), "the table gives every step: none is evaluated")
			res2 := finish(b, "res2", g2, rec2)
			b.Assert(eq(code.C("ctx.end", res2), i(3)), "the growth ends at the same position")
			fs := b.LetN([]string{"far", "set"}, code.C("exp.unisolate", mk))
			b.Assert(eq(fs[0], i(3)), "the call has the expectations of the steps, which the table took")
			b.Assert(eq(code.C("exp.keptCount", code.C("exp.copy", fs[1])), i(1)), "the farthest of them")
			b.Assert(eq(code.C("ctx.pop", call), code.C("ctx.storeCtx")),
				"and what the steps read: the outer seed that their lookahead observed")
			b.Do("ctx.done", call)
			b.Do(ref.CtxUngrowOp)
		})
	}
	// A growth in a context, with the body of its steps: it reads the seed, and recovers an error.
	add("grow_steps", func(b *code.Builder) {
		b.Let("g", code.C("grow.begin", rule(ctxLeaderA), i(0)))
		rec := b.Let("rec", code.C("rec.mark"))
		gb := b.Let("gb", code.C("grow.begin", rule(ctxLeaderB), i(0)))
		b.Assert(code.C("grow.ctx", gb), "the growth is in the context of the first")
		body := func(b *code.Builder, g code.Expr, name string, end int64, ok bool, rec code.Expr) {
			readSeed(b, name, ctxLeaderB)
			b.Do("rec.push", code.C(ErrOp, i(end)))
			b.SetReg(exec.Pos, i(end))
			step(b, g, code.C(OprNodeOp), ok, rec)
		}
		b.Assert(code.C("grow.next", gb), "the first step is evaluated")
		body(b, gb, "s1", 2, true, rec)
		b.Assert(eq(code.R(exec.Pos), i(0)), "a step that matched more returns to the start")
		b.Assert(eq(code.C("rec.mark"), rec), "the errors of the step are dropped from the record")
		b.Assert(code.C("grow.next", gb), "the step from the new seed is evaluated")
		body(b, gb, "s2", 2, true, rec)
		b.Assert(code.Not(code.C("grow.next", gb)), "a step that is not longer ends the growth")
		res := finish(b, "res", gb, rec)
		b.Assert(code.C("ctx.ok", res), "the last seed matched")
		b.Assert(eq(code.C("ctx.end", res), i(2)), "and ended where the first step did")
		b.Assert(eq(code.R(exec.Pos), i(2)), "the position is its end")
		b.Assert(code.Not(code.C("rec.none", code.C("ctx.errs", res))), "its errors are recorded")
		b.Assert(eq(code.C("rec.mark"), code.Add(rec, i(1))), "as one entry after the mark")
		// The steps are in the table of the context: another growth of the leader takes them without the body.
		b.SetReg(exec.Pos, i(0))
		rec2 := b.Let("rec2", code.C("rec.mark"))
		gb2 := b.Let("gb2", code.C("grow.begin", rule(ctxLeaderB), i(0)))
		b.Assert(code.Not(code.C("grow.next", gb2)), "a growth that the table gives its steps evaluates none")
		res2 := finish(b, "res2", gb2, rec2)
		b.Assert(eq(code.C("ctx.end", res2), i(2)), "and ends at the same position")
		// A step that failed holds no errors, and its errors are dropped from the record all the same.
		b.Do(ref.CtxUngrowOp)
		rec3 := b.Let("rec3", code.C("rec.mark"))
		b.Let("g3", code.C("grow.begin", rule(ctxLeaderA), i(0)))
		gb3 := b.Let("gb3", code.C("grow.begin", rule(ctxLeaderB), i(0)))
		b.Assert(code.C("grow.next", gb3), "a step is evaluated")
		body(b, gb3, "s3", 1, false, rec3)
		b.Assert(eq(code.C("rec.mark"), rec3), "a step that failed leaves no errors in the record")
		b.Assert(code.Not(code.C("grow.next", gb3)), "and ends the growth")
		res3 := finish(b, "res3", gb3, rec3)
		b.Assert(code.Not(code.C("ctx.ok", res3)), "which failed")
		b.Assert(code.C("rec.none", code.C("ctx.errs", res3)), "without errors")
		b.Do(ref.CtxUngrowOp)
	})
	// The call of a leader in a context keeps the last result of its growth, which grow.end returns.
	add("grow_keep", func(b *code.Builder) {
		b.Let("g", code.C("grow.begin", rule(ctxLeaderA), i(0)))
		rec := b.Let("rec", code.C("rec.mark"))
		c, cs := enter(b, "c", ctxLeaderB)
		b.Assert(code.Not(code.C("ctx.found", cs)), "a leader that does not grow reads no seed")
		b.Do("ctx.push", c)
		gb := b.Let("gb", code.C("grow.begin", rule(ctxLeaderB), i(0)))
		b.Assert(code.C("grow.next", gb), "the step is evaluated")
		readSeed(b, "in", ctxLeaderA)
		b.SetReg(exec.Pos, i(2))
		step(b, gb, code.C(OprNodeOp), true, rec)
		b.Assert(code.Not(code.C("grow.next", gb)), "the step from the new seed is the memoized one, which is not longer")
		res := finish(b, "res", gb, rec)
		b.Assert(eq(code.C("ctx.end", res), i(2)), "the growth ends at the first step")
		far := b.Let("far", code.C("ctx.far", res))
		b.Assert(eq(code.C("ctx.pop", c), code.C("ctx.storeCtx")), "a result that read the seed of another growth goes to the context")
		keep(b, c, ctxLeaderB, 0, true, 2, code.C(OprNodeOp), noErrs, code.C("exp.empty"))
		b.Assert(eq(code.C("ctx.far", res), far), "the keep of a leader's result does not change the result that grow.end returned")
		b.Do("ctx.done", c)
		b.Do(ref.CtxUngrowOp)
	})
	// Two results of steps with the same end and success share a seed identity only if they are equal.
	add("grow_canonical", func(b *code.Builder) {
		b.Let("g", code.C("grow.begin", rule(ctxLeaderA), i(0)))
		rec := b.Let("rec", code.C("rec.mark"))
		v1, v2 := code.Null(code.THandle(exec.Value)), b.Let("v2", code.C(OprNodeOp))
		take := func(b *code.Builder, name string, v code.Expr) code.Expr {
			gb := b.Let("g"+name, code.C("grow.begin", rule(ctxLeaderB), i(0)))
			b.Assert(code.C("grow.next", gb), "the step is evaluated")
			readSeed(b, name, ctxLeaderA)
			b.SetReg(exec.Pos, i(3))
			step(b, gb, v, true, rec)
			b.Assert(code.Not(code.C("grow.next", gb)), "the step from the new seed is the memoized one, which is not longer")
			return finish(b, "res"+name, gb, rec)
		}
		take(b, "1", v1)
		b.Do(ref.CtxReseedOp, t, i(1), i(5))
		b.SetReg(exec.Pos, i(0))
		r2 := take(b, "2", v2)
		b.Assert(eq(code.C("ctx.value", r2), v2), "a step with a value, unlike one without, has its own result, though it ends where the first did")
		b.Do(ref.CtxUngrowOp)
	})
	add("grow_wide", func(b *code.Builder) {
		b.Let("g", code.C("grow.begin", rule(ctxWideLead), i(0)))
		g2 := b.Let("g2", code.C("grow.begin", rule(ctxNarrowLead), i(0)))
		b.Assert(code.C("grow.ctx", g2), "the growth names a present set with a leader that has no bit")
		b.Do(ref.CtxUngrowOp)
		b.Do(ref.CtxUngrowOp)
	})
	return cs
}

// ctxEntry is a handle that a randomized case made, with the model's mirror of it.
type ctxEntry struct {
	expr code.Expr
	h    any
}

// RandomCtx returns a randomized case of the dependency memo with about n calls, drawn with rng, that
// meets every precondition: growths, calls and lookaheads nest as the call protocol nests them, and
// a call stores its result where ctx.pop sends it. The positions, keys and identities come from small
// domains, so that results are found again.
func RandomCtx(name string, rng *rand.Rand, n int) Case {
	m := CtxModule()
	cfg := CtxConfig()
	mirror := ref.New(cfg)
	regs := map[string]any{exec.Pos: int64(0), exec.Silent: int64(0)}
	mirror.UseRegisters(regs)
	f := m.AddFunc(name, nil, nil, []string{"budget", "stream", "document"}, func(b *code.Builder) {
		count := 0
		fresh := func() string { count++; return fmt.Sprintf("x%d", count) }
		var values, errs, kept, entries []ctxEntry
		// call runs the operation on the mirror and emits it; it returns the mirror's results and the
		// expressions of the case's, or false if the call breaks the precondition.
		call := func(opName string, args []any, exprs ...code.Expr) ([]any, []code.Expr, bool) {
			o := m.Op(opName)
			if !mirror.Allows(o, args) {
				return nil, nil, false
			}
			res := mirror.Call(o, args)
			switch len(o.Results) {
			case 0:
				b.Do(opName, exprs...)
				return nil, nil, true
			case 1:
				return res, []code.Expr{b.Let(fresh(), code.C(opName, exprs...))}, true
			}
			names := make([]string, len(o.Results))
			for k := range names {
				names[k] = fresh()
			}
			return res, b.LetN(names, code.C(opName, exprs...)), true
		}
		setReg := func(reg string, v int64) {
			regs[reg] = v
			b.SetReg(reg, code.Int(v))
		}
		pick := func(es []ctxEntry) ctxEntry { return es[rng.IntN(len(es))] }
		ruleArg := func(r int64) (any, code.Expr) { return ref.RuleRef{Index: r}, code.C(RuleOp, code.Int(r)) }

		// The handles the calls pass.
		for range 2 {
			values = append(values, ctxEntry{b.Let(fresh(), code.C(OprNodeOp)), mirror.OprHandle(OprNodeOp)})
		}
		values = append(values, ctxEntry{code.Null(code.THandle(exec.Value)), nil})
		errs = append(errs, ctxEntry{code.Null(code.THandle(exec.Errs)), nil})
		for k := range 2 {
			e := rng.Int64N(20)
			call("rec.push", []any{&ref.Err{ID: e}}, code.C(ErrOp, code.Int(e)))
			res, ex, _ := call("rec.since", []any{int64(k)}, code.Int(int64(k)))
			errs = append(errs, ctxEntry{ex[0], res[0]})
		}
		res, ex, _ := call("exp.empty", nil)
		kept = append(kept, ctxEntry{ex[0], res[0]})
		mk, mkx, _ := call("exp.isolate", []any{int64(0)}, code.Int(0))
		for _, id := range []int64{3, 4} {
			call("exp.add", []any{ref.ExpID{ID: id}}, code.C(ExpOp, code.Int(id)))
		}
		set, setx, _ := call("exp.unisolate", []any{mk[0]}, mkx[0])
		res, ex, _ = call("exp.copy", []any{set[1]}, setx[1])
		kept = append(kept, ctxEntry{ex[0], res[0]})

		// The rules that calls draw from, by cycle: the cycles of two leaders and of many.
		cycles := [][]int64{{ctxLeaderA, ctxLeaderB, ctxMember, ctxPlain}, {ctxOtherLead, ctxOther}, {ctxWideLead, ctxNarrowLead, ctxWide}}
		cycle := cycles[0]
		if rng.IntN(4) == 0 {
			cycle = cycles[1+rng.IntN(2)]
		}
		leaders := func() []int64 {
			var ls []int64
			for _, r := range cycle {
				if r != ctxMember && r != ctxPlain && r != ctxOther && r != ctxWide {
					ls = append(ls, r)
				}
			}
			return ls
		}()
		ids := func() int64 { return 1 + rng.Int64N(3) }
		pos := func() int64 {
			if rng.IntN(5) == 0 {
				return 1
			}
			return 0
		}

		var items func(depth int, inGrowth bool)
		var callBlock func(depth int, r, key int64)
		// callBlock is a memoized call of r with the key as the call protocol makes it.
		callBlock = func(depth int, r, key int64) {
			start := regs[exec.Pos].(int64)
			ra, rx := ruleArg(r)
			cs, cx, _ := call("ctx.enter", []any{ra, int64(0)}, rx, code.Int(0))
			c, cExpr := cs[0], cx[0]
			if res, _, _ := call("ctx.found", []any{cs[1]}, cx[1]); res[0].(bool) {
				call("ctx.ok", []any{cs[1]}, cx[1])
				call("ctx.value", []any{cs[1]}, cx[1])
				call("ctx.end", []any{cs[1]}, cx[1])
				call("ctx.errs", []any{cs[1]}, cx[1])
				setReg(exec.Pos, start)
				return
			}
			// An entry that another store replaced is no longer a handle.
			entries = slices.DeleteFunc(entries, func(e ctxEntry) bool { return mirror.Dead(e.h) })
			if len(entries) > 0 && rng.IntN(2) == 0 {
				e := pick(entries)
				call("ctx.allows", []any{c, e.h}, cExpr, e.expr)
			}
			if res, _, _ := call("ctx.active", []any{c}, cExpr); res[0].(bool) {
				h, hx, _ := call("ctx.lookup", []any{c, ra, int64(0), key}, cExpr, rx, code.Int(0), code.C(ValuesOp, code.Int(key)))
				if res, _, _ := call("ctx.found", []any{h[0]}, hx[0]); res[0].(bool) {
					call("ctx.ok", []any{h[0]}, hx[0])
					call("ctx.value", []any{h[0]}, hx[0])
					call("ctx.end", []any{h[0]}, hx[0])
					call("ctx.errs", []any{h[0]}, hx[0])
					call("ctx.far", []any{h[0]}, hx[0])
					k, kx, _ := call("ctx.expected", []any{h[0]}, hx[0])
					call("exp.keptCount", []any{k[0]}, kx[0])
					setReg(exec.Pos, start)
					return
				}
			}
			call("ctx.push", []any{c}, cExpr)
			if rng.IntN(2) == 0 {
				// A read of a seed, so that the result goes to the context.
				callBlock(depth+1, leaders[rng.IntN(len(leaders))], 0)
			}
			items(depth+1, false)
			setReg(exec.Pos, start)
			res, _, _ := call("ctx.pop", []any{c}, cExpr)
			call("ctx.storeMemo", nil)
			call("ctx.storeCtx", nil)
			ok, end := rng.IntN(3) > 0, start+rng.Int64N(4)
			switch store := res[0].(int64); {
			case store == ref.CtxStoreCtx && !cfg.CtxRules[r].Leader:
				v, er, k := pick(values), pick(errs), pick(kept)
				silent := regs[exec.Silent].(int64) > 0
				call("ctx.keep", []any{c, ra, start, int64(0), key, ok, end, silent, v.h, er.h, end, k.h},
					cExpr, rx, code.Int(start), code.Int(0), code.C(ValuesOp, code.Int(key)), code.BoolC(ok), code.Int(end),
					code.BoolC(silent), v.expr, er.expr, code.Int(end), k.expr)
			case store == ref.CtxStoreMemo && rng.IntN(2) == 0:
				res, ex, _ := call("memo.store", []any{r, start, int64(0), key, ok, end, false, nil, nil, false},
					code.Int(r), code.Int(start), code.Int(0), code.C(ValuesOp, code.Int(key)), code.BoolC(ok), code.Int(end),
					code.BoolC(false), code.Null(code.THandle(exec.Value)), code.Null(code.THandle(exec.Errs)), code.BoolC(false))
				call("memo.setPresent", []any{res[0], c}, ex[0], cExpr)
				entries = append(entries, ctxEntry{ex[0], res[0]})
			}
			call("ctx.done", []any{c}, cExpr)
		}
		// growPos is the position of the innermost growth in progress, or -1: a growth begun there is in a
		// context, which the steps of the growths in a context need.
		growPos := int64(-1)
		// takeStep receives a step of the growth g in a context as the composition does, with the errors it
		// recovered after the mark of the growth if it succeeded, which the step then drops from the record.
		takeStep := func(g any, gx code.Expr, v ctxEntry, ok bool, mark []any, markx []code.Expr) {
			if !ok {
				call("rec.truncate", []any{mark[0]}, markx[0])
			}
			es, ex, _ := call("rec.since", []any{mark[0]}, markx[0])
			call("grow.step", []any{g, v.h, ok, es[0]}, gx, v.expr, code.BoolC(ok), ex[0])
			call("rec.truncate", []any{mark[0]}, markx[0])
		}
		items = func(depth int, inGrowth bool) {
			for k := rng.IntN(4); k >= 0 && count < n; k-- {
				switch x := rng.IntN(10); {
				case x < 2 && depth < 4:
					// A growth at a position, with its seeds.
					setReg(exec.Pos, pos())
					r := leaders[rng.IntN(len(leaders))]
					ra, rx := ruleArg(r)
					ok, end, id := rng.IntN(2) == 0, regs[exec.Pos].(int64)+rng.Int64N(3), ids()
					if _, _, allowed := call(ref.CtxGrowOp, []any{ra, int64(0), ok, end, id}, rx, code.Int(0), code.BoolC(ok), code.Int(end), code.Int(id)); !allowed {
						continue
					}
					outer := growPos
					growPos = regs[exec.Pos].(int64)
					items(depth+1, true)
					growPos = outer
					call(ref.CtxUngrowOp, nil)
				case (x < 3 || (inGrowth || growPos >= 0) && x < 4) && depth < 4 && (inGrowth || growPos >= 0 || rng.IntN(2) == 0):
					// A growth that grow.begin begins, with its steps as the grow composition takes them:
					// in a context through the table of the context, otherwise outside one.
					if growPos >= 0 && rng.IntN(5) > 0 {
						setReg(exec.Pos, growPos)
					} else {
						setReg(exec.Pos, pos())
					}
					start := regs[exec.Pos].(int64)
					leader := leaders[rng.IntN(len(leaders))]
					ra, rx := ruleArg(leader)
					mark, markx, _ := call("rec.mark", nil)
					gs, gx, allowed := call("grow.begin", []any{ra, int64(0)}, rx, code.Int(0))
					if !allowed {
						continue
					}
					outerPos := growPos
					growPos = start
					if res, _, _ := call("grow.ctx", []any{gs[0]}, gx[0]); res[0].(bool) {
						// The steps of a growth in a context: the body reads seeds and recovers errors, and the
						// growth ends when a step is not longer or the table gives no more steps.
						for range 1 + rng.IntN(4) {
							if res, _, _ := call("grow.next", []any{gs[0]}, gx[0]); !res[0].(bool) {
								break
							}
							items(depth+1, false)
							if rng.IntN(2) == 0 {
								e := rng.Int64N(20)
								call("rec.push", []any{&ref.Err{ID: e}}, code.C(ErrOp, code.Int(e)))
							}
							setReg(exec.Pos, start+rng.Int64N(4))
							ok := rng.IntN(4) > 0
							v := pick(values)
							takeStep(gs[0], gx[0], v, ok, mark, markx)
						}
						// A growth that the loop left is ended after its steps are taken: finish it as the
						// composition does, with the steps the table still gives.
						for {
							if res, _, _ := call("grow.next", []any{gs[0]}, gx[0]); !res[0].(bool) {
								break
							}
							items(depth+1, false)
							setReg(exec.Pos, start+rng.Int64N(4))
							v, ok := pick(values), rng.IntN(4) > 0
							takeStep(gs[0], gx[0], v, ok, mark, markx)
						}
						done, dx, _ := call("grow.end", []any{gs[0]}, gx[0])
						call("rec.truncate", []any{mark[0]}, markx[0])
						call("ctx.ok", []any{done[0]}, dx[0])
						call("ctx.value", []any{done[0]}, dx[0])
						call("ctx.end", []any{done[0]}, dx[0])
						es, ex, _ := call("ctx.errs", []any{done[0]}, dx[0])
						call("rec.add", []any{es[0]}, ex[0])
						growPos = outerPos
						continue
					}
					for range 1 + rng.IntN(3) {
						items(depth+1, false)
						if rng.IntN(2) == 0 {
							e := rng.Int64N(20)
							call("rec.push", []any{&ref.Err{ID: e}}, code.C(ErrOp, code.Int(e)))
						}
						setReg(exec.Pos, start+rng.Int64N(4))
						ok := rng.IntN(4) > 0
						if res, _, _ := call("grow.longer", []any{gs[0], ok}, gx[0], code.BoolC(ok)); !res[0].(bool) {
							break
						}
						es, ex, _ := call("rec.since", []any{mark[0]}, markx[0])
						v := pick(values)
						call("grow.adopt", []any{gs[0], v.h, es[0]}, gx[0], v.expr, ex[0])
						call("rec.truncate", []any{mark[0]}, markx[0])
						// The next step reads the seed it adopted.
						callBlock(depth+1, leader, 0)
					}
					call("rec.truncate", []any{mark[0]}, markx[0])
					// The growth ends with grow.end, which moves the position; the case then drops the entries
					// after the mark and records the errors of the seed, as the composition does.
					done, dx, _ := call("grow.end", []any{gs[0]}, gx[0])
					call("rec.truncate", []any{mark[0]}, markx[0])
					call("ctx.ok", []any{done[0]}, dx[0])
					call("ctx.value", []any{done[0]}, dx[0])
					call("ctx.end", []any{done[0]}, dx[0])
					es, ex, _ := call("ctx.errs", []any{done[0]}, dx[0])
					call("rec.add", []any{es[0]}, ex[0])
					setReg(exec.Pos, start)
					growPos = outerPos
				case x < 3 && inGrowth && rng.IntN(2) == 0:
					ok, end, id := rng.IntN(2) == 0, regs[exec.Pos].(int64)+rng.Int64N(3), ids()
					call(ref.CtxReseedOp, []any{ok, end, id}, code.BoolC(ok), code.Int(end), code.Int(id))
				case x < 5 && depth < 5:
					site := rng.Int64N(3)
					mark, markx, _ := call("rec.mark", nil)
					call(ref.CtxLookBeginOp, []any{site}, code.Int(site))
					silent := regs[exec.Silent].(int64)
					setReg(exec.Silent, silent+1)
					items(depth+1, false)
					if rng.IntN(3) == 0 {
						e := rng.Int64N(20)
						call("rec.push", []any{&ref.Err{ID: e}}, code.C(ErrOp, code.Int(e)))
					}
					call("rec.truncate", []any{mark[0]}, markx[0])
					setReg(exec.Silent, silent)
					ok := rng.IntN(2) == 0
					call(ref.CtxLookEndOp, []any{ok}, code.BoolC(ok))
				case depth < 6:
					r, key := cycle[rng.IntN(len(cycle))], rng.Int64N(2)
					callBlock(depth, r, key)
					if rng.IntN(2) == 0 {
						// The same call again, which may reuse the result of the first.
						callBlock(depth, r, key)
					}
				}
			}
		}
		for count < n {
			items(0, false)
		}
	})
	return Case{Module: m, Func: f, Config: cfg}
}
