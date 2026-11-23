package state

import (
	"slices"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// Resuming repetitions in a Document.
//
// A Document reparse evaluates again the rules whose results span the edit, and a long repetition
// in one of them (the lines of a file, the statements of a block) ran again element by element:
// every element but the edited one was a memo hit, but a hit per element is still work in
// proportion to the length of the repetition. During Document parses, such a repetition records
// its run: for each element, its positions, the input range it examined, its value and the
// expectations it recorded. When the repetition runs again after one edit, at the same position
// or the one the edit moved it to, it reuses the elements that examined only input before the edit
// as they are, and parses from there; once an element ends where an old element after the edit
// began (moved by the edit), it reuses the rest of the old run too, moving the nodes of their
// values (moveValue). The result,
// the expectations and the examined range are those of running every element.
//
// An element's result must depend only on the input it examined, as for a memo result: repetitions
// whose elements have predicates or call rules that read variables are not resumed, nor is a run
// that recovered errors, reached a #missing insertion site (reachedInsertionIn) or read the seed of
// a growing left recursion, even of a growth that started and ended within the run (readSeedIn).
// Elements are moved past an edit only if their values contain no position values.

// RunSite is a repetition that can be resumed (compiler.repeat).
type RunSite struct {
	Id              int
	M               Matcher
	Scope           *program.Scope
	OwnScope, Build bool
	Min, Max        int
	Shiftable       bool // the elements' values contain no position values
	// Value makes an element's value from the value of the element expression.
	Value func(p *Parser, v *value.Node, start int) *value.Node
}

type RunKey struct{ Site, start int }

// RunRecord is a run of a repetition in the parse of the generation gen.
type RunRecord struct {
	gen   uint32
	Elems []RunElem
}

type RunElem struct {
	Start, end     int
	From, examined int // the input range [from, examined) the element examined
	far            int // the position of the expectations exp
	exp            []ExpID
	V              *value.Node
}

// RunState is a run of a repetition in progress in a Document parse (ResumeRepeat, and the REPEAT
// instruction of the VMs). The printed compositions of the runs (run_exec.go, from
// internal/ir/code/exec/compose) read and write its exported fields, which are the fields of the
// Run record of the Exec IR (exec.RunFields); the rest is the engine's own.
type RunState struct {
	Min, Max                      int
	Shiftable, Done, Moved        bool
	Prefixed                      bool
	Count                         int
	EditStart, EditEnd, EditDelta int
	// The run is Old[:K], then the new elements Mid, then Old[J:J+T] (the elements after the edit,
	// moved), then the new elements Post.
	K, J, T int
	// The search for the old element to resync at resumes at Seek; the run resyncs at none before
	// Barred (runResync).
	Seek, Barred            int
	ElemStart               int
	OuterExamined, OuterLow int
	Mark                    ExpMark
	Key                     RunKey
	Old                     []RunElem // the run that this one takes over, updated in place
	Mid, Post               []RunElem
	rec0, seedReads0        int
	reached0                int // the insertion sites reached at the start (MissingTokenState.Reached)
	gamma0, log0            int // for RunOpenGrowthsOnly: the growths open and the reads logged at the start
}

// RunInit is run.init: it starts the run r of the site at the position, keeping the capacity of its
// lists of new elements.
func (p *Parser) RunInit(r *RunState, site, min, max int, shiftable bool) {
	const maxInt = int(^uint(0) >> 1)
	*r = RunState{Key: RunKey{site, p.Pos}, Min: min, Max: max, Shiftable: shiftable, J: -1,
		EditStart: maxInt, EditEnd: maxInt, rec0: len(p.Recovered), seedReads0: p.SeedReads,
		gamma0: len(p.gamma), log0: len(p.seedReadLog), Mid: r.Mid[:0], Post: r.Post[:0]}
	if p.Missing != nil {
		r.reached0 = p.Missing.Reached
	}
}

// runTakeHere is run.takeHere.
func (p *Parser) runTakeHere(r *RunState) bool {
	rec := p.Runs[r.Key]
	if rec == nil {
		return false
	}
	r.Old = rec.Elems
	return true
}

// runTakeLast is run.takeLast.
func (p *Parser) runTakeLast(r *RunState, at, gen int) bool {
	key := RunKey{r.Key.Site, at}
	rec := p.LastRuns[key]
	if rec == nil || int(rec.gen) != gen {
		return false
	}
	r.Old = rec.Elems
	delete(p.LastRuns, key)
	return true
}

// runTake is run.take: it moves the old element i of r by shift, its value made in the generation gen.
func (p *Parser) runTake(r *RunState, i, shift, gen int) *value.Node {
	el := &r.Old[i]
	if shift != 0 {
		el.V = p.moveValue(el.V, shift, uint32(gen))
		el.Start += shift
		el.end += shift
		el.From += shift
		el.examined += shift
		el.far += shift
	}
	return el.V
}

// runAdd is run.add.
func (p *Parser) runAdd(r *RunState, after bool, start, end, from, examined, far int, exp []ExpID, v *value.Node) {
	el := RunElem{Start: start, end: end, From: from, examined: examined, far: far, exp: exp, V: v}
	if after {
		r.Post = append(r.Post, el)
	} else {
		r.Mid = append(r.Mid, el)
	}
}

// runClean is run.clean.
func (p *Parser) runClean(r *RunState) bool {
	return len(p.Recovered) == r.rec0 && !p.readSeedIn(r) && !p.reachedInsertionIn(r)
}

// RunIgnoresInsertions records a run whose elements reached an insertion site (Tuning.RunMut).
const RunIgnoresInsertions = 2

// reachedInsertionIn reports whether an element of the run r reached an insertion site or replayed
// an insertion. Such an element depends on the insertions before it and takes the attempts of the
// limit, both of which a later parse has anew, even where it recovered no error: its insertion
// backtracked, or the site had inserted at the position already.
func (p *Parser) reachedInsertionIn(r *RunState) bool {
	return p.Missing != nil && p.Missing.Reached != r.reached0 && p.tune.RunMut != RunIgnoresInsertions
}

// runRecord is run.record.
func (p *Parser) runRecord(r *RunState) {
	var elems []RunElem
	if r.J < 0 {
		elems = append(r.Old[:r.K], r.Mid...)
	} else {
		elems = append(slices.Replace(r.Old[:r.J+r.T], r.K, r.J, r.Mid...), r.Post...)
	}
	p.Runs[r.Key] = &RunRecord{gen: p.Gen, Elems: elems}
}

// RunOpenGrowthsOnly records a run that read only seeds of growths that started and ended within it.
const RunOpenGrowthsOnly = 1

// readSeedIn reports whether a call of the run r read the seed of a growth, directly or by reusing
// a result that depends on one, including a growth that started and ended within the run: an
// iteration that grew a leader where it started and read its seed would, resumed at the position of
// an open growth of that leader, stand for an evaluation that reads that growth.
func (p *Parser) readSeedIn(r *RunState) bool {
	if p.tune.RunMut == RunOpenGrowthsOnly {
		for _, i := range p.seedReadLog[r.log0:] {
			if i < r.gamma0 {
				return true
			}
		}
		return false
	}
	return p.SeedReads != r.seedReads0
}

// ResumeRepeat runs the repetition rs (see compiler.repeat) in a Document parse, resuming its run
// from the previous parse when it can (the compositions of run_exec.go). An iteration is no event
// (spec/runtime/parsing.md#compute-units): what an evaluated element matches and builds counts, and
// the elements that the run reuses count nothing.
func (p *Parser) ResumeRepeat(rs *RunSite) (*value.Node, bool) {
	start := p.Pos
	var r RunState
	p.RunStart(&r, rs.Id, rs.Min, rs.Max, rs.Shiftable)
	base := len(p.KidStack)
	if rs.Build {
		p.KidStack = slices.Grow(p.KidStack, len(r.Old))
	}
	for p.RunMore(&r) {
		if v, ok := p.RunNext(&r); ok {
			if rs.Build {
				p.KidStack = append(p.KidStack, v)
			}
			continue
		}
		m0 := p.Mark()
		prevCut, prevFrame := p.Cut, p.Frame
		p.Cut = false
		var f *Frame
		if rs.OwnScope {
			f = p.NewFrame(len(rs.Scope.Names))
			p.Frame = f
		}
		p.RunBegin(&r)
		v, ok := rs.M(p)
		if ok && rs.Build {
			v = rs.Value(p, v, m0.Pos)
			if rs.OwnScope {
				v = p.AttachCaptures(v, rs.Scope, f, m0.Pos, p.Pos)
			}
		}
		p.RunEnd(&r, ok, v)
		cut := p.Cut
		p.Cut, p.Frame = prevCut, prevFrame
		if !ok {
			p.Reset(m0)
			if cut {
				p.TruncateKids(base)
				return nil, false
			}
			break
		}
		if rs.Build {
			p.KidStack = append(p.KidStack, v)
		}
		if p.Pos == m0.Pos {
			break
		}
	}
	if r.Count < rs.Min {
		p.TruncateKids(base)
		return nil, false
	}
	p.RunFinish(&r)
	if !rs.Build {
		return nil, true
	}
	return p.NewNode(nodes.Data{Kind: valueimpl.ListKind, Start: int32(start), End: int32(p.Pos), Children: p.Kids(base), Fresh: true}), true
}

// ReleaseRunLists clears the element lists of a finished run for reuse.
func (p *Parser) ReleaseRunLists(r *RunState) {
	clear(r.Mid)
	clear(r.Post)
}
