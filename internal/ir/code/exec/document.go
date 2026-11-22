package exec

import (
	"strings"

	"syntaxir.dev/sxir/internal/ir/code"
)

// QMoved is the size of the value of the entry that the call moves: for each distinct node in it, one
// and the number of its children and fields.
const QMoved = "moved"

// QMoveEdits is the edits that the call replays to learn how far a node moves that another result
// moved since the entry's value was made: for each generation of the value's nodes other than the
// entry's, the edits since that generation. It is zero for a target that keeps no generations.
const QMoveEdits = "move_edits"

// Names of the handle types of the operations of Document edits and runs.
const (
	// EditLog is the edits of a Document, in order.
	EditLog = "EditLog"
	// Run is a repetition run in progress in a Document parse.
	Run = "Run"
)

// Runs is the repetition runs of a Document: for each repetition of the last parse that a parse may
// resume, the elements it matched.
const Runs = "runs"

// documentMove adds to m the host operation that moves the value of a memo entry that an edit of a
// Document moved (memo.splice). The positions in a value lie outside the parser VM's state, so the
// operation is checked against the charges of the target that implements it, not against a model.
func documentMove(m *code.Module) {
	entry := code.THandle(Entry)
	m.AddOp(&code.Op{Name: "doc.move", Component: Memo, Params: []code.Param{code.P("e", entry)},
		Results: []code.Type{code.TBool}, Reads: []string{Memo, Host}, Writes: []string{Memo},
		Features: []string{Document}, Kind: code.OpHost, NoReturn: true,
		Cost: code.Cost{Terms: []code.CostTerm{{Coef: 2, Quantity: QMoved}, {Coef: 1, Quantity: QMoveEdits}}},
		Contract: `Makes the value of entry e valid at the position of e, after memo.splice moved e: moves the
positions in the value by as much as the edits moved e since the value was made. A target that may
not change a value that the parse returned moves a copy; one whose contract says that a reuse moves
the nodes of earlier results moves them in place, where each node records how many edits its
positions account for. Returns false, changing nothing, when the target cannot move the value (a
value of the host that is not made of nodes and of basic values); the call that finds e then evaluates
the rule again. Returns true for an entry that no edit moved.
The cost has two parts. Each pass over the value charges, for each distinct node it visits, one unit
and one for each child and field. A target that copies checks the value and then copies it, in two
passes, and a refusal charges no more than the check of the nodes it reached. A target that moves in
place makes one pass, charges a second unit for each reference to an empty node, which it copies,
and for each copy it forgets at the end, and replays the edits since the generation of a node that
another result moved, once for each generation.`})
}

