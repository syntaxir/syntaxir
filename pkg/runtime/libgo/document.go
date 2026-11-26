package libgo

import (
	"fmt"
	"strings"
	"unicode/utf8"
	"unsafe"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// Document incrementally parses the configured start rule. It belongs to one
// goroutine; returned nodes may be shared with later parses.
//
// Of its fields and methods, BeginParse, EndParse, ResolveParseOptions, State are for generated parsers; not a stable API.
type Document struct {
	State    *documentState
	defaults parseOptions
	// parsing is set while a parse runs, whose observation callbacks must not
	// parse or edit the Document again (BeginParse).
	parsing bool
}

// DocumentBusy returned by a Parse, ParseValue or Edit of a Document that is
// already parsing. Only a trace callback of that parse can make one; parsers
// generated with observability export it as ErrDocumentBusy.
//
// DocumentBusy is for generated parsers; not a stable API.
var DocumentBusy = fmt.Errorf("document is already parsing")

func (d *Document) BeginParse() error {
	if d.parsing {
		return DocumentBusy
	}
	d.parsing = true
	return nil
}

func (d *Document) EndParse() { d.parsing = false }

// MakeDocument is for generated parsers; not a stable API.
func MakeDocument(root *Rule, text string, opts []sxirruntime.ParseOption) (d *Document, err error) {
	if witnessFeatures {
		useFeature(root.g, featureDocument)
	}
	o, err := ResolveOptions(root.g, opts)
	if err != nil {
		return nil, err
	}
	// With a missing-token limit, every parse of the Document runs the insertion table: the limit
	// is fixed at construction.
	root = root.forMissing(o)
	defer recoverParse(&d, &err)
	o = prepareText(o, text)
	// Construction performs the same input decoding and budget checks as a parse,
	// but its counters do not carry into subsequent invocations.
	p := &Parser{unit: o.unit}
	p.setControl(o.control)
	p.setGrammar(root.g)
	p.Expectations = o.expectations
	o.control.attachMemory(p)
	if o.unit == sxirinput.CodePoints {
		p.setSource(text)
	}
	o.control.Check()
	o.control, o.prepared = nil, false
	if o.unit == sxirinput.CodePoints {
		text = normalizeCodePoints(text)
	}
	state := &documentState{root: root, text: text, unit: o.unit}
	if o.unit == sxirinput.CodePoints {
		in := p.In
		if p.src != text {
			// The text held invalid UTF-8: decode the normalized text.
			in = []rune(text)
		}
		state.setText(text, in)
	}
	state.expectations = o.expectations
	return &Document{State: state, defaults: o}, nil
}

// Text returns the current text. In Bytes it holds the exact bytes, including invalid UTF-8; in
// CodePoints each invalid UTF-8 byte has been replaced by U+FFFD (normalizeCodePoints).
func (d *Document) Text() string { return d.State.currentText() }

// Edit replaces [start,end) in the Document's position unit.
func (d *Document) Edit(start, end int, text string) error {
	if d.parsing {
		return DocumentBusy
	}
	_, err := d.State.editReuse(start, end, text)
	return err
}

// Parse parses the current text, applying invocation options after the
// Document's construction defaults. An invocation cannot change its unit.
func (d *Document) Parse(opts ...sxirruntime.ParseOption) (*sxirvalue.Node, error) {
	if err := d.BeginParse(); err != nil {
		return nil, err
	}
	defer d.EndParse()
	o, err := d.ResolveParseOptions(opts)
	if err != nil {
		return nil, err
	}
	return d.State.parse(o)
}

func (d *Document) ResolveParseOptions(opts []sxirruntime.ParseOption) (parseOptions, error) {
	o := d.defaults
	if err := o.applyOptions(opts); err != nil {
		return o, err
	}
	depth, err := resolveDepth(o.limits.Depth, d.State.root.g.maxDepth)
	if err != nil {
		return o, err
	}
	o.maxDepth = depth
	if d.State.root.g.NoBudgetControls && (o.context != nil || o.computeOut != nil || !limitsDisabled(o.limits)) {
		return o, fmt.Errorf("this parser was generated without limit controls (-no-limits; generate it without that option to use WithContext, WithComputeReport and a finite Compute or Memory limit)")
	}
	if err := o.validateDiagnostics(); err != nil {
		return o, err
	}
	if err := o.validateObservation(d.State.root.g); err != nil {
		return o, err
	}
	if o.missingTokens != d.defaults.missingTokens {
		// The Document runs the table that its limit selected at construction.
		return o, fmt.Errorf("document parse cannot change the missing-token limit")
	}
	if o.computeOut != nil {
		*o.computeOut = 0
	}
	return o, nil
}

// Unit returns the position unit of the document, fixed at construction.
func (d *Document) Unit() sxirinput.PositionUnit { return d.State.unit }

// ResetHistory releases the memo and the repetition runs, the incremental reuse history of a
// generated Document, without changing the text, the construction options or the statistics of the
// last parse. Trees returned earlier are detached from future node movement; this does not undo
// earlier movement.
func (d *Document) ResetHistory() { d.State.resetHistory() }

// resetHistory is ResetHistory.
func (d *documentState) resetHistory() {
	d.memo, d.typedMemo, d.runs, d.edits, d.moves = memoTable{}, nil, nil, nil, 0
}

// retainedBytes returns the bytes that a parse of either form under a memory limit starts its count
// at: everything the Document keeps from earlier parses, each once. If they alone pass the limit,
// the parse resets the whole history first (resetHistory).
func (d *documentState) retainedBytes() uint64 {
	return saturatingAdd(d.memo.chargedBytes(), d.historyBytes())
}

// historyBytes returns what the Document keeps beside the memo table of Node parses: the run
// records, the estimate of the typed memo's slots and entries, and the edit log, which both memos
// share. A Node parse reads its memo table whole as it counts (attachMemory), so the count of its
// control starts at these bytes; a typed parse starts at all of retainedBytes.
func (d *documentState) historyBytes() uint64 {
	n := saturatingAdd(runBytes(d.runs), uint64(cap(d.edits))*uint64(unsafe.Sizeof(documentEdit{})))
	if t, ok := d.typedMemo.(interface{ retainedBytes() uint64 }); ok {
		n = saturatingAdd(n, t.retainedBytes())
	}
	return n
}

// CacheStats returns a point-in-time estimate of the memo and the repetition runs that the Document
// retains. It does not measure the process heap or bytes returned to the operating system.
func (d *Document) CacheStats() sxirruntime.CacheStats {
	var s sxirruntime.CacheStats
	t := &d.State.memo
	for _, head := range t.slots {
		for e := head; e != nil; e = e.next {
			s.LiveMemoEntries++
		}
	}
	for e := t.free; e != nil; e = e.next {
		s.RetiredMemoEntries++
	}
	s.EstimatedBytes += uint64(cap(t.slots)) * uint64(unsafe.Sizeof((*memoEntry)(nil)))
	s.EstimatedBytes += uint64(cap(t.chunks)) * uint64(unsafe.Sizeof([]memoEntry(nil)))
	for _, chunk := range t.chunks {
		s.EstimatedBytes += uint64(cap(chunk)) * uint64(unsafe.Sizeof(memoEntry{}))
	}
	s.EstimatedBytes += uint64(cap(t.seen)) * uint64(unsafe.Sizeof(uint64(0)))
	s.EstimatedBytes += uint64(cap(t.calls)) * uint64(unsafe.Sizeof(seenCalls{}))
	s.EstimatedBytes += uint64(cap(t.pages)) * uint64(unsafe.Sizeof(seenRulePages{}))
	for _, dir := range t.pages {
		s.EstimatedBytes += uint64(cap(dir.pages)) * uint64(unsafe.Sizeof((*[16]uint64)(nil)))
	}
	s.EstimatedBytes += uint64(cap(t.bitPages.chunks)) * uint64(unsafe.Sizeof((*[64][16]uint64)(nil)))
	s.EstimatedBytes += uint64(len(t.bitPages.chunks)) * uint64(unsafe.Sizeof([64][16]uint64{}))
	s.EstimatedBytes += t.indexBytes
	for _, run := range d.State.runs {
		s.RepetitionElements += len(run.elems)
	}
	s.EstimatedBytes += runBytes(d.State.runs)
	return s
}

// Stats returns the evaluation and reuse counts of the last Parse: Reused counts the memo hits and the
// elements of repetitions that the parse resumed.
func (d *Document) Stats() sxirruntime.ParseStats {
	s := d.State.stats
	return sxirruntime.ParseStats{Evaluated: s.Evaluated, Reused: s.Reused, MemoHits: s.Reused - s.Resumed, ResumedElements: s.Resumed}
}

// A slice's first element, length and capacity identify an exact slice view.
// The element pointer keeps the backing array visible to the collector while
// cloning. Empty slices without capacity have no elements or mutable backing
// array to share.
//
// DocumentSliceKey is for generated parsers; not a stable API.
type DocumentSliceKey struct {
	First    any
	Length   int
	Capacity int
}

// newDocumentState parses only the configured start rule. In CodePoints the text is normalized
// (normalizeCodePoints), as MakeDocument normalizes it.
func newDocumentState(root *Rule, text string, unit sxirinput.PositionUnit) *documentState {
	if unit == sxirinput.CodePoints {
		text = normalizeCodePoints(text)
	}
	d := &documentState{root: root, text: text, unit: unit}
	if unit == sxirinput.CodePoints {
		d.setText(text, []rune(text))
	}
	return d
}

func (d *documentState) edit(start, end int, replacement string) error {
	_, err := d.editReuse(start, end, replacement)
	return err
}

// documentEdit is an edit as the memo sees it (memo.splice): the input [start, end) is replaced
// with input delta units longer, and the input after end decodes as before.
type documentEdit struct{ start, end, delta int }

// editReuse applies an edit to the text and the memo (memo.splice) and returns it as the memo sees
// it: from before start when, in Bytes, the replacement can complete an incomplete UTF-8 sequence
// before start. In CodePoints the replacement is normalized (normalizeCodePoints) first.
func (d *documentState) editReuse(start, end int, replacement string) (ed documentEdit, err error) {
	length := d.units()
	if start < 0 || end < start || end > length {
		return documentEdit{}, fmt.Errorf("invalid range [%d,%d) for text of length %d", start, end, length)
	}
	if d.unit == sxirinput.Bytes {
		if err := documentByteBoundary(d.text, start); err != nil {
			return documentEdit{}, err
		}
		if err := documentByteBoundary(d.text, end); err != nil {
			return documentEdit{}, err
		}
		// The replacement can complete an incomplete UTF-8 sequence before start,
		// turning invalid bytes, each a separate character, into one character.
		// Such a sequence has at most UTFMax-1 bytes, each one position, so
		// results that examined them are not retained.
		reuse := start
		for i := 0; i < utf8.UTFMax-1 && i < start; i++ {
			if r, size := utf8.DecodeLastRuneInString(d.text[:start-i]); r != utf8.RuneError || size != 1 {
				break
			}
			reuse--
		}
		ed = documentEdit{start: reuse, end: end, delta: len(replacement) - (end - start)}
		d.text = d.text[:start] + replacement + d.text[end:]
	} else {
		// The text is valid UTF-8 and so is the normalized replacement, so no
		// sequence joins across the ends of the edit: the characters around it
		// decode as before.
		replacement = normalizeCodePoints(replacement)
		ed = documentEdit{start: start, end: end, delta: utf8.RuneCountInString(replacement) - (end - start)}
		d.editText(start, end, replacement)
	}
	if len(d.edits) == maxDocumentEdits {
		// The log is full: the next parse starts without the memos and the runs, as a parse from scratch
		// does.
		d.memo, d.typedMemo, d.edits, d.runs = memoTable{}, nil, nil, nil
	} else {
		d.edits = append(d.edits, ed)
		d.memo.splice(d.edits)
		if d.typedMemo != nil {
			d.typedMemo.splice(d.edits)
		}
	}
	return ed, nil
}

// dropMemo drops the memo of Node parses and the runs that they recorded, and the edit log with them
// unless the typed memo still refers to it.
func (d *documentState) dropMemo() {
	d.memo, d.runs = memoTable{}, nil
	if d.typedMemo == nil {
		d.edits = nil
	}
}

// dropTypedMemo drops the memo of typed parses, and the edit log with it unless the memo of Node
// parses still holds entries that refer to it or runs whose generations count its edits.
func (d *documentState) dropTypedMemo() {
	d.typedMemo = nil
	if len(d.memo.slots) == 0 && d.runs == nil {
		d.edits = nil
	}
}

// maxDocumentEdits bounds the edit log of a Document (documentState.edits): the edit that finds it
// full drops the memo and empties the log instead of adding to it.
var maxDocumentEdits = 1 << 16

// splice applies the last edit of log to the memo (memo.splice): the entries at the positions it
// replaced, from its start to its end, both included, are applied the edits they do not account for
// at once (advanceEntry) and stay, move by its delta or go; the slots of the positions after it follow
// them through the gap (moveGap), and every other entry is applied the edit when a lookup finds it.
// The hash indexes of the chains are dropped, since slots move.
func (t *memoTable) splice(log []documentEdit) {
	ed := log[len(log)-1]
	t.index, t.indexBytes = nil, 0
	type kept struct {
		e   *memoEntry
		pos int
	}
	var stay []kept
	for pos := ed.start; pos <= ed.end; pos++ {
		i := t.slot(pos)
		if i >= len(t.slots) {
			break
		}
		for e := t.slots[i]; e != nil; {
			next := e.next
			// The entry accounts for the edits before this one first, at its slot; this one then keeps
			// it at its position or moves it by the delta.
			if advanceEntry(e, log[:len(log)-1]) {
				to := pos
				if e.examined > ed.start {
					to += ed.delta
				}
				if advanceEntry(e, log) {
					stay = append(stay, kept{e, to})
					e = next
					continue
				}
			}
			t.retire(e)
			e = next
		}
		t.slots[i] = nil
	}
	var added int
	t.slots, added = moveGap(&t.memoGap, t.slots, ed.start, ed.end, ed.delta)
	t.memBytes += uint64(added) * ptrBytes
	for _, k := range stay {
		i := t.slot(k.pos)
		k.e.next = t.slots[i]
		t.slots[i] = k.e
	}
}

// retire returns e to the free list of the table, dropping what it refers to.
func (t *memoTable) retire(e *memoEntry) {
	*e = memoEntry{next: t.free}
	t.free = e
}

// moveMarkLimit is the number of moves after which a parse starts without the memo of the Document,
// and numbers the moves from 1 again. A parse moves each entry of the memo at most once, so it makes
// far fewer moves than the numbers that remain.
const moveMarkLimit = 1 << 31

// documentMove moves the nodes of the value of e by the shift that edits gave e (advanceEntry), in
// copies, since the value may be part of a result already returned, and reports whether it could:
// a value that holds a host value other than a node or a basic value is not moved. The check and the
// copy each cost, for every distinct node of the value, a unit and one more for each of its children
// and fields.
func (p *Parser) documentMove(e *memoEntry) bool {
	if e.shift == 0 {
		return true
	}
	if e.node == nil {
		// A result without a node holds no positions.
		e.shift = 0
		return true
	}
	// Moving the value is work of the Document that a parse from scratch does not do.
	n, ok := p.moveCopy(e.node, e.shift)
	if !ok {
		return false
	}
	e.node, e.shift = n, 0
	return true
}

// moveCopy returns a copy of the value n with its positions moved by shift, and reports whether it
// could: a value that holds a host value other than a node or a basic value is not moved. The check
// and the copy each cost, for every distinct node of the value, a unit and one more for each of its
// children and fields.
func (p *Parser) moveCopy(n *sxirvalue.Node, shift int32) (*sxirvalue.Node, bool) {
	// The check marks the nodes it visits with the number of the move, which no earlier move of the
	// Document had, so that a mark that an earlier move left, in this parse or an earlier one, whether
	// that move went on or refused, never reads as a visit of this one. A node that the check finds
	// marked occurs in the value more than once; such a value is moved through a map from the nodes to
	// their copies, so that each is copied once and the copies are shared as the nodes were.
	d := p.Document
	d.moves++
	movable, shared := p.movable(n, d.moves)
	if !movable {
		return nil, false
	}
	if shared {
		if p.moved == nil {
			p.moved = map[*sxirvalue.Node]*sxirvalue.Node{}
		}
		n = p.moveShared(n, shift)
		clear(p.moved)
		return n, true
	}
	return p.moveNode(n, shift), true
}

// movable reports whether moveNode can move n: its fields and box hold only nodes and basic values;
// and whether a node occurs in n more than once. It marks the nodes it visits with mark.
func (p *Parser) movable(n *sxirvalue.Node, mark uint32) (movable, shared bool) {
	if n == nil {
		return true, false
	}
	if D(n).Mark == mark {
		return true, true
	}
	D(n).Mark = mark
	if !basicValue(D(n).Box) {
		return false, false
	}
	for _, c := range D(n).Children {
		ok, sh := p.movable(c, mark)
		if !ok {
			return false, false
		}
		shared = shared || sh
	}
	for _, f := range D(n).Fields {
		if c, ok := f.Value.(*sxirvalue.Node); ok {
			ok, sh := p.movable(c, mark)
			if !ok {
				return false, false
			}
			shared = shared || sh
		} else if !basicValue(f.Value) {
			return false, false
		}
	}
	return true, shared
}

// basicValue reports whether v holds no positions: nil, a boolean, a number or a string.
func basicValue(v any) bool {
	switch v.(type) {
	case nil, bool, string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr,
		float32, float64, complex64, complex128:
		return true
	}
	return false
}

// moveNode returns a copy of the movable node n, in which no node occurs twice, with its positions
// and those of its descendants moved by shift.
func (p *Parser) moveNode(n *sxirvalue.Node, shift int32) *sxirvalue.Node {
	if n == nil {
		return nil
	}
	c := p.NewNode(*D(n))
	D(c).Start += shift
	D(c).End += shift
	if D(n).Children != nil {
		D(c).Children = p.Nodes(len(D(n).Children))
		for i, k := range D(n).Children {
			D(c).Children[i] = p.moveNode(k, shift)
		}
	}
	if D(n).Fields != nil {
		D(c).Fields = p.fields(len(D(n).Fields))
		for _, f := range D(n).Fields {
			if k, ok := f.Value.(*sxirvalue.Node); ok {
				f.Value = p.moveNode(k, shift)
			}
			D(c).Fields = append(D(c).Fields, f)
		}
	}
	return c
}

// moveShared is moveNode for a value that holds a node more than once: moved holds the copies made
// so far, which it shares.
func (p *Parser) moveShared(n *sxirvalue.Node, shift int32) *sxirvalue.Node {
	if n == nil {
		return nil
	}
	if c := p.moved[n]; c != nil {
		return c
	}
	c := p.NewNode(*D(n))
	p.moved[n] = c
	D(c).Start += shift
	D(c).End += shift
	if D(n).Children != nil {
		D(c).Children = p.Nodes(len(D(n).Children))
		for i, k := range D(n).Children {
			D(c).Children[i] = p.moveShared(k, shift)
		}
	}
	if D(n).Fields != nil {
		D(c).Fields = p.fields(len(D(n).Fields))
		for _, f := range D(n).Fields {
			if k, ok := f.Value.(*sxirvalue.Node); ok {
				f.Value = p.moveShared(k, shift)
			}
			D(c).Fields = append(D(c).Fields, f)
		}
	}
	return c
}

// normalizeCodePoints returns s with each byte that is not part of a valid UTF-8 encoding replaced by
// U+FFFD, the text of s in CodePoints, where each such byte decodes as one replacement character. It
// returns s itself when s is valid UTF-8.
func normalizeCodePoints(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 2*len(s)/3)
	for _, r := range s {
		b.WriteRune(r)
	}
	return b.String()
}

