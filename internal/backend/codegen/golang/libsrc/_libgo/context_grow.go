package libgo

import "slices"

// The steps of the growth of a left-recursion leader in a context, where another leader of its
// cycle grows at the same position: the steps are memoized in the table of the context, with the
// jumps over runs of steps that read no other seed. The engine and the generated parsers share them;
// each package begins, adopts outside a context and ends a growth itself (growth is its state).

// growMemoStep takes the steps of a growth in a context that its table gives, from the jumps and the
// memoized steps, replaying their dependencies and merging their expectations, and adopts each step
// that grew. It returns the first memoized step that did not grow, or nil when the table keeps no
// step from the seed.
func (p *Parser) growMemoStep(g *growth) *ctxRes {
	if p.ctxMut(ctxMutNoStepMemo) {
		return nil
	}
	for {
		id := p.gamma[g.gi].id
		if j := p.findJump(g, id); j != nil {
			if !p.ctxMut(ctxMutNoJumpSeedRead) {
				p.seedRead(g.gp)
			}
			if !p.ctxMut(ctxMutNoJumpReplayDeps) {
				p.replayDeps(g.start, g.gp, g.present, j.deps)
			}
			if !p.ctxMut(ctxMutNoJumpMergeExp) {
				p.MergeExpected(j.far, j.exp)
			}
			// The step after the jump reads the seed it lands on, whose examined range is the jump's
			// (recordJumps), so this touch only keeps the jump a complete replay of its steps.
			p.touchRange(j.examined, j.from)
			p.setSeed(g, j.end, j.id)
			continue
		}
		k := g.growKey()
		h := ctxHeadKey{kind: ctxStep, id: k.rule, min: k.min, ms: k.ms, present: g.present}
		s := p.ctxFind(g.start, g.gp, h, nil)
		if s == nil || !p.reusable(s.silent) {
			return nil
		}
		if !p.ctxMut(ctxMutNoStepSeedRead) {
			p.seedRead(g.gp)
		}
		if !p.ctxMut(ctxMutNoStepReplayDeps) {
			p.replayDeps(g.start, g.gp, g.present, s.deps)
		}
		if !p.ctxMut(ctxMutNoStepMergeExp) {
			p.MergeExpected(s.far, s.exp)
		}
		// A step taken from the context records what it examined, like every other reuse: the last
		// step of a growth, which does not grow, can be taken from the context, and nothing else
		// records how far it examined.
		p.touchRange(s.examined, s.from)
		if !p.growGrew(g, s.e) {
			return s.e
		}
		p.adoptGrownStep(g, s.e, s.id, runStep{seed: id, far: s.far, exp: s.exp, examined: s.examined, from: s.from, silent: s.silent, deps: s.deps})
	}
}

// growIsolate starts evaluating a step of a growth in a context, with its own frame, examined range
// and expectations.
func (p *Parser) growIsolate(g *growth) {
	g.evaluated = true
	p.pushFrame(g.start, g.gp)
	g.savedExamined, g.savedFrom = p.examRange()
	p.setExamRange(g.start, g.start)
	g.exp = p.Isolate(g.start)
}

// growStepEnd ends the evaluation of a step of a growth in a context that growIsolate began, restoring
// the examined range and the expectations around it, and returns the dependencies that the step
// recorded and what else it recorded (its deps are left to growKeep).
func (p *Parser) growStepEnd(g *growth) ([]dep, runStep) {
	id := p.gamma[g.gi].id
	_, ds := p.popFrame()
	far, inner := p.Unisolate(g.exp)
	exp := p.Keep(inner)
	examined, from := p.examRange()
	p.MergeExpected(far, inner)
	p.setExamRange(max(g.savedExamined, examined), min(g.savedFrom, from))
	return ds, runStep{seed: id, far: far, exp: exp, examined: examined, from: from, silent: p.Silent > 0}
}

// growKeep keeps the result res of the step that growStepEnd ended, with the dependencies ds and the
// record st that it returned, in the table of the context, and returns the result that stands for it
// (canonicalSeed). It holds the step until growAdoptStep adopts it.
func (p *Parser) growKeep(g *growth, res *ctxRes, ds []dep, st runStep) *ctxRes {
	k := g.growKey()
	res, rid := p.canonicalSeed(g.start, g.gp, k, res)
	sd, _ := p.stored(ds)
	h := ctxHeadKey{kind: ctxStep, id: k.rule, min: k.min, ms: k.ms, present: g.present}
	p.ctxStore(g.start, g.gp, h, sd, &ctxSlot{e: res, id: rid, far: st.far, exp: st.exp, examined: st.examined, from: st.from, silent: st.silent})
	p.mergeDeps(ds)
	st.deps = sd
	g.pendID, g.pendStep = rid, st
	return res
}

// growGrew reports whether the step s of a growth in a context matched more than the seed.
func (p *Parser) growGrew(g *growth, s *ctxRes) bool {
	best := p.gamma[g.gi].seed
	return s.succeeded() && (!best.succeeded() || s.resultEnd() > best.resultEnd())
}

// growAdoptStep adopts the step s of a growth in a context that grew, which growKeep returned last, as
// the seed.
func (p *Parser) growAdoptStep(g *growth, s *ctxRes) {
	p.adoptGrownStep(g, s, g.pendID, g.pendStep)
}

// adoptGrownStep adopts the step s of a growth in a context that grew, with identity id and what it
// recorded st, as the seed, and returns to the start of the growth. It extends the run of steps that
// read no other seed with the step, or ends it.
func (p *Parser) adoptGrownStep(g *growth, s *ctxRes, id uint64, st runStep) {
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
	if !other && s.errFree() {
		st.deps = obs
		c := cap(g.run)
		g.run = append(g.run, st)
		p.useMem(uint64(cap(g.run)-c)*runStepBytes + uint64(cap(obs))*sdepBytes)
	} else {
		p.recordJumps(g)
	}
	p.Spend()
	p.setSeed(g, s, id)
	p.Pos = g.start
}

// recordJumps records, for each seed of the run, a jump to the current seed with what the steps
// from that seed on recorded, and starts a new run.
func (p *Parser) recordJumps(g *growth) {
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
			if d.kind == depObs && !slices.Contains(j.deps, d) {
				continue next
			}
		}
		return true
	}
	return false
}

// findJump returns a jump from the seed with identity id that holds in the current context.
func (p *Parser) findJump(g *growth, id uint64) *jumpRec {
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
			if d.kind != depObs || p.ctxMut(ctxMutIgnoreObs) {
				continue
			}
			if v, free := p.obsFree(g.start, g.gp, g.present, d.site, g.growKey()); v != d.val || !free && !p.ctxMut(ctxMutJumpAnySeed) {
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
		if !slices.Contains(out, d) {
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
		if !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}