// documentEdits adds to m the host operations of the edits that a target applies to a memo entry when
// it finds the entry: the engine and the generated Go library apply an edit at once only to the entries
// at its positions. They are what such an implementation of memo.splice reads and writes, and have no
// reference model: a lazy edit changes no result that a composition could observe, so conformance
// checks them through memo.splice and memo.lookup.
func documentEdits(m *code.Module) {
	entry := code.THandle(Entry)
	log := m.AddHandle(EditLog, Memo, "The edits of a Document, in order.")
	host := func(o *code.Op) {
		o.Kind = code.OpHost
		o.Features = []string{Document}
		m.AddOp(o)
	}
	attribute := func(name string, t code.Type, doc string) {
		host(&code.Op{Name: name, Component: Memo, Params: []code.Param{code.P("e", entry)}, Results: []code.Type{t},
			Reads: []string{Memo}, Contract: doc})
	}
	attribute("memo.seed", code.TBool, `Reports whether entry e is the seed of a growth, which no edit keeps. It guards memoAdvance: a seed
lives within one parse, when no edit is pending, so the guard is not reached in a parse; only a case of
conformance, which stores seeds without a parse, reaches it.`)
	attribute("memo.from", code.TInt, `Returns the start of the input that entry e examined, as of the edits it accounts for.`)
	attribute("memo.pinned", code.TBool, `Reports whether a call pinned entry e (memo.pin).`)
	attribute("memo.shifted", code.TBool, `Reports whether an edit moved entry e since its value was last made valid (doc.move).`)
	attribute("memo.applied", code.TInt, `Returns the number of edits that entry e accounts for.`)
	host(&code.Op{Name: "memo.advance", Component: Memo, Writes: []string{Memo},
		Params: []code.Param{code.P("e", entry), code.P("from", code.TInt), code.P("examined", code.TInt), code.P("shift", code.TInt),
			code.P("shifted", code.TBool), code.P("applied", code.TInt)},
		Contract: `Records that entry e accounts for the first applied edits: its examined input is [from, examined),
its position, its end and the far position of its expectations move by shift, the shift of its value
grows by shift, and shifted tells whether its value has to move.`})
	count := []code.Param{code.P("log", log)}
	at := []code.Param{code.P("log", log), code.P("i", code.TInt)}
	host(&code.Op{Name: "edit.count", Component: Memo, Params: count, Results: []code.Type{code.TInt}, Reads: []string{Memo},
		Contract: `Returns the number of edits of the log.`})
	for _, a := range [][2]string{{"edit.start", "Returns the start of the input that edit i replaced."},
		{"edit.end", "Returns the end of the input that edit i replaced."},
		{"edit.delta", "Returns how many units longer the replacement of edit i is than the input it replaced."}} {
		host(&code.Op{Name: a[0], Component: Memo, Params: at, Results: []code.Type{code.TInt}, Reads: []string{Memo},
			Contract: a[1], Requires: `0 <= i < edit.count(log).`})
	}
}

// RunField is a field of the record of a run in progress (handle Run), which the operation run.<name>
// reads and run.set<Name> writes.
type RunField struct {
	Name string
	Type code.Type
	Doc  string
}

// RunFields are the fields of the record of a run in progress. A target holds them as fields of its
// record of the run, under the names with the first letter upper-cased where its language needs that
// for access from the printed functions.
func RunFields() []RunField {
	f := func(n string, t code.Type, doc string) RunField { return RunField{n, t, doc} }
	return []RunField{
		f("min", code.TInt, "The least number of elements of the repetition."),
		f("max", code.TInt, "The greatest number of elements of the repetition, or -1 for no bound."),
		f("shiftable", code.TBool, "Whether the values of the elements may move with an edit (no position in them)."),
		f("count", code.TInt, "The number of elements of the run so far, reused or evaluated."),
		f("done", code.TBool, "Whether a reused element ended the repetition: it was empty, or the run reached max."),
		f("moved", code.TBool, "Whether the old run is the one that the edit moved here: none of it lies before the edit."),
		f("editStart", code.TInt, "The start of the input that the one edit since the old run replaced; the greatest int when the old run is of this text."),
		f("editEnd", code.TInt, "The end of the input that the edit replaced; the greatest int when the old run is of this text."),
		f("editDelta", code.TInt, "How many units longer the replacement of the edit is than what it replaced; 0 when the old run is of this text."),
		f("prefixed", code.TBool, "Whether the run is past its reused prefix (runNext)."),
		f("k", code.TInt, "The number of old elements before the edit that the run reused."),
		f("j", code.TInt, "The index of the first old element after the edit that the run reuses, or -1 before the run is back in step."),
		f("t", code.TInt, "The number of old elements from j on that the run reused."),
		f("seek", code.TInt, "The old element at which the search for the old element after the edit that the run resyncs at resumes (runResync)."),
		f("barred", code.TInt, "The number of old elements that the run cannot resync at: one past the last old element that the scan found to examine input before the end of the edit (runResync)."),
		f("elemStart", code.TInt, "Where the element being evaluated started."),
		f("outerExamined", code.TInt, "The examined end of the enclosing call, set aside while an element is evaluated (doc.enter)."),
		f("outerLow", code.TInt, "The examined start of the enclosing call, set aside while an element is evaluated (doc.enter)."),
		f("mark", code.THandle(Mark), "The isolation of the expectations of the element being evaluated (exp.isolate)."),
	}
}

