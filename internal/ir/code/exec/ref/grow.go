package ref

import (
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
)

// The reference model of the steps of a growth in a context (grow.memoStep, grow.isolate, grow.keepStep,
// grow.grew and grow.adoptStep) and of the end of a growth (grow.end), on the state of ctx.go.
//
// A step of a growth in a context is memoized in the table of its context under the identity of the seed it
// started from, so that another growth at the position takes it without evaluating the body. The model takes
// every step one at a time; an implementation may also take several at once (a jump) in grow.memoStep
// where the steps since a seed read no other seed, which has the same effects: the same seed, the same
// dependencies and expectations passed on, the same position. The results of equal steps share one
// identity as a seed (canonSeed). Two results are equal when they succeeded, end at the same position, hold
// no errors and have equal values. Implementations compare the structure of the values, and the cases make
// only two kinds of value, the null value and the nodes of OprNodeOp, which are equal to one another, so the
// model compares whether a value is null. It does not model the examined range, which an implementation may
// compare too.

// The model keeps no cost: a charge for these operations is not declared as a quantity of the model yet.
// TODO(#310): declare it when the charges count logical events.

// canonKey is what the results of equal steps share.
type canonKey struct {
	rule, min int64
	end       int64
	ok        bool
}

type canonRec struct {
	res *CtxResult
	id  uint64
}

// allowsGrow reports whether a call of an operation of the steps of a growth in a context or of grow.end
// meets its precondition.
func (m *Model) allowsGrow(o *code.Op, args []any) (allowed, handled bool) {
	s := &m.ctx
	g, _ := args[0].(*GrowState)
	switch o.Name {
	case "grow.memoStep", "grow.isolate":
		ok := s.innermost(g) && g.gp < g.gi && !g.evaluating && g.pending == nil
		if o.Name == "grow.isolate" {
			ok = ok && g.missed
		}
		return ok, true
	case "grow.keepStep":
		return s.innermost(g) && g.gp < g.gi && g.evaluating && len(s.frames) == g.frame &&
			len(m.exp.marks) > 0 && m.exp.marks[len(m.exp.marks)-1] == g.exp, true
	case "grow.grew":
		r := ctxRes(args[1])
		return r != nil && r == g.pending, true
	case "grow.adoptStep":
		r := ctxRes(args[1])
		return r != nil && r == g.pending && s.innermost(g) && m.grew(g, r), true
	case "grow.end":
		return s.innermost(g) && !g.evaluating, true
	}
	return false, false
}

// callGrow runs an operation of the steps of a growth in a context or grow.end, and reports false for
// another operation.
func (m *Model) callGrow(o *code.Op, args []any) ([]any, bool) {
	s := &m.ctx
	switch o.Name {
	case "grow.memoStep":
		g := args[0].(*GrowState)
		k := s.growths[g.gi].key
		h := ctxHead{step: true, id: k.rule, min: k.min, present: g.present}
		for {
			slot, _ := s.find(g.start, g.gp, h, 0)
			if slot == nil || slot.res.silent && !m.silent() {
				g.missed = true
				return []any{nil}, true
			}
			s.replayDeps(g.start, g.gp, g.present, slot.deps)
			m.mergeKeptExpectations(slot.res.far, slot.res.kept)
			if !m.grew(g, slot.res) {
				g.missed = false
				return []any{slot.res}, true
			}
			m.adoptStep(g, slot.res, slot.id)
		}
	case "grow.isolate":
		g := args[0].(*GrowState)
		// The step has its own frame and expectation record.
		s.frames = append(s.frames, ctxFrame{pos: g.start, gp: g.gp, depth: len(s.growths)})
		res, _ := m.callExp(&code.Op{Name: "exp.isolate"}, []any{g.start})
		g.evaluating, g.missed, g.frame, g.exp = true, false, len(s.frames), res[0].(*ExpMark)
		return nil, true
	case "grow.keepStep":
		g := args[0].(*GrowState)
		ok := args[2].(bool)
		_, ds := s.popFrame()
		unisolated, _ := m.callExp(&code.Op{Name: "exp.unisolate"}, []any{g.exp})
		far, set := unisolated[0].(int64), unisolated[1].(*ExpSet)
		kept := keptOf(set.ids)
		m.mergeKeptExpectations(far, kept)
		g.evaluating, g.exp = false, nil
		res := &CtxResult{ok: ok, end: m.pos(), value: args[1], silent: m.silent(), far: far, kept: kept,
			examined: m.doc.examined, from: m.doc.low}
		if ok {
			res.errs = args[3]
		} else {
			res.end = g.start
		}
		res, rid := s.canonSeed(g, s.growths[g.gi].key, res)
		sd, _ := s.stored(ds)
		h := ctxHead{step: true, id: s.growths[g.gi].key.rule, min: s.growths[g.gi].key.min, present: g.present}
		s.store(g.start, g.gp, h, sd, &ctxSlot{res: res, id: rid})
		s.mergeDeps(ds)
		g.pending, g.pendingID = res, rid
		return []any{res}, true
	case "grow.grew":
		g := args[0].(*GrowState)
		grew := m.grew(g, ctxRes(args[1]))
		if !grew {
			// The growth ends: no step of it is pending any more.
			g.pending = nil
		}
		return []any{grew}, true
	case "grow.adoptStep":
		g := args[0].(*GrowState)
		m.adoptStep(g, g.pending, g.pendingID)
		g.pending = nil
		return nil, true
	case "grow.end":
		g := args[0].(*GrowState)
		rec := s.growths[g.gi]
		best := rec.seed
		s.dropGrowth(g.gi)
		res := &CtxResult{ok: best.ok, end: best.end, value: best.value, errs: best.errs, silent: m.silent(),
			examined: m.doc.examined, from: m.doc.low}
		m.regs[exec.Pos] = best.end
		s.grown, s.grownRule = res, rec.key.rule
		return []any{res}, true
	}
	return nil, false
}

