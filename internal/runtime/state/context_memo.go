package state

import (
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/program"
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
	idx   int32 // Rule.LeaderIndex
	cycle int32 // Rule.Cycle
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

// ctxRes is a result that a context keeps: a seed, the result of a growth step, or the result of a
// call that read a seed. The engine keeps memo entries.
type ctxRes = MemoEntry

// succeeded reports whether the result matched.
func (e *MemoEntry) succeeded() bool { return e.Ok }

// resultEnd returns the end of the result.
func (e *MemoEntry) resultEnd() int { return e.End }

// errFree reports whether the result recovered no errors.
func (e *MemoEntry) errFree() bool { return e.Errs.None() }

// examinedEnd and examinedFrom return the range of input that the result examined.
func (e *MemoEntry) examinedEnd() int  { return e.Examined }
func (e *MemoEntry) examinedFrom() int { return e.From }

// rangeKept reports whether two results that examined different ranges differ: in a Document parse
// (MemoAll), whose reuse after an edit depends on the ranges, and in a parse with
// ContextMemoExamined, which tests check the dependency memo with. Other parses, observed ones
// included, compare growing results and outcomes without their ranges (spec/runtime/parsing.md,
// Evaluations of Rules).
func (p *Parser) rangeKept() bool { return p.MemoAll || p.ContextMemoExamined }

// sameResult reports whether two results are equal, comparing at most *budget pairs of nodes
// (nodesEqual).
func (p *Parser) sameResult(a, b *MemoEntry, budget *int) bool {
	return p.nodesEqual(a.Node, b.Node, budget)
}

// gkey returns the key of the growth's leader.
func (g *growRec) gkey() growKey {
	return growKey{rule: int32(g.key.Rule), min: int32(g.key.Min), ms: g.key.MissingSilent}
}

// at returns the position of the growth.
func (g *growRec) at() int { return g.key.Pos }

// maxLeaderBits is the number of leaders of a cycle that have a bit in present sets
// (program.MaxLeaderBits).
const maxLeaderBits = program.MaxLeaderBits

// presentSet is a present set: the bits of the leaders of a context (growRec.leaderBit), or
// presentWide and an identity. The engine holds it in a word of 64 bits, as MemoEntry.Present does;
// the values are those of the Go library's sets of 32 bits.
type presentSet = uint64

// missingBit is the missing-silent bit of a key (MemoKey.MissingSilent): the key of a seed of a
// lookahead where missing-token insertion is off. The Go library inserts no tokens and gives the
// bit a type that takes no space.
type missingBit = bool

// recordsIn reports whether evaluations in a context with the present set present record their
// dependencies, so that their results are kept.
func (p *Parser) recordsIn(present presentSet) bool {
	return !p.ContextMemoOff && (p.tune.MemoMut != CtxNarrowPresent || present&presentWide == 0)
}

// seedRead counts a read of the seed of a growth, or a reuse of a result that depends on seeds, of
// the growths from gamma[i] on.
func (p *Parser) seedRead(i int) {
	p.SeedReads++
	if p.tune.RunMut == RunOpenGrowthsOnly {
		p.seedReadLog = append(p.seedReadLog, i)
	}
}

// LookBegin starts a lookahead of the given site (lookBegin).
func (p *Parser) LookBegin(site int) { p.lookBegin(site) }

// LookEnd ends the innermost lookahead, whose expression matched if ok (lookEnd). It discards the
// errors recovered and the tokens inserted inside the lookahead, whether it matched or not
// (lookRestore).
func (p *Parser) LookEnd(ok bool) { p.lookEnd(ok) }

// lookMark is what the end of a lookahead restores: the recovered errors and the inserted tokens at
// its start.
type lookMark struct{ rec, missing int }

// lookMark returns the marks of a lookahead that starts now.
func (p *Parser) lookMark() lookMark { return lookMark{rec: len(p.Recovered), missing: p.MissingLen} }

// lookRestore drops the errors recovered and the tokens inserted after the marks m.
func (p *Parser) lookRestore(m lookMark) {
	if p.MissingLen > m.missing {
		p.ResetMissing(m.missing)
	}
	p.Recovered = p.Recovered[:m.rec]
}

// examRange returns the examined range of the current evaluation.
func (p *Parser) examRange() (examined, from int) { return p.Hw, p.lw }

// setExamRange sets the examined range of the current evaluation.
func (p *Parser) setExamRange(examined, from int) { p.Hw, p.lw = examined, from }

// samePositionBasis reports whether the positions of the nodes a and b account for the same edits
// of a Document (Data.Mark), so that equal positions are the same positions: the engine moves the
// nodes of a reused result lazily (MoveTree), and a node holds the positions of the text of its Mark.
func (p *Parser) samePositionBasis(a, b *value.Node) bool { return nodes.D(a).Mark == nodes.D(b).Mark }