// documentRuns adds to m the host operations of the repetition runs of a Document (spec/runtime/documents.md,
// "Runs"). A backend calls the compositions of the runs (runStart, runMore, runNext, runBegin, runEnd,
// runFinish) where a repetition of a Document parse starts, reuses elements and evaluates its
// element expression, which it runs itself, so that no operation runs grammar code. The operations hold
// the records of runs and their elements; which record a run takes over, which elements it reuses, and
// what it records are decided by the compositions. They charge nothing: what a run reuses is not an
// event of the parse, and the compositions charge the work of the Document in its free regions.
func documentRuns(m *code.Module, value code.Type) {
	m.AddComponent(Runs, `The repetition runs of a Document: for each repetition of the last parse that a parse may
resume, the positions, the examined input, the value and the expectations of its elements; and the runs
recorded so far in this parse.`)
	run := m.AddHandle(Run, Runs, "A repetition run in progress in a Document parse.")
	r := code.P("r", run)
	i := code.P("i", code.TInt)
	host := func(o *code.Op) {
		o.Kind = code.OpHost
		o.Component = Runs
		if o.Reads == nil {
			o.Reads = []string{Runs}
		}
		o.Features = []string{Document}
		m.AddOp(o)
	}
	host(&code.Op{Name: "run.init", Writes: []string{Runs},
		Params: []code.Param{r, code.P("site", code.TInt), code.P("min", code.TInt), code.P("max", code.TInt), code.P("shiftable", code.TBool)},
		Reads:  []string{Runs, Pos, Rec},
		Contract: `Starts the run r of the repetition site at the position: its fields are the zero values except min, max
and shiftable, j is -1, editStart and editEnd are the greatest int; it has no old run and no new
elements. It notes the recovered errors and the reads of growing results so far, for run.clean.`})
	host(&code.Op{Name: "run.takeHere", Params: []code.Param{r}, Results: []code.Type{code.TBool}, Writes: []string{Runs},
		Contract: `Takes over as the old run of r the record that this parse holds for the site of r at its start, and
reports whether there is one. The records that this parse holds are those it made, and those of the parse
before when no edit came since.`})
	host(&code.Op{Name: "run.previous", Results: []code.Type{code.TBool},
		Contract: `Reports whether the parse may take over the records of the parse before: an edit came since it, and
its records were kept.`})
	host(&code.Op{Name: "run.takeLast", Params: []code.Param{r, code.P("at", code.TInt), code.P("gen", code.TInt)},
		Results: []code.Type{code.TBool}, Writes: []string{Runs},
		Contract: `Takes over as the old run of r the record of the parse before for the site of r at the position at,
if that parse recorded it when the Document had had gen edits, removes it from the records of that parse,
and reports whether it did.`})
	host(&code.Op{Name: "run.oldCount", Params: []code.Param{r}, Results: []code.Type{code.TInt},
		Contract: `Returns the number of elements of the old run of r, 0 if it has none.`})
	for _, a := range [][2]string{{"run.oldStart", "where it started"}, {"run.oldEnd", "where it ended"},
		{"run.oldFrom", "the start of the input it examined"}, {"run.oldExamined", "the end of the input it examined, exclusive"},
		{"run.oldFar", "the position of the expectations it recorded"}} {
		host(&code.Op{Name: a[0], Params: []code.Param{r, i}, Results: []code.Type{code.TInt},
			Requires: `0 <= i < run.oldCount(r).`, Contract: `Returns, for the old element i of r, ` + a[1] + `, as of the edits that r took up to now.`})
	}
	host(&code.Op{Name: "run.oldKept", Params: []code.Param{r, i}, Results: []code.Type{code.THandle(Kept)},
		Requires: `0 <= i < run.oldCount(r).`, Contract: `Returns the expectations that the old element i of r recorded at run.oldFar(r, i).`})
	host(&code.Op{Name: "run.take", Params: []code.Param{r, i, code.P("shift", code.TInt), code.P("gen", code.TInt)},
		Results: []code.Type{value}, Reads: []string{Runs, Host}, Writes: []string{Runs},
		Requires: `0 <= i < run.oldCount(r); shift is 0 unless r is shiftable.`,
		Contract: `Moves the old element i of r by shift: its positions, examined input and far position, and the nodes of
its value, which the parse that recorded it made when the Document had had gen edits (doc.move). Returns
its value. With shift 0 it changes nothing.`})
	host(&code.Op{Name: "run.resumed", Writes: []string{Runs},
		Contract: `Counts an element that the parse reused from a run, in the statistics of the Document (as doc.reused)
and in its count of resumed elements.`})
	host(&code.Op{Name: "run.add", Writes: []string{Runs},
		Params: []code.Param{r, code.P("after", code.TBool), code.P("start", code.TInt), code.P("end", code.TInt), code.P("from", code.TInt),
			code.P("examined", code.TInt), code.P("far", code.TInt), code.P("kept", code.THandle(Kept)), code.P("v", value)},
		Contract: `Adds to r an element that the parse evaluated: from start to end, having examined [from, examined),
with the expectations kept at far and the value v. It belongs to the new elements before the old elements
after the edit that r reuses, or, with after, to those after them.`})
	host(&code.Op{Name: "run.clean", Params: []code.Param{r}, Results: []code.Type{code.TBool}, Reads: []string{Runs, Rec},
		Contract: `Reports whether, since run.init(r), no recovered error remains and no call read the current result of
a growth, directly or by reusing a result that read one, even of a growth that started and ended within
the run.`})
	host(&code.Op{Name: "run.minimum", Results: []code.Type{code.TInt},
		Contract: `Returns the least number of elements of a run that a Document records, MinRecordedRun outside tests.`})
	host(&code.Op{Name: "run.record", Params: []code.Param{r}, Writes: []string{Runs},
		Contract: `Records in this parse the run r for its site and start, replacing any record there, with the generation
of the Document: the old elements before k, the new elements before the old elements after the edit, the
old elements from j to j + t when j >= 0, and the new elements after them.`})
	host(&code.Op{Name: "doc.log", Results: []code.Type{code.THandle(EditLog)}, Reads: []string{Memo},
		Contract: `Returns the log of the edits of the Document since its memo was last dropped; the parse is of the text
after them all.`})
	for _, f := range RunFields() {
		host(&code.Op{Name: "run." + f.Name, Params: []code.Param{r}, Results: []code.Type{f.Type},
			Contract: `Returns the field ` + f.Name + ` of the run r: ` + f.Doc})
		host(&code.Op{Name: "run.set" + strings.ToUpper(f.Name[:1]) + f.Name[1:], Params: []code.Param{r, code.P("x", f.Type)},
			Writes: []string{Runs}, Contract: `Sets the field ` + f.Name + ` of the run r to x.`})
	}
}

// PendingEditLimit is the number of edits that a memo entry may lag behind when a lookup finds it. A
// lookup applies the edits it does not account for to the entry, one step for each; past this limit
// the entry is dropped and the call evaluates again, whose cost is that of the work the entry stands
// for, so the work of applying edits stays within a constant factor of the work a parse from
// scratch does. Where edits are spread over a 2,000-line input, a parse that reuses a memo that lags by
// 64 edits costs about a third of a parse from scratch and one that lags by 256 edits costs as much;
// where they land in a few places, reuse pays at every count measured, up to 1,024, and the limit gives
// that up for the bound.
const PendingEditLimit = 128

// MinRecordedRun is the number of elements below which a Document records no run of a repetition
// (run.minimum): a short repetition costs little to run again. Every backend records the same runs, so
// that each reuses the same elements.
const MinRecordedRun = 16
