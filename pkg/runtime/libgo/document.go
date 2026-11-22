package libgo

import (
	"fmt"
	"strings"
	"unicode/utf8"

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
func MakeDocument(root *Rule, text string, opts []ParseOption) (d *Document, err error) {
	if witnessFeatures {
		useFeature(root.g, featureDocument)
	}
	o, err := ResolveOptions(root.g, opts)
	if err != nil {
		return nil, err
	}
	defer recoverParse(&d, &err)
	o = prepareText(o, text)
	// Construction performs the same input decoding and budget checks as a parse,
	// but its counters do not carry into subsequent invocations.
	p := &Parser{unit: o.unit, Control: o.control}
	p.setGrammar(root.g)
	p.Expectations = o.expectations
	o.control.attachMemory(p)
	if o.unit == sxirinput.CodePoints {
		p.setSource(text)
	} else if o.control != nil {
		p.SpendRead(uint64(len(text)))
	}
	o.control.Check()
	o.control, o.prepared = nil, false
	if o.unit == sxirinput.CodePoints {
		text = normalizeCodePoints(text)
	}
	state := newDocumentState(root, text, o.unit)
	state.expectations = o.expectations
	return &Document{State: state, defaults: o}, nil
}

// Text returns the current text. In Bytes it holds the exact bytes, including invalid UTF-8; in
// CodePoints each invalid UTF-8 byte has been replaced by U+FFFD (normalizeCodePoints).
func (d *Document) Text() string { return d.State.text }

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
func (d *Document) Parse(opts ...ParseOption) (*sxirvalue.Node, error) {
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

func (d *Document) ResolveParseOptions(opts []ParseOption) (parseOptions, error) {
	o := d.defaults
	for _, opt := range opts {
		opt(&o)
	}
	depth, err := resolveDepth(o.limits.Depth, d.State.root.g.maxDepth)
	if err != nil {
		return o, err
	}
	o.maxDepth = depth
	if d.State.root.g.NoBudgetControls && (o.context != nil || !limitsDisabled(o.limits)) {
		return o, fmt.Errorf("this parser was generated without limit controls (-no-limits; generate it without that option to use WithContext and a finite Compute or Memory limit)")
	}
	if err := o.validateDiagnostics(); err != nil {
		return o, err
	}
	if err := o.validateObservation(d.State.root.g); err != nil {
		return o, err
	}
	return o, nil
}

// Stats returns the evaluation and reuse counts of the last Parse. A generated Document reuses
// memo results only, never repetition elements, so every reuse is a memo hit.
func (d *Document) Stats() sxirruntime.ParseStats {
	s := d.State.stats
	return sxirruntime.ParseStats{Evaluated: s.Evaluated, Reused: s.Reused, MemoHits: s.Reused}
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

// newDocumentState parses only the configured start rule.
func newDocumentState(root *Rule, text string, unit sxirinput.PositionUnit) *documentState {
	return &documentState{root: root, text: text, unit: unit}
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
	length := len(d.text)
	if d.unit != sxirinput.Bytes {
		length = utf8.RuneCountInString(d.text)
	}
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
		d.text = d.text[:documentByteOffset(d.text, start)] + replacement + d.text[documentByteOffset(d.text, end):]
	}
	if len(d.edits) == maxDocumentEdits {
		// The log is full: the next parse starts without the memos, as a parse from scratch does.
		d.memo, d.typedMemo, d.edits = memoTable{}, nil, nil
	} else {
		d.edits = append(d.edits, ed)
		d.memo.splice(d.edits)
		if d.typedMemo != nil {
			d.typedMemo.splice(d.edits)
		}
	}
	return ed, nil
}

// dropMemo drops the memo of Node parses, and the edit log with it unless the typed memo still
// refers to it.
func (d *documentState) dropMemo() {
	d.memo = memoTable{}
	if d.typedMemo == nil {
		d.edits = nil
	}
}

// dropTypedMemo drops the memo of typed parses, and the edit log with it unless the memo of Node
// parses still holds entries that refer to it.
func (d *documentState) dropTypedMemo() {
	d.typedMemo = nil
	if len(d.memo.slots) == 0 {
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
	shift := e.shift
	// The check marks the nodes it visits with the number of the move, which no earlier move of the
	// Document had, so that a mark that an earlier move left, in this parse or an earlier one, whether
	// that move went on or refused, never reads as a visit of this one. A node that the check finds
	// marked occurs in the value more than once; such a value is moved through a map from the nodes to
	// their copies, so that each is copied once and the copies are shared as the nodes were.
	// Moving the value is work of the Document that a parse from scratch does not do.
	defer p.Control.FreeEnd(p.Control.FreeBegin(true))
	d := p.Document
	d.moves++
	movable, shared := p.movable(e.node, d.moves)
	if !movable {
		return false
	}
	if shared {
		if p.moved == nil {
			p.moved = map[*sxirvalue.Node]*sxirvalue.Node{}
		}
		e.node = p.moveShared(e.node, int32(shift))
		clear(p.moved)
	} else {
		e.node = p.moveNode(e.node, int32(shift))
	}
	e.shift = 0
	return true
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
	p.spendValue(1 + len(D(n).Children) + len(D(n).Fields))
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
	p.spendValue(len(D(n).Children) + len(D(n).Fields))
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
	p.spendValue(len(D(n).Children) + len(D(n).Fields))
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

func documentByteOffset(text string, index int) int {
	if index == 0 {
		return 0
	}
	count := 0
	for off := range text {
		if count == index {
			return off
		}
		count++
	}
	return len(text)
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
	o = prepareText(o, d.text)
	if o.control != nil && o.control.limits.Memory != 0 && d.memo.chargedBytes() > o.control.limits.Memory {
		// The retained memo alone exceeds the limit: the parse starts without it.
		d.dropMemo()
	}
	p := &Parser{unit: d.unit, MaxDepth: o.maxDepth, Control: o.control}
	p.setGrammar(d.root.g)
	p.Expectations = o.expectations
	p.Observation = o.control.observe(newObservation(&o.observe, &p.Silent))
	defer p.releaseObservation()
	if d.unit == sxirinput.Bytes {
		p.bs, p.n = d.text, len(d.text)
	} else {
		p.setSource(d.text)
	}
	if d.moves > moveMarkLimit {
		// The marks that the nodes of the memo carry would soon repeat: start without it.
		d.dropMemo()
		d.moves = 0
	}
	p.Document, p.memo = d, d.memo
	p.edits, p.gen = d.edits, uint32(len(d.edits))
	p.setTracking()
	p.setDeferMemo()
	o.control.attachMemory(p)
	d.stats = documentStats{}
	defer func() {
		if x := recover(); x != nil {
			d.dropMemo()
			panic(x)
		}
		d.memo = p.memo
	}()
	p.Control.Check()
	v, ok := p.Call(d.root.forParse(p), 0)
	p.Control.Check()
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
	res = convert(n, o.control, err)
	p.Control.Check()
	return res, err
}

// publishDocumentError separates caller-owned diagnostics from private memo
// entries. Strings are immutable, but the error objects and slice containers
// must not be shared with a later invocation.
func publishDocumentError(p *Parser, err error) error {
	// The copy is work that only a Document parse does.
	defer p.Control.FreeEnd(p.Control.FreeBegin(true))
	switch e := err.(type) {
	case sxirvalue.SyntaxErrors:
		if e == nil {
			return e
		}
		p.spendValue(len(e))
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
	p.Spend()
	if e == nil {
		return nil
	}
	out := new(sxirvalue.SyntaxError)
	*out = *e
	if e.Expected != nil {
		p.spendValue(len(e.Expected))
		out.Expected = make([]string, len(e.Expected))
		copySpend(p, out.Expected, e.Expected)
	}
	if e.Messages != nil {
		p.spendValue(len(e.Messages))
		out.Messages = make([]string, len(e.Messages))
		copySpend(p, out.Messages, e.Messages)
	}
	return out
}