func documentByteBoundary(text string, pos int) error {
	if pos < len(text) && !utf8.RuneStart(text[pos]) {
		for start := pos - 1; start >= max(0, pos-utf8.UTFMax+1); start-- {
			if utf8.RuneStart(text[start]) {
				_, size := utf8.DecodeRuneInString(text[start:])
				if start+size > pos {
					return fmt.Errorf("position %d is not at a character boundary", pos)
				}
				break
			}
		}
	}
	return nil
}

// parse retains completed memo entries after ordinary syntax failures. A
// panic or budget interruption discards all entries because they may depend on
// unfinished left-recursion growth or partially evaluated actions.
func (d *documentState) parse(o parseOptions) (*sxirvalue.Node, error) {
	return DocumentParseConverted(d, o, func(n *sxirvalue.Node, _ *BudgetControl, _ error) *sxirvalue.Node { return n })
}

// DocumentParseConverted keeps Node parsing and final typed conversion inside
// the same limit-controlled invocation and memo cleanup boundary.
//
// DocumentParseConverted is for generated parsers; not a stable API.
func DocumentParseConverted[T any](d *documentState, o parseOptions, convert func(*sxirvalue.Node, *BudgetControl, error) T) (res T, err error) {
	defer recoverParse(&res, &err)
	if o.unit != d.unit {
		return res, fmt.Errorf("document parse cannot change unit")
	}
	if o.expectations != d.expectations {
		return res, fmt.Errorf("document parse cannot change expectations")
	}
	if d.root == nil {
		return res, fmt.Errorf("document start rule is missing")
	}
	o = prepareUnits(o, uint64(d.units()))
	if o.control != nil && o.control.limits.Memory != 0 && d.retainedBytes() > o.control.limits.Memory {
		// What the Document retains alone exceeds the limit: the parse starts without it, as after
		// ResetHistory.
		d.resetHistory()
	}
	p := &Parser{unit: d.unit, MaxDepth: o.maxDepth}
	p.setControl(o.control)
	p.setGrammar(d.root.g)
	p.Expectations = o.expectations
	p.Observation = newObservation(&o.observe, &p.Silent, &d.stats.Resumed)
	defer p.releaseObservation()
	p.useDocumentInput(d)
	if d.moves > moveMarkLimit {
		// The marks that the nodes of the memo carry would soon repeat: start without it.
		d.dropMemo()
		d.moves = 0
	}
	p.Document, p.memo = d, d.memo
	p.edits, p.gen = d.edits, uint32(len(d.edits))
	// The runs of the last parse: the records of the same text, which this parse takes over, or those
	// of the text before the edits since, which it may resume once.
	p.runs, p.lastRuns = d.runs, d.runs
	if p.runs != nil && d.runGen == p.gen {
		p.lastRuns = nil
	} else {
		p.runs = map[runKey]*runRecord{}
	}
	p.setTracking()
	p.setDeferMemo()
	// Each parse has the whole limit, as a parse from scratch of the text has. The memo keeps no result
	// of a rule that can insert and the runs keep no element that reached an insertion site
	// (runClean), so a later parse reuses no insertion and no attempt of this one. A parse that
	// reaches the limit panics, which drops the memo and the runs.
	p.startMissing(o)
	o.control.attachMemory(p)
	if o.control != nil && o.control.limits.Memory != 0 {
		o.control.retained = d.historyBytes()
	}
	d.stats = documentStats{}
	defer func() {
		if x := recover(); x != nil {
			// The parse stopped inside growths and runs: none of its state is kept.
			d.dropMemo()
			panic(x)
		}
		d.memo, d.runs, d.runGen = p.memo, p.runs, p.gen
	}()
	p.Control.Check()
	p.startUnits(o)
	defer p.reportUnits()
	v, ok := p.Call(d.root.forParse(p), 0)
	p.Control.Check()
	p.endUnits(!ok || p.Pos != p.n)
	if p.units.memo != nil {
		p.reportMemo(p.memoLines())
	}
	var n *sxirvalue.Node
	if ok && p.Pos == p.n {
		n = v
		if len(p.Recovered) > 0 {
			err = sxirvalue.SyntaxErrors(p.recoveredErrors())
		}
	} else {
		if ok {
			p.Expect(p.Pos, IdEndInput)
		}
		err = p.MakeError(p.farthest, p.exp[p.expBase:])
	}
	err = publishDocumentError(p, err)
	if p.units.on {
		// The copy is work of the Document that counts no unit: the context and the memory count are
		// observed after it, before the conversion, as the engine checks after its copy.
		p.unitsObserve()
	}
	c, done := conversionControl(o.unitsCtx, o.control)
	defer done()
	res = convert(n, c, err)
	c.Check()
	return res, err
}

// publishDocumentError separates caller-owned diagnostics from private memo
// entries. Strings are immutable, but the error objects and slice containers
// must not be shared with a later invocation.
func publishDocumentError(p *Parser, err error) error {
	switch e := err.(type) {
	case sxirvalue.SyntaxErrors:
		if e == nil {
			return e
		}
		out := make(sxirvalue.SyntaxErrors, len(e))
		for i, item := range e {
			out[i] = cloneDocumentError(p, item)
		}
		return out
	}
	// Ordinary failures are freshly assembled by MakeError, not memoized.
	return err
}

func cloneDocumentError(p *Parser, e *sxirvalue.SyntaxError) *sxirvalue.SyntaxError {
	if e == nil {
		return nil
	}
	out := new(sxirvalue.SyntaxError)
	*out = *e
	if e.Expected != nil {
		out.Expected = make([]string, len(e.Expected))
		copySpend(p, out.Expected, e.Expected)
	}
	if e.Messages != nil {
		out.Messages = make([]string, len(e.Messages))
		copySpend(p, out.Messages, e.Messages)
	}
	return out
}
