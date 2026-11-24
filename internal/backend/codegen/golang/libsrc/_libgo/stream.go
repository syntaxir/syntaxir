package libgo

import (
	"bufio"
	"errors"
	"io"
	"iter"
	"math"
	"slices"
	"unicode/utf8"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// streamInput is the input of a stream parse. Positions are absolute; the
// buffer holds the input from base on, read from reader as matching needs it.
// The generated input fields (in, bs and n) stay empty, so inlined fast paths
// fall back to the stream-aware peek and literal matching.
//
// Emit is for generated parsers; not a stable API.
type streamInput struct {
	reader *bufio.Reader
	in     []rune // CodePoints
	bs     []byte // Bytes
	base   int
	// textFloor is the last commit point (stream.cutText): text cuts every range at it, whatever
	// input before it the buffer still holds, so a text never depends on how the input arrives.
	textFloor int
	// baseLine and baseCol are the line and column (1-based, in the unit) of base.
	baseLine, baseCol int
	eof               bool
	// maxInput, when positive, is the longest stream the parse accepts, in units (streamFill).
	maxInput int
	// start is the Id of the start rule of the stream parse, the one rule whose #stream repetition
	// streams.
	start int
	// Emit receives the committed elements of the start rule's #stream
	// repetition with the parse's budget control. Nil collects the repetition
	// as a batch parse does.
	Emit func(*sxirvalue.Node, *BudgetControl) error
	// memoBase is the position of memo.slots[0], the floor of the memo: the
	// entries before it were discarded.
	memoBase int
	// scratch is the capture scratch in use when the #stream repetition
	// started. Frames allocated after it belong to committed elements and
	// are reused.
	scratch frameScratchMark
	// nodeChunks counts the node chunks started since the last split.
	nodeChunks int
	// diagnostic receives the committed recovered errors; p.diagnosed counts
	// the entries of p.Recovered already delivered to it or to tail.
	diagnostic func(*sxirvalue.SyntaxError) error
	tail       streamDiagnosticTail
	text       streamTextSnapshot
	// lines holds the line starts once the whole input is loaded and
	// nothing was discarded.
	lines []int
	// nlStarts holds the positions after each newline in [base, nlScanned) of the buffer, in
	// ascending order; nlScanned is the position up to which streamLineCol has scanned.
	nlStarts  []int
	nlScanned int
}

const streamTextChunk = 1024

// streamTextSnapshot owns a small immutable copy of already loaded input, so
// that the texts of nearby nodes share one allocation. Returned substrings
// retain at most one chunk; offsets (CodePoints) remain parser-owned.
type streamTextSnapshot struct {
	base, end int
	source    string
	offsets   []int32
}

// frameScratchMark is the allocation state of a frameScratch.
type frameScratchMark struct{ frameN, slotCI, slotOff int }

// rewind clears and reuses the frames and slots allocated after m.
func (a *frameScratch) rewind(m frameScratchMark) {
	for i := m.frameN; i < a.frameN; i++ {
		a.frames[i/nodeChunk][i%nodeChunk] = CaptureFrame{}
	}
	for ci := m.slotCI; ci <= a.slotCI && ci < len(a.slots); ci++ {
		lo, hi := 0, ptrChunk
		if ci == m.slotCI {
			lo = m.slotOff
		}
		if ci == a.slotCI {
			hi = a.slotOff
		}
		if lo < hi {
			clear(a.slots[ci][lo:hi])
		}
	}
	a.frameN, a.slotCI, a.slotOff = m.frameN, m.slotCI, m.slotOff
}

// unitsReader is the reader of a stream whose parse counts logical compute units and observes a
// context or a memory limit. A read of the caller's reader is no event: it counts no unit. Before the
// read it observes the context, then the memory count; after it, the context again, where an error
// other than the end of the input that the read returned with its data wins over a cancellation
// first observed then (spec/runtime/parsing.md, Checks and Interruptions).
type unitsReader struct {
	reader io.Reader
	p      *Parser
}

func (r *unitsReader) Read(dst []byte) (int, error) {
	p := r.p
	p.unitsObserve()
	n, err := r.reader.Read(dst)
	if ctx := p.units.ctx; ctx != nil {
		if cerr := contextErr(ctx); cerr != nil {
			if err != nil && err != io.EOF {
				cerr = err
			}
			panic(fatal{err: cerr})
		}
		p.units.obsAt = p.Units + budgetCheckInterval
		p.setUnitsCheck()
	}
	return n, err
}

// RunParseStream parses the input read from reader with rule r, passing each
// committed element of the rule's #stream repetition to emit.
//
// RunParseStream is for generated parsers; not a stable API.
func RunParseStream(r *Rule, reader io.Reader, emit func(*sxirvalue.Node) error, opts []sxirruntime.ParseOption) error {
	if witnessFeatures {
		useFeature(r.g, featureStream)
	}
	var e func(*sxirvalue.Node, *BudgetControl) error
	if emit != nil {
		e = func(n *sxirvalue.Node, _ *BudgetControl) error { return emit(n) }
	}
	return RunParseStreamWithBudget(r, reader, e, opts)
}

// RunParseStreamWithBudget is RunParseStream with emit also given the parse's budget
// control, so that converting elements to typed values is charged to it.
//
// RunParseStreamWithBudget is for generated parsers; not a stable API.
func RunParseStreamWithBudget(r *Rule, reader io.Reader, emit func(*sxirvalue.Node, *BudgetControl) error, opts []sxirruntime.ParseOption) (err error) {
	if witnessFeatures {
		useFeature(r.g, featureStream)
	}
	o, err := resolveEntryOptions(r.g, opts, true)
	if err != nil {
		return err
	}
	if emit == nil && (o.diagnostic != nil || o.diagnosticLimit > 0) {
		// A diagnostic-only stream still needs element commit boundaries.
		emit = func(*sxirvalue.Node, *BudgetControl) error { return nil }
	}
	var n *sxirvalue.Node
	defer recoverParse(&n, &err)
	o = prepareOptions(o, 0, true)
	p := &Parser{unit: o.unit, MaxDepth: o.maxDepth}
	p.setGrammar(r.g)
	p.Expectations = o.expectations
	p.setControl(o.control)
	p.startUnits(o)
	defer p.reportUnits()
	if p.units.on && p.units.observes() {
		reader = &unitsReader{reader: reader, p: p}
	}
	p.Observation = newObservation(&o.observe, &p.Silent, nil)
	if usedFeatures&featureObserve != 0 && p.Observation != nil {
		defer p.releaseObservation()
	}
	p.setTracking()
	p.Stream = &streamInput{reader: bufio.NewReader(reader), baseLine: 1, baseCol: 1, Emit: emit, diagnostic: o.diagnostic, start: r.Id}
	p.Stream.tail.limit = o.diagnosticLimit
	p.Stream.maxInput = o.maxInput
	p.setDeferMemo()
	o.control.attachMemory(p)
	p.Control.Check()
	_, ok := p.Call(r.forParse(p), 0)
	p.Control.Check()
	atEnd := ok && p.streamAtEnd()
	p.endUnits(!atEnd)
	if atEnd {
		// Recoveries in the successful suffix are reported before the result.
		if err := p.reportRecovered(); err != nil {
			return err
		}
		if p.Stream.tail.limit > 0 {
			return p.Stream.tail.result(p)
		}
		if len(p.Recovered) > 0 {
			return sxirvalue.SyntaxErrors(p.recoveredErrors())
		}
		return nil
	}
	if ok {
		p.Expect(p.Pos, IdEndInput)
	}
	return p.MakeError(p.farthest, p.exp[p.expBase:])
}

// StreamStart is called when the start rule's #stream repetition starts.
// The capture frames allocated so far, including the start rule's, stay in
// use; elements allocate their nodes from chunks of their own.
func (p *Parser) StreamStart() {
	if nodeFrameScratch {
		a := &p.captureScratch
		p.Stream.scratch = frameScratchMark{a.frameN, a.slotCI, a.slotOff}
	} else {
		p.frameSlab = nil
	}
	p.nodeSlab, p.ptrSlab, p.fieldSlab = nil, nil, nil
	p.Stream.nodeChunks = 0
	// The chunks of the prefix stay in use for the whole stream; commits return only those taken
	// since.
	p.memKept += p.memUsed
	p.memUsed = 0
}

// streamReport delivers the recovered errors committed since the last report; an error of the
// diagnostic callback ends the parse with it, before the element is delivered.
func (p *Parser) streamReport() {
	if err := p.reportRecovered(); err != nil {
		panic(fatal{err})
	}
}

// streamCheck observes the context, then the memory count, around the delivery of an element: before
// the report and the delivery, and after the callback returns (spec/runtime/parsing.md, Checks and
// Interruptions, Streams).
func (p *Parser) streamCheck() {
	if p.units.on {
		p.unitsObserve()
		return
	}
	p.Control.Check()
}

// streamDeliver passes the element v to the callback of the stream parse; v is final from then on,
// and a callback error ends the parse unchanged.
func (p *Parser) streamDeliver(v *sxirvalue.Node) {
	if v != nil {
		D(v).Fresh = false
	}
	c, done := conversionControl(p.units.ctx, p.Control)
	defer done()
	p.streamFail(p.Stream.Emit(v, c))
}

// streamFail ends the parse with the error err of the stream callback, if there is one.
func (p *Parser) streamFail(err error) {
	if err != nil {
		panic(fatal{err})
	}
}

// streamRelease releases what the committed elements held, for streamCommit.
func (p *Parser) streamRelease() {
	s := p.Stream
	p.Trail = p.Trail[:0]
	// Capture frames are referenced only by active calls, and the elements'
	// calls have returned, so the frames allocated since the repetition
	// started can be reused.
	if nodeFrameScratch {
		p.captureScratch.rewind(s.scratch)
	}
	// A chunk stays alive while anything in it is referenced and keeps alive
	// what its objects point to, so chunks shared by consecutive elements
	// would chain every emitted element to the parser. New chunks start once
	// the node chunk is nearly used up or more than one chunk was started.
	if len(p.nodeSlab) < nodeChunk/8 || s.nodeChunks >= 2 {
		letGo := p.memUsed
		p.nodeSlab, p.ptrSlab, p.fieldSlab = nil, nil, nil
		if !nodeFrameScratch {
			p.frameSlab = nil
		}
		s.nodeChunks = 0
		// The chunks let go of hold the elements delivered so far, and so does the text copied
		// for them: their bytes are returned, except those of the expectation chunk in use and
		// the frame chunk kept.
		p.memUsed = uint64(cap(p.arena))*expIDBytes + uint64(cap(p.frameSlab))*frameBytes
		if p.countsMemory() {
			// The text snapshot in use stays held; the values before it were delivered.
			letGo += p.Control.values
			p.Control.values = 0
			if s.text.source != "" {
				p.Control.values = uint64(len(s.text.source)) + uint64(cap(s.text.offsets))*4
			}
			// A memo entry or skip record keeps the nodes, text and strings of the elements it was
			// built in until it is pruned, so the bytes stay counted until then (retire).
			p.Control.retire(letGo - min(letGo, p.memUsed+p.Control.values))
		}
	}
}

// streamDropRecords discards what the parse keeps about the positions before pos besides the memo,
// which no later call reads, for streamCommit.
func (p *Parser) streamDropRecords(pos int) {
	// Completed calls may have shortened the trail before the commit;
	// its inactive capacity can still refer to committed elements.
	clear(p.Trail[:cap(p.Trail)])
	p.pruneSkipRecords(pos)
	p.pruneScanRecords(pos)
	p.Observation.dropDiscarded(pos)
}

// streamSettleErrors forgets the recovered errors before the boundary, for streamCommit.
func (p *Parser) streamSettleErrors() {
	// The recovered errors before the boundary were delivered and
	// collected, and no active call restores a longer prefix of them:
	// the start rule's call saved an empty one, and the element marks
	// are no longer active. Memo entries hold their errors as immutable
	// lists (recEntry), so clearing the record does not touch them.
	if cap(p.Recovered) <= streamRecoveryScratchMax {
		clear(p.Recovered[:cap(p.Recovered)])
		p.Recovered = p.Recovered[:0]
	} else {
		p.Recovered = nil
	}
	p.diagnosed, p.diagnosedSkip, p.diagnosedList = 0, 0, nil
	if p.countsMemory() {
		// With a diagnostic limit, the tail counts the errors it keeps, and only memo entries
		// may still hold the originals, until the next prune recounts them (recountMemoHeld);
		// without one, Recovered keeps the errors and errs keeps counting them.
		p.Control.memoHeld = saturatingAdd(p.Control.memoHeld, p.Control.errs)
		p.Control.errs = 0
	}
}

// reportRecovered delivers the recovered errors committed since the last
// call to the diagnostic callback and the retained tail. It runs only at
// stream boundaries, which recoveries in abandoned alternatives never reach.
func (p *Parser) reportRecovered() error {
	s := p.Stream
	if s.diagnostic == nil && s.tail.limit == 0 {
		return nil
	}
	return p.deliverRecovered()
}

// reportError delivers the recovered error original to the diagnostic callback, as a copy that the
// callback owns, and to the retained tail.
func (p *Parser) reportError(original *sxirvalue.SyntaxError) error {
	s := p.Stream
	p.Control.Check()
	if s.diagnostic != nil {
		e := *original
		e.Expected = append([]string(nil), e.Expected...)
		e.Messages = append([]string(nil), e.Messages...)
		if err := s.diagnostic(&e); err != nil {
			return err
		}
	}
	if s.tail.limit > 0 {
		s.tail.add(original)
	}
	p.Control.Check()
	return nil
}

// Keep only small recovery scratch slices across stream elements. The
// diagnostic tail, not this scratch, owns committed errors.
const streamRecoveryScratchMax = 16

// streamDiagnosticTail is populated only at stream boundaries. The ring
// avoids shifting a large tail for each committed recovery.
type streamDiagnosticTail struct {
	limit           int
	errors          sxirvalue.SyntaxErrors
	head            int
	total           uint64
	dropped         uint64
	truncated       bool
	countsSaturated bool
	// bytes counts the errors in errors (syntaxErrorSize), for the memory limit.
	bytes uint64
}

func (t *streamDiagnosticTail) add(e *sxirvalue.SyntaxError) {
	t.bytes += syntaxErrorSize(e)
	if t.total == math.MaxUint64 {
		t.countsSaturated = true
	} else {
		t.total++
	}
	if len(t.errors) < t.limit {
		t.errors = append(t.errors, e)
		return
	}
	t.truncated = true
	if t.dropped == math.MaxUint64 {
		t.countsSaturated = true
	} else {
		t.dropped++
	}
	t.bytes -= syntaxErrorSize(t.errors[t.head])
	t.errors[t.head] = e
	t.head++
	if t.head == t.limit {
		t.head = 0
	}
}

func (t *streamDiagnosticTail) result(p *Parser) error {
	p.Control.Check()
	if len(t.errors) == 0 {
		return nil
	}
	if !t.truncated {
		return t.errors
	}
	ordered := t.errors
	if t.head != 0 {
		ordered = append(append(make(sxirvalue.SyntaxErrors, 0, len(t.errors)), t.errors[t.head:]...), t.errors[:t.head]...)
	}
	p.Control.Check()
	return &sxirvalue.TruncatedSyntaxErrors{Errors: ordered, Total: t.total, Dropped: t.dropped, CountsSaturated: t.countsSaturated}
}

var errIteratorStop = errors.New("stream iterator stopped")

// StreamPull advances a stream parse synchronously to one committed element
// at a time. The standard library pull coroutine has no independently
// scheduled producer.
//
// StreamPull is for generated parsers; not a stable API.
type StreamPull[T any] struct {
	next     func() (T, bool)
	stop     func()
	terminal bool
	err      error
}

// IterateStream validates the options of a stream entry point of grammar g and returns a
// lazy iterator over the elements that run passes to emit.
//
// IterateStream is for generated parsers; not a stable API.
func IterateStream[T any](g *grammar, opts []sxirruntime.ParseOption, run func(emit func(T) error) error) (*StreamPull[T], error) {
	if err := validateStream(g, opts); err != nil {
		return nil, err
	}
	it := &StreamPull[T]{}
	it.next, it.stop = iter.Pull(func(yield func(T) bool) {
		it.err = run(emitTo(yield))
	})
	return it, nil
}

// IterateNodes validates the options of a stream entry point of grammar g and returns a lazy
// iterator over the nodes that run passes to emit.
//
// IterateNodes is for generated parsers; not a stable API.
func IterateNodes(g *grammar, opts []sxirruntime.ParseOption, run func(emit func(*sxirvalue.Node) error) error) (*sxirruntime.StreamIterator, error) {
	if err := validateStream(g, opts); err != nil {
		return nil, err
	}
	return sxirruntime.NewStreamIterator(func(yield func(*sxirvalue.Node) bool) error { return run(emitTo(yield)) }), nil
}

// validateStream validates the options of a stream entry point of grammar g.
func validateStream(g *grammar, opts []sxirruntime.ParseOption) error {
	_, err := resolveEntryOptions(g, opts, true)
	return err
}

// emitTo returns the emit function of a stream parse that passes each element to yield, and stops
// the parse once yield returns false.
func emitTo[T any](yield func(T) bool) func(T) error {
	return func(v T) error {
		if !yield(v) {
			return errIteratorStop
		}
		return nil
	}
}

func (it *StreamPull[T]) Next() (v T, ok bool, err error) {
	if it.terminal {
		return v, false, it.err
	}
	defer func() {
		if x := recover(); x != nil {
			// Pull propagates the original caller panic after the parser unwinds.
			it.terminal = true
			it.next, it.stop = nil, nil
			panic(x)
		}
	}()
	v, ok = it.next()
	if ok {
		return v, true, nil
	}
	it.terminal = true
	it.next, it.stop = nil, nil
	return v, false, it.err
}

func (it *StreamPull[T]) Close() {
	if it.terminal {
		return
	}
	it.terminal = true
	stop := it.stop
	it.next, it.stop = nil, nil
	defer func() { it.err = sxirruntime.ErrStreamClosed }()
	stop()
}

// streamDiscard discards the input before keep. The buffer holds read-ahead
// input, so it is compacted only once at least half of it can go.
func (p *Parser) streamDiscard(keep int) {
	s := p.Stream
	n := keep - s.base
	if n <= 0 || 2*n < len(s.in)+len(s.bs) {
		return
	}
	s.baseLine, s.baseCol = p.streamLineCol(keep)
	// Drop the line starts up to the new base; streamLineCol has scanned up to keep.
	k, _ := slices.BinarySearch(s.nlStarts, keep+1)
	s.nlStarts = s.nlStarts[:copy(s.nlStarts, s.nlStarts[k:])]
	if p.unit == sxirinput.Bytes {
		s.bs = s.bs[:copy(s.bs, s.bs[n:])]
	} else {
		s.in = s.in[:copy(s.in, s.in[n:])]
	}
	s.base = keep
}

// pruneStreamMemo discards the memo entries before pos and makes pos the floor of the memo, below
// which lookups find nothing and stores keep nothing (memo.prune).
func (p *Parser) pruneStreamMemo(pos int) {
	s, t := p.Stream, &p.memo
	k := pos - s.memoBase
	if k <= 0 {
		return
	}
	for _, head := range t.slots[:min(k, len(t.slots))] {
		for e := head; e != nil; {
			next := e.next
			*e = memoEntry{next: t.free}
			t.free = e
			e = next
		}
	}
	// The slots of the pruned positions are returned; the entries go to the free list, whose
	// reuse is not charged again.
	t.memBytes -= min(t.memBytes, uint64(min(k, len(t.slots)))*ptrBytes)
	if k >= len(t.slots) {
		clear(t.slots)
		t.slots = t.slots[:0]
	} else {
		n := copy(t.slots, t.slots[k:])
		clear(t.slots[n:])
		t.slots = t.slots[:n]
	}
	if t.index != nil {
		moved := make(map[int]memoIndex[memoEntry], len(t.index))
		t.indexBytes = 0
		for at, ix := range t.index {
			if at >= k {
				moved[at-k] = ix
				t.indexBytes += indexBytes(ix)
			}
		}
		t.index = moved
	}
	s.memoBase = pos
	p.releaseAtPrune(pos)
}

// streamLoaded returns the position of the end of the input read so far.
func (s *streamInput) loaded() int {
	return s.base + len(s.in) + len(s.bs)
}

// streamFill reads the input up to just before position n and reports whether
// it is available. Once the required amount has been read, it reads only what
// is already buffered and does not wait for more.
func (p *Parser) streamFill(n int) bool {
	p.unitsDemand(n)
	ok := p.streamFillTo(n)
	if max := p.Stream.maxInput; max > 0 && n > max && p.Stream.loaded() > max {
		// The outcome does not depend on how the reader chunks the stream: the parser needed the
		// unit at MaxInput and the stream holds it. loaded is at least n unless the stream ended.
		panic(fatal{err: &sxirruntime.LimitError{Kind: sxirruntime.LimitInput, Limit: uint64(max), Requested: uint64(min(n, p.Stream.loaded()))}})
	}
	return ok
}

func (p *Parser) streamFillTo(n int) bool {
	s := p.Stream
	for s.loaded() < n && !s.eof {
		p.streamRead()
		for s.reader.Buffered() > 0 {
			if p.unit == sxirinput.CodePoints {
				if b, _ := s.reader.Peek(s.reader.Buffered()); !utf8.FullRune(b) {
					break
				}
			}
			p.streamRead()
		}
	}
	return s.loaded() >= n
}

func (p *Parser) streamRead() {
	p.Control.Check()
	s := p.Stream
	var err error
	if p.unit == sxirinput.Bytes {
		var b byte
		if b, err = s.reader.ReadByte(); err == nil {
			s.bs = append(s.bs, b)
		}
	} else {
		var r rune
		if r, _, err = s.reader.ReadRune(); err == nil {
			s.in = append(s.in, r)
		}
	}
	if err == io.EOF {
		s.eof = true
	} else if err != nil {
		panic(fatal{err})
	}
}

// streamPeek is peek on a stream. A complete character never waits for more
// input; an incomplete UTF-8 prefix waits for more bytes or the end of input.
func (p *Parser) streamPeek() (rune, int, bool) {
	s := p.Stream
	if p.Pos < s.base || !p.streamFill(p.Pos+1) {
		return 0, 0, false
	}
	if p.unit == sxirinput.CodePoints {
		return s.in[p.Pos-s.base], 1, true
	}
	b := s.bs[p.Pos-s.base:]
	if b[0] < utf8.RuneSelf {
		return rune(b[0]), 1, true
	}
	for !s.eof && !utf8.FullRune(b) {
		p.streamFillTo(s.loaded() + 1)
		b = s.bs[p.Pos-s.base:]
	}
	r, size := utf8.DecodeRune(b)
	return r, size, true
}

// The traced forms of the reads below are what the traced rule bodies call: an observed parse
// reports the exact examined end of every call, and a stream records it only in them, so that
// the plain bodies of a stream parse read as they would without observability. Outside a stream
// each one is the plain read, which records the examined end of a parse that tracks it.

// PeekTraced is Peek in a traced rule body.
//
// PeekTraced is for generated parsers; not a stable API.
func (p *Parser) PeekTraced() (rune, int, bool) {
	if p.Stream == nil {
		return p.Peek()
	}
	r, size, ok := p.streamPeek()
	end := p.Pos + size
	if !ok {
		end = p.Pos + 1
	} else if size == 1 && r == utf8.RuneError && p.unit == sxirinput.Bytes {
		end = decodedEndBytes(p.Stream.bs[p.Pos-p.Stream.base:], p.Pos, size)
	}
	p.documentTouch(end)
	return r, size, ok
}

// MatchLiteralTraced is MatchLiteral in a traced rule body.
//
// MatchLiteralTraced is for generated parsers; not a stable API.
func (p *Parser) MatchLiteralTraced(rs []rune, text string, desc ExpID, build bool) (*sxirvalue.Node, bool) {
	if p.Stream == nil {
		return p.MatchLiteral(rs, text, desc, build)
	}
	start := p.Pos
	s := p.Stream
	if p.unit == sxirinput.Bytes {
		for i := 0; i < len(text); i++ {
			if c, ok := p.streamByte(p.Pos); !ok || c != text[i] {
				p.documentTouch(p.Pos + 1)
				p.Pos = start
				return p.matchReplacementLiteralTraced(rs, text, desc, build)
			}
			p.Pos++
		}
	} else {
		for _, r := range rs {
			if p.Pos < s.base || !p.streamFill(p.Pos+1) || s.in[p.Pos-s.base] != r {
				p.documentTouch(p.Pos + 1)
				p.Pos = start
				p.Expect(start, desc)
				return nil, false
			}
			p.Pos++
		}
	}
	p.documentTouch(p.Pos)
	if !build {
		return nil, true
	}
	return p.buildNode(Data{Kind: valueimpl.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.LiteralText(start, text), Terminal: true, Fresh: true}), true
}

// matchReplacementLiteralTraced is matchReplacementLiteral on a stream in a traced rule body.
func (p *Parser) matchReplacementLiteralTraced(rs []rune, text string, desc ExpID, build bool) (*sxirvalue.Node, bool) {
	start := p.Pos
	if !p.literalHasReplacement(rs) {
		p.Expect(start, desc)
		return nil, false
	}
	for _, want := range rs {
		size, ok := p.MatchCharTraced(want, false)
		if !ok {
			p.Expect(start, desc)
			p.Pos = start
			return nil, false
		}
		p.Pos += size
	}
	if !build {
		return nil, true
	}
	return p.buildNode(Data{Kind: valueimpl.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.LiteralText(start, text), Terminal: true, Fresh: true}), true
}

// MatchLiteralFoldTraced is MatchLiteralFold in a traced rule body.
//
// MatchLiteralFoldTraced is for generated parsers; not a stable API.
func (p *Parser) MatchLiteralFoldTraced(rs []rune, desc ExpID, build bool) (*sxirvalue.Node, bool) {
	if p.Stream == nil {
		return p.MatchLiteralFold(rs, desc, build)
	}
	start := p.Pos
	for _, want := range rs {
		size, ok := p.MatchCharTraced(want, true)
		if !ok {
			p.Expect(start, desc)
			return nil, false
		}
		p.Pos += size
	}
	if !build {
		return nil, true
	}
	return p.buildNode(Data{Kind: valueimpl.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.Text(start, p.Pos), Terminal: true, Fresh: true}), true
}

// AtLineEndTraced is AtLineEnd in a traced rule body.
//
// AtLineEndTraced is for generated parsers; not a stable API.
func (p *Parser) AtLineEndTraced() bool {
	if p.Stream == nil {
		return p.AtLineEnd()
	}
	p.documentTouch(p.Pos + 1)
	return p.lineEnd()
}

// decodedEndBytes is decodedEnd for the loaded bytes b of a stream.
func decodedEndBytes(b []byte, pos, size int) int {
	if size > 1 {
		return pos + size
	}
	k := 1
	for k <= len(b) && !utf8.FullRune(b[:k]) {
		k++
	}
	return pos + k
}

// streamByte returns the byte at position i (Bytes).
func (p *Parser) streamByte(i int) (byte, bool) {
	s := p.Stream
	if i < s.base || !p.streamFill(i+1) {
		return 0, false
	}
	return s.bs[i-s.base], true
}

// streamMatchLiteral is MatchLiteral on a stream. It reads only as far as the
// literal matches.
func (p *Parser) streamMatchLiteral(rs []rune, text string, desc ExpID, build bool) (*sxirvalue.Node, bool) {
	start := p.Pos
	s := p.Stream
	if p.unit == sxirinput.Bytes {
		for i := 0; i < len(text); i++ {
			if c, ok := p.streamByte(p.Pos); !ok || c != text[i] {
				p.Pos = start
				return p.matchReplacementLiteral(rs, text, desc, build)
			}
			p.Pos++
		}
	} else {
		for _, r := range rs {
			if p.Pos < s.base || !p.streamFill(p.Pos+1) || s.in[p.Pos-s.base] != r {
				p.Pos = start
				p.Expect(start, desc)
				return nil, false
			}
			p.Pos++
		}
	}
	if !build {
		return nil, true
	}
	return p.buildNode(Data{Kind: valueimpl.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.LiteralText(start, text), Terminal: true, Fresh: true}), true
}

func (p *Parser) streamAtLineStart() bool {
	if p.Pos == 0 {
		return true
	}
	if p.unit == sxirinput.Bytes {
		c, ok := p.streamByte(p.Pos - 1)
		return ok && c == '\n'
	}
	s := p.Stream
	i := p.Pos - 1
	return i >= s.base && p.streamFill(i+1) && s.in[i-s.base] == '\n'
}

func (p *Parser) streamAtEnd() bool {
	return !p.streamFill(p.Pos + 1)
}

// rangeText is node.rangeText: the input of the range [start, end) of a node. In a stream whose parse
// counts logical compute units, the text of a node is an event of one unit for each position unit of
// its result (spec/runtime/parsing.md, Compute Units), which it counts before it makes the text.
func (p *Parser) rangeText(start, end int) string {
	if p.Stream != nil && p.units.on {
		f := p.Stream.textFloor
		p.unitsSized(max(end, f) - max(start, f))
	}
	return p.Text(start, end)
}

// heldText is node.heldText: the text that the terminal node n holds, and whether it holds one. In a
// stream, the text that text takes from a string node is the event that rangeText counts, of the
// width of the node's range; an error node, whose text is the input of its range, counts there.
func (p *Parser) heldText(n *sxirvalue.Node) (string, bool) {
	d := D(n)
	if d.Terminal && p.Stream != nil && p.units.on && shape(n) != valueimpl.ShapeError {
		p.unitsSized(int(d.End - d.Start))
	}
	return d.Text, d.Terminal
}

// streamText returns owned text between start and end, cut at the last commit
// point (textFloor) whatever input before it the buffer still holds.
func (p *Parser) streamText(start, end int) string {
	s := p.Stream
	start, end = max(start, s.textFloor), max(end, s.textFloor)
	if start == end {
		return ""
	}
	c := &s.text
	if start >= c.base && end <= c.end && c.source != "" {
		if p.unit == sxirinput.Bytes {
			return c.source[start-c.base : end-c.base]
		}
		return c.source[c.offsets[start-c.base]:c.offsets[end-c.base]]
	}
	// Do not read ahead for text or repeatedly copy tiny reader fragments.
	// Oversized ranges are copied on their own.
	if end-start > streamTextChunk || s.loaded()-start < 64 {
		return p.streamCopy(start, end)
	}
	snap := streamTextSnapshot{base: start, end: start + min(streamTextChunk, s.loaded()-start)}
	snap.source = p.streamCopy(snap.base, snap.end)
	if p.unit == sxirinput.Bytes {
		*c = snap
		return snap.source[:end-start]
	}
	if p.countsMemory() {
		// An interruption while filling must not leave partial offsets in
		// the snapshot in use, so a controlled parse fills separate storage.
		snap.offsets = make([]int32, snap.end-snap.base+1)
		p.chargeValueBytes(4 * len(snap.offsets))
	} else if n := snap.end - snap.base + 1; cap(c.offsets) >= n {
		snap.offsets = c.offsets[:n]
	} else {
		snap.offsets = make([]int32, n)
	}
	i := 0
	for off := range snap.source {
		snap.offsets[i] = int32(off)
		i++
	}
	snap.offsets[i] = int32(len(snap.source))
	*c = snap
	return snap.source[:snap.offsets[end-start]]
}

// streamCopy copies the loaded text between start and end.
func (p *Parser) streamCopy(start, end int) string {
	s := p.Stream
	start, end = start-s.base, end-s.base
	if p.unit == sxirinput.Bytes {
		p.chargeValueBytes(end - start)
		return string(s.bs[start:end])
	}
	text := string(s.in[start:end])
	p.chargeValueBytes(len(text))
	return text
}

// streamLineCol returns the line and column of pos, counted from base.
func (p *Parser) streamLineCol(pos int) (int, int) {
	s := p.Stream
	if s.eof && s.base == 0 {
		// The whole input is loaded: look the line up in a table of line
		// starts, built on first use, instead of scanning for each error.
		if s.lines == nil {
			s.lines = []int{0}
			for i := range s.loaded() {
				if p.unit == sxirinput.Bytes && s.bs[i] == '\n' || p.unit == sxirinput.CodePoints && s.in[i] == '\n' {
					s.lines = append(s.lines, i+1)
				}
			}
		}
		pos = min(pos, s.loaded())
		lo, hi := 0, len(s.lines) // the number of line starts at or before pos
		for lo < hi {
			if m := int(uint(lo+hi) >> 1); s.lines[m] <= pos {
				lo = m + 1
			} else {
				hi = m
			}
		}
		return lo, pos - s.lines[lo-1] + 1
	}
	// A stream buffer: scan each unit once, whatever the order of the queries, and look the
	// position up in the line starts found so far.
	pos = max(min(pos, s.loaded()), s.base)
	s.nlScanned = max(s.nlScanned, s.base)
	for ; s.nlScanned < pos; s.nlScanned++ {
		var nl bool
		if p.unit == sxirinput.Bytes {
			nl = s.bs[s.nlScanned-s.base] == '\n'
		} else {
			nl = s.in[s.nlScanned-s.base] == '\n'
		}
		if nl {
			s.nlStarts = append(s.nlStarts, s.nlScanned+1)
		}
	}
	// k is the number of line starts at or before pos.
	k, _ := slices.BinarySearch(s.nlStarts, pos+1)
	if k == 0 {
		return s.baseLine, s.baseCol + pos - s.base
	}
	return s.baseLine + k, pos - s.nlStarts[k-1] + 1
}
