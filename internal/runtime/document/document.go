package document

import (
	"fmt"
	"weak"

	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// Document parses a text that is edited repeatedly. It reuses the memo of the previous parse for
// the parts the edits do not affect.
//
// Each memo result records the input range [from, examined) it examined. For an edit
// [start, end):
//   - Results with examined <= start are reused as is (they examined only input before the edit).
//   - Results with from >= end are reused with their positions shifted by the edit's length
//     difference (they examined only input after the edit). However, results of rules whose
//     result depends on position values, and results containing recovered errors, cannot be
//     shifted.
//   - All other results are discarded.
//   - Results that read the seed of a growing left recursion never enter the memo table: they
//     are kept only while the growth lasts, so no later parse reuses them.
//
// Edit applies these rules only to the results at the edited positions; any other result applies
// the edits made since its last use when it is next looked up (AdvanceEntry).
//
// The nodes of a shifted result are moved in place when the result is reused (moveResult), so
// trees returned by earlier parses change with them.
type Document struct {
	changes  *documentChanges
	identity *documentIdentity
	sequence uint64
	runner   Runner
	start    string
	back     state.Backend // resolved construction backend
	defaults state.ParseOptions
	in       state.Input // current text (fully loaded)
	memo     *state.MemoTable
	stats    runtime.ParseStats
	edits    []state.DocEdit // all edits so far; nodes record how many their positions account for
	runs     map[state.RunKey]*state.RunRecord
	runGen   uint32             // edit generation of runs; unchanged parses keep this map
	kids     []*value.Node      // the parser's stack of repetition values, kept for the next parse
	machine  state.BackendState // the backend state of the last parse, cleared and kept for the next one
	// resumed is the number of repetition elements the last Parse resumed (for tests).
	resumed int
	// Candidate-only weak reference: dropped snapshots must remain collectible.
	previousSnapshot weak.Pointer[snapshotGraph]
	// previousRoot is the root of the last published snapshot graph (0 before one): the logical
	// fact that a snapshot was taken, which stays when the collector drops previousSnapshot.
	previousRoot int
}

// Runner is the program a Document parses with. The engine's Program
// implements it; the Document only resolves the backend, prepares the program
// for one parse and runs it.
type Runner interface {
	// SelectBackend returns the backend that a request for b runs on.
	SelectBackend(b state.Backend) state.Backend
	// PrepareRun returns the runner that parses on backend b under the budget control c.
	PrepareRun(b state.Backend, c *state.BudgetControl) (Runner, error)
	// Shared returns the program part that a parser for this runner reads.
	Shared() *state.SharedProgram
	// Run parses p's input from the rule start on backend b.
	Run(p *state.Parser, b state.Backend, start string) (*value.Node, error)
}

// New returns a Document that parses the text in from the rule start
// with runner on backend back. o holds the construction options.
func New(runner Runner, start string, back state.Backend, o state.ParseOptions, in state.Input) *Document {
	return &Document{identity: new(documentIdentity), sequence: 1, runner: runner, start: start, back: back, defaults: o, in: in, memo: state.NewMemoTable()}
}

// maxEdits bounds the edit log: when it is full, the memo is dropped and the log restarts. It is a
// variable for tests.
var maxEdits = 1 << 16

// ParseDefaults returns a copy of the construction options. Invocation counters
// are separate from these defaults.
func (d *Document) ParseDefaults() state.ParseOptions { return d.defaults }

// Text returns the current text.
func (d *Document) Text() string { return d.in.Text(0, d.in.Loaded()) }

// Stats returns the evaluation and memo usage counts of the last Parse.
func (d *Document) Stats() runtime.ParseStats { return d.stats }

// Parse parses the current text. Parser-produced syntax diagnostics belong to
// this call; returned Nodes can share the Document's mutable graph.
func (d *Document) Parse() (*value.Node, error) { return d.ParseWith(d.defaults) }

// ParseWith parses using fully resolved options. Unit, effective backend and
// recognition mode must match construction; this method does not merge defaults.
func (d *Document) ParseWith(o state.ParseOptions) (*value.Node, error) {
	if d.changes != nil {
		var n *value.Node
		_, err := d.snapshotWith(o, &n)
		return n, err
	}
	return d.parseWithCapture(o, nil)
}

// parseWithCapture keeps parsing and result capture inside one interruption and
// cleanup boundary. capture uses the same controller as the parser.
func (d *Document) parseWithCapture(o state.ParseOptions, capture func(*state.Parser, *value.Node, error) error) (n *value.Node, err error) {
	if err := d.checkOptions(o); err != nil {
		return nil, err
	}
	if o.ComputeReport != nil {
		*o.ComputeReport = 0
	}
	var p *state.Parser
	executionStarted := false
	defer func() {
		x := recover()
		if p != nil {
			if executionStarted {
				if x != nil {
					p.Aborted = true
				}
				d.finishParse(p)
			}
			// Setup has not changed Document-owned state. Detach only the
			// temporary parser if admission or the pre-run check failed.
			p.Control = nil
			p.ReleaseTraceReferences()
		}
		if x != nil {
			if f, ok := x.(state.Fatal); ok {
				n, err = nil, f.Err
			} else if cp, ok := x.(state.ContextPanic); ok {
				panic(cp.Value)
			} else {
				// run already classified VM faults; never classify again.
				panic(x)
			}
		}
	}()
	depth, err := state.DepthLimit(o, d.back)
	if err != nil {
		return nil, err
	}
	lim, _ := state.ResolveLimits(o.Limits, uint64(d.in.Loaded()), false)
	if err := state.CheckInputUnits(lim, uint64(d.in.Loaded())); err != nil {
		return nil, err
	}
	c := state.NewBudgetControl(o.Context, lim)
	if c == nil && o.ComputeReport != nil {
		c = state.NewReportControl(lim) // it counts, and limits nothing
	}
	if !state.BudgetControlsEnabled && c != nil {
		return nil, state.ErrNoControls
	}
	if o.ComputeReport != nil {
		defer func() { *o.ComputeReport = c.Used - c.Doc }()
	}
	c.Check()
	if lim.Memory != 0 && d.retainedBytes() > lim.Memory {
		// The caches alone pass the limit: drop them and parse from scratch, rather than fail
		// every later parse of the Document.
		d.ResetHistory()
	}
	active, setupErr := d.runner.PrepareRun(d.back, c)
	if setupErr != nil {
		return nil, setupErr
	}
	runs, lastRuns := d.runs, d.runs
	gen := uint32(len(d.edits))
	if runs != nil && d.runGen == gen {
		// Memo hits may skip every repetition. Keep valid same-generation
		// records without copying the map or advancing their edit generation.
		lastRuns = nil
	} else {
		runs = map[state.RunKey]*state.RunRecord{}
	}
	p = &state.Parser{Prog: active.Shared(), Input: d.in, Memo: d.memo, MemoAll: true, NoPlain: true, MaxDepth: depth, Gen: gen, Edits: d.edits, Expectations: o.Expectations,
		Runs: runs, LastRuns: lastRuns, KidStack: d.kids}
	p.Control = c
	p.SetTuning(o.Tune)
	if o.MissingTokens > 0 && p.Prog.HasMissing {
		// Each parse has the whole limit, as a parse from scratch of the text has. The memo keeps
		// no result of a rule that can insert and the runs keep no element that reached an
		// insertion, so a later parse reuses no insertion and no attempt of this one.
		p.Missing = &state.MissingTokenState{Limit: o.MissingTokens}
	}
	if c != nil && lim.Memory != 0 {
		c.SetRetained(d.historyBytes())
		c.AttachMemory(p)
	}
	if o.Traced() {
		p.SetTrace(o.Trace, o.Profiles)
		p.SelectTrace(o.TraceSelection)
		defer p.CloseTrace()
	}
	c.Check()
	if reuseDocumentVMValues && d.machine != nil {
		d.machine.Keep(d.in.Loaded(), false)
		d.machine.Attach(p)
		p.Backend = d.machine
	}
	executionStarted = true
	n, err = active.Run(p, d.back, d.start)
	if !p.Aborted {
		c.Check()
		// Snapshot capture publishes its own diagnostics. Ordinary Parse must
		// detach recovered errors from memo entries before exposing them.
		if capture == nil {
			if recovered, ok := err.(value.SyntaxErrors); ok {
				m := p.FreeBegin(true) // work of the Document that a parse from scratch does not do
				err = copyDocumentDiagnostics(p, recovered)
				p.FreeEnd(m)
				c.Check()
			}
		}
	}
	if capture != nil {
		// The capture and the comparison of a snapshot are work that a parse from scratch does not do:
		// Document units, which no Compute limit counts and which keep the polls for cancellation and
		// memory (spec/runtime/documents.md#change-notifications).
		m := p.FreeBegin(true)
		err = capture(p, n, err)
		p.FreeEnd(m)
	}
	return n, err
}

// checkOptions rejects the options of a parse that the Document cannot run: stream diagnostics, and a
// unit, backend, recognition mode, expectations mode or missing-token limit other than those of its
// construction. A rejected parse changes nothing, not even the change notifications.
func (d *Document) checkOptions(o state.ParseOptions) error {
	if err := state.ValidateStreamDiagnostics(o, false); err != nil {
		return err
	}
	if o.Unit != d.in.Unit || d.runner.SelectBackend(o.Backend) != d.back || o.Recognize || o.Expectations != d.defaults.Expectations || o.MissingTokens != d.defaults.MissingTokens {
		return fmt.Errorf("document parse cannot change unit, backend, recognition mode, expectations or missing-token limit")
	}
	return nil
}

// finishParse transfers reusable state before any traced parser is detached.
// Mandatory cleanup is unbudgeted, so interruption cannot abort it.
func (d *Document) finishParse(p *state.Parser) {
	d.stats = p.ReportedStats()
	d.kids = p.KidStack[:0]
	clear(d.kids[:cap(d.kids)]) // let go of the nodes, even on a trace panic
	if reuseDocumentVMValues {
		d.machine = p.Backend
		if d.machine != nil {
			// A whole-rule memo hit evaluates no bodies and never writes the
			// backend's stacks. Their already cleared storage needs no second scan.
			d.machine.Keep(d.in.Loaded(), p.Stats.Evaluated != 0)
		}
	}
	// Keep the tables the parse built on demand, so later parses and edits reuse them.
	d.in.Offs, d.in.Lines = p.Offs, p.Lines
	if p.Aborted {
		// The parse stopped inside growths and runs: discard its state rather than
		// sort out which of its results a completed parse would have kept.
		d.invalidateParseState()
	} else {
		state.KeepSeedResults(d.memo, p)
		d.runs, d.resumed = p.Runs, p.Resumed
		d.runGen = p.Gen
	}
}

func (d *Document) invalidateParseState() {
	clear(d.kids[:cap(d.kids)])
	if d.machine != nil {
		d.machine.Release()
	}
	d.memo = state.NewMemoTable()
	d.runs, d.kids, d.machine, d.resumed = nil, nil, nil, 0
}

// Edit replaces [start, end) of the text (in the Document's position unit) with text.
// With byte positions, start and end must lie on character boundaries.
// Invalid UTF-8 bytes are separate characters; an edit may complete a prefix.
func (d *Document) Edit(start, end int, text string) error {
	n := d.in.Loaded()
	if start < 0 || end < start || end > n {
		return fmt.Errorf("invalid range [%d,%d) for text of length %d", start, end, n)
	}
	if d.in.Unit == input.Bytes {
		for _, pos := range []int{start, end} {
			var err error
			if state.DocumentPieceInput && d.in.Pieces != nil {
				err = d.in.Pieces.ValidBoundary(pos)
			} else {
				err = state.ValidBoundary(d.in.Src, input.Bytes, pos)
			}
			if err != nil {
				return err
			}
		}
	}
	if err := d.nextVersion(); err != nil {
		return err
	}
	from := d.Version()
	delta := d.in.Replace(start, end, text)
	d.sequence++
	d.recordEdit(from, []input.TextEdit{{Start: start, End: end, Text: text}})
	if state.DocumentPieceInput && d.in.Pieces == nil && d.in.Loaded() >= state.DocumentPieceMinSize {
		// A small Document can grow into the measured range where the piece
		// representation wins. Promote once, after the flat edit has completed.
		d.in.Pieces = state.NewDocumentPieces(d.in.Text(0, d.in.Loaded()), d.in.Unit)
		d.in.In, d.in.Bs, d.in.Src, d.in.Offs, d.in.SrcOK = nil, nil, "", nil, false
	}
	if len(d.edits) == maxEdits {
		d.memo, d.edits, d.runs = state.NewMemoTable(), nil, nil
		return nil
	}
	d.edits = state.EditMemo(d.memo, d.edits, state.DocEdit{Start: start, End: end, Delta: delta})
	return nil
}