// adoptStep adopts the step res of the growth g, with identity rid, as its seed, and returns to the
// start of the growth.
func (m *Model) adoptStep(g *GrowState, res *CtxResult, rid uint64) {
	rec := &m.ctx.growths[g.gi]
	rec.seed, rec.id = res, rid
	m.regs[exec.Pos] = g.start
}

// grew reports whether the step res of the growth g matched more than its seed.
func (m *Model) grew(g *GrowState, res *CtxResult) bool {
	seed := m.ctx.growths[g.gi].seed
	return res.ok && (!seed.ok || res.end > seed.end)
}

// canonSeed returns the result of a step with its identity as a seed: the result and identity of an equal
// result of a step of the growth that the table of the context holds, or res with a new identity.
func (s *ctxModel) canonSeed(g *GrowState, k growKey, res *CtxResult) (*CtxResult, uint64) {
	s.seeds++
	id := failSeed + s.seeds
	if l, _ := res.errs.(*ErrList); !res.ok || l != nil && l.n > 0 {
		return res, id
	}
	t := s.table(g.start, g.gp, true)
	if t.canon == nil {
		t.canon = map[canonKey]canonRec{}
	}
	ck := canonKey{rule: k.rule, min: k.min, end: res.end, ok: res.ok}
	if c, ok := t.canon[ck]; ok && (c.res.value == nil) == (res.value == nil) {
		return c.res, c.id
	}
	t.canon[ck] = canonRec{res: res, id: id}
	return res, id
}

// dropGrowth ends the growth i: its table, which only its steps use, and the growth itself.
func (s *ctxModel) dropGrowth(i int) {
	if n := len(s.tables); n > 0 && s.tables[n-1].pos == s.growths[i].pos && s.tables[n-1].depth == i {
		s.tables = s.tables[:n-1]
	}
	s.growths = s.growths[:i]
	if i == 0 {
		clear(s.wide)
	}
}

// keptOf returns the set kept for the entries ids, which is the empty set for none.
func keptOf(ids []ExpID) *ExpKept {
	if len(ids) == 0 {
		return emptyKept
	}
	return &ExpKept{ids: slices.Clone(ids)}
}

// mergeKeptExpectations records the expectations ids at far in the current record, as expect does for each: one
// past the farthest position starts the record anew, one at it joins the entries, and one in a lookahead
// or before it is dropped.
func (m *Model) mergeKeptExpectations(far int64, k *ExpKept) {
	s := &m.exp
	for _, id := range k.ids {
		if m.silent() || far < s.cur.far {
			continue
		}
		if far > s.cur.far {
			s.cur = expRecord{far: far}
		}
		if m.cfg.NoExpectations {
			if len(s.cur.ids) == 0 {
				s.write(0)
				s.cur.ids = append(s.cur.ids, Placeholder)
			}
			continue
		}
		if !slices.Contains(s.cur.ids, id) {
			s.write(len(s.cur.ids))
			s.cur.ids = append(slices.Clip(s.cur.ids), id)
		}
	}
}
