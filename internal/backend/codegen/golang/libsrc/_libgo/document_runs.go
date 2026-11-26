package libgo

import (
	"slices"
	"unsafe"

	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// Resuming repetitions in a Document, as the engine does (spec/runtime/documents.md, "Reuse Model").
//
// A Document parse records the run of each repetition that may resume (flow.resumeStart): for each
// element, its positions, the input it examined, its expectations and its value. When the repetition
// runs again, the printed compositions of the runs (call_exec.go, from internal/ir/code/exec/compose)
// decide which recorded elements it reuses, as they decide it for the engine, so that both reuse the
// same elements and count the same units. A recorded value is nil or a node of the value model, whose
// kinds are the string, tuple, list and error nodes of the grammar and the struct, tuple, list and
// basic values that an action builds (such as an int or a bool), so every recorded value can move.

// runKey is a repetition site and the position of a run of it.
type runKey struct{ site, start int }

// runRecord is a run of a repetition in the parse of the generation gen.
type runRecord struct {
	gen   uint32
	elems []runElem
}

// runElem is an element of a recorded run.
type runElem struct {
	start, end     int
	from, examined int // the input range [from, examined) the element examined
	far            int // the position of the expectations exp
	exp            []ExpID
	v              *sxirvalue.Node
}

// RunState is a run of a repetition in progress in a Document parse. Its exported fields are the
// fields of the Run record of the Exec IR (exec.RunFields), which the printed compositions read and
// write; the rest is the library's own.
//
// RunState is for generated parsers; not a stable API.
type RunState struct {
	Min, Max                      int
	Shiftable, Done, Moved        bool
	Prefixed                      bool
	Count                         int
	EditStart, EditEnd, EditDelta int
	// The run is old[:K], then the new elements mid, then old[J:J+T] (the elements after the edit,
	// moved), then the new elements post.
	K, J, T int
	// The search for the old element to resync at resumes at Seek; the run resyncs at none before
	// Barred (runResync).
	Seek, Barred            int
	ElemStart               int
	OuterExamined, OuterLow int
	Mark                    ExpMark
	key                     runKey
	old                     []runElem // the run that this one takes over, updated in place
	mid, post               []runElem
	rec0, seedReads0        int
	reached0                int // the insertion sites reached at the start (missingState.reached)
}

// ResumeOn reports whether a repetition resumes its run (flow.resumeOn): in a Document parse, outside
// a lookahead.
//
// ResumeOn is for generated parsers; not a stable API.
func (p *Parser) ResumeOn() bool { return p.runs != nil && p.Silent == 0 }

// ResumeStart starts the run of the repetition site at the position, with at least min and at most
// max elements (max -1 for no bound) (flow.resumeStart). shiftable tells whether the values of its
// elements hold no position values.
//
// ResumeStart is for generated parsers; not a stable API.
func (p *Parser) ResumeStart(site, min, max int, shiftable bool) *RunState {
	var r *RunState
	if n := len(p.freeRuns); n > 0 {
		r = p.freeRuns[n-1]
		p.freeRuns = p.freeRuns[:n-1]
	} else {
		r = new(RunState)
	}
	p.runStart(r, site, min, max, shiftable)
	return r
}

// ResumeFinish records the run r of a repetition that matched (flow.resumeFinish), and keeps r for the
// next run that starts.
//
// ResumeFinish is for generated parsers; not a stable API.
func (p *Parser) ResumeFinish(r *RunState) {
	p.runFinish(r)
	clear(r.mid)
	clear(r.post)
	r.old = nil
	p.freeRuns = append(p.freeRuns, r)
}

// runInit is run.init: it starts the run r of the site at the position, keeping the capacity of its
// lists of new elements.
func (p *Parser) runInit(r *RunState, site, min, max int, shiftable bool) {
	const maxInt = int(^uint(0) >> 1)
	*r = RunState{key: runKey{site, p.Pos}, Min: min, Max: max, Shiftable: shiftable, J: -1,
		EditStart: maxInt, EditEnd: maxInt, rec0: len(p.Recovered), seedReads0: p.seedReads,
		reached0: p.missingReached(), mid: r.mid[:0], post: r.post[:0]}
}

// runTakeHere is run.takeHere.
func (p *Parser) runTakeHere(r *RunState) bool {
	rec := p.runs[r.key]
	if rec == nil {
		return false
	}
	r.old = rec.elems
	return true
}

// runTakeLast is run.takeLast.
func (p *Parser) runTakeLast(r *RunState, at, gen int) bool {
	key := runKey{r.key.site, at}
	rec := p.lastRuns[key]
	if rec == nil || int(rec.gen) != gen {
		return false
	}
	r.old = rec.elems
	delete(p.lastRuns, key)
	return true
}

// runTake is run.take: it moves the old element i of r, its value made in the generation gen, by
// shift, as documentMove moves the value of a memo entry.
func (p *Parser) runTake(r *RunState, i, shift, gen int) *sxirvalue.Node {
	el := &r.old[i]
	if shift != 0 {
		el.v = p.moveValue(el.v, shift, uint32(gen))
		el.start += shift
		el.end += shift
		el.from += shift
		el.examined += shift
		el.far += shift
	}
	return el.v
}

// runAdd is run.add.
func (p *Parser) runAdd(r *RunState, after bool, start, end, from, examined, far int, exp []ExpID, v *sxirvalue.Node) {
	el := runElem{start: start, end: end, from: from, examined: examined, far: far, exp: exp, v: v}
	if after {
		r.post = append(r.post, el)
	} else {
		r.mid = append(r.mid, el)
	}
}

// runClean is run.clean: the run recovered no error, read no seed of a growth, directly or by
// reusing a result that depends on one, and reached no insertion site. An element that reached one
// depends on the insertions before it and takes the attempts of the limit, both of which a later
// parse has anew, even where it recovered no error: its insertion backtracked, or the site had
// inserted at the position already.
func (p *Parser) runClean(r *RunState) bool {
	return len(p.Recovered) == r.rec0 && p.seedReads == r.seedReads0 && p.missingReached() == r.reached0
}

// runRecord is run.record.
func (p *Parser) runRecord(r *RunState) {
	var elems []runElem
	if r.J < 0 {
		elems = append(r.old[:r.K], r.mid...)
	} else {
		elems = append(slices.Replace(r.old[:r.J+r.T], r.K, r.J, r.mid...), r.post...)
	}
	p.runs[r.key] = &runRecord{gen: p.gen, elems: elems}
}

// seedRead counts a read of the seed of a growth, or a reuse of a result that depends on seeds, of
// the growths from gamma[i] on: a run in which one happens is not recorded (runClean). The engine
// also logs i for a test; the library needs only the count.
func (p *Parser) seedRead(int) { p.seedReads++ }

// runBytes returns the bytes of the run records runs, as a parse under a memory limit counts them.
func runBytes(runs map[runKey]*runRecord) uint64 {
	var n uint64
	for _, run := range runs {
		n += uint64(unsafe.Sizeof(runRecord{})) + uint64(cap(run.elems))*uint64(unsafe.Sizeof(runElem{}))
	}
	return n
}
