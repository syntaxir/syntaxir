package libgo

import (
	"math"
	"unsafe"

	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// Sizes of the elements that the memory counters charge (budget_memory.go) and that both parsers
// define alike. The sizes of the types each parser defines for itself, such as its nodes, frames and
// memo entries, are beside its counters.
const (
	ptrBytes    = uint64(unsafe.Sizeof((*sxirvalue.Node)(nil)))
	fieldBytes  = uint64(unsafe.Sizeof(valueimpl.NodeField{}))
	anyBytes    = uint64(unsafe.Sizeof(any(nil)))
	intBytes    = uint64(unsafe.Sizeof(0))
	stringBytes = uint64(unsafe.Sizeof(""))
	// syntaxErrorBytes is a syntax error; the lists that hold its pointer count the pointer.
	syntaxErrorBytes = uint64(unsafe.Sizeof(sxirvalue.SyntaxError{}))
	expIDBytes       = uint64(unsafe.Sizeof(ExpID(0)))
	// internBytes is the value of an entry of the map of intern, besides the bytes of its key.
	internBytes = stringBytes + 8
	// mapBytes is the header of a map; an element of a map is counted as its key and value.
	mapBytes = 48
	// memoIndexEntryBytes is an element of a hash index of a memo chain, and of the map of indexes.
	memoIndexEntryBytes = 16
)

// memoryCounts are the memory counters that a BudgetControl keeps itself; the memory check adds them
// to the counters and buffers of the parser it reads.
type memoryCounts struct {
	// retained is the memory, in bytes, charged to the control itself: the bytes that a Document
	// retains from earlier parses outside the memo table of the parse.
	retained uint64
	// values counts the storage of the values the parse builds on the controlled path: text copied
	// out of the input, concatenated strings and rebound environments. It belongs to the nodes and
	// memo entries built since, so a stream returns it with their chunks.
	values uint64
	// errs counts the syntax errors the parse builds. A stream moves it to memoHeld at a commit that
	// releases the recovered errors to the diagnostic tail, which counts those it keeps; otherwise
	// Recovered keeps the errors to the end of the parse. It is not reduced when a backtrack drops
	// recovered errors, so it can exceed what the parse still holds: the memory limit sees an
	// overcount, not an undercount.
	errs uint64
	// memoHeld counts what memo entries, skip records and the record of recovered errors keep
	// beyond a commit: the lists of recovered errors, the errors once a commit has moved them, and
	// the keys of memo entries. Those holders share these objects and stay until a memo prune, so a
	// commit does not return it: a stream replaces it at a prune with the bytes reachable from the
	// holders that remain (Parser.recountMemoHeld). Between prunes it can exceed what the parse
	// holds, never fall below it.
	memoHeld uint64
	// pending counts the bytes of chunks and values that a commit let go of, but that a memo entry
	// or skip record may still reference, so a stream returns them at a prune instead (retire). A
	// memo entry keeps the nodes, text and strings of the elements it was built in until it is
	// pruned.
	pending uint64
	// retired lists them by commit, oldest first, with the highest position of any memo entry or
	// skip record stored by then (memoHigh).
	retired []retiredBytes
	// memoHigh is the highest position at which a memo entry or skip record has been stored.
	memoHigh int
}

// counted returns the sum of the counters.
func (m *memoryCounts) counted() uint64 {
	n := saturatingAdd(saturatingAdd(m.retained, m.values), saturatingAdd(m.errs, m.memoHeld))
	return saturatingAdd(n, m.pending)
}

// retiredBytes is what one commit let go of: its bytes, and the highest position of any memo entry
// or skip record stored by then.
type retiredBytes struct {
	bytes uint64
	high  int
}

// retire keeps n bytes that a commit let go of counted until a prune: the nodes, text and strings
// of the delivered elements stay referenced by the memo entries and skip records that hold them. A
// parse without a memory limit has no count to keep, and its caller does not call it.
func (m *memoryCounts) retire(n uint64) {
	if n == 0 {
		return
	}
	m.retired = append(m.retired, retiredBytes{bytes: n, high: m.memoHigh})
	m.pending = saturatingAdd(m.pending, n)
}

// noteStored records that a memo entry or skip record starts at pos.
func (m *memoryCounts) noteStored(pos int) {
	if pos > m.memoHigh {
		m.memoHigh = pos
	}
}

// releaseRetired returns, at a prune that discards the entries and records before pos, the bytes
// of the commits no remaining holder can reference. A holder that remains starts at pos or later,
// and every node it reaches starts at its own start or later. The nodes, text and strings that a
// commit let go of were built before it, so a remaining holder reaches them only through a memo
// entry or skip record stored before that commit and starting at pos or later, and no such holder
// exists if the highest position stored by then is below pos. Commits are in order and the highest
// position never falls, so the commits to return are the oldest ones.
func (m *memoryCounts) releaseRetired(pos int) {
	n := 0
	for n < len(m.retired) && m.retired[n].high < pos {
		m.pending -= min(m.pending, m.retired[n].bytes)
		n++
	}
	m.retired = m.retired[:copy(m.retired, m.retired[n:])]
}

// chargeMemoHeld charges n bytes that memo entries or the record of recovered errors keep: a list
// of recovered errors or the key of a memo entry (memoryCounts.memoHeld).
func (p *Parser) chargeMemoHeld(n uint64) {
	if p.countsMemory() {
		p.Control.memoHeld = saturatingAdd(p.Control.memoHeld, n)
	}
}

// releaseAtPrune settles what a stream's memo holds when it prunes the memo before pos: it returns the
// bytes that commits let go of and no remaining holder reaches (releaseRetired), and recounts what the
// holders keep (recountMemoHeld, over the memo that each parser provides and the skip tables).
func (p *Parser) releaseAtPrune(pos int) {
	if p.countsMemory() {
		p.Control.releaseRetired(pos)
	}
	p.recountMemoHeld()
}

// chargeValueBytes charges n bytes of a value that only the controlled path builds
// (BudgetControl.values).
func (p *Parser) chargeValueBytes(n int) {
	if p.countsMemory() {
		p.Control.values = saturatingAdd(p.Control.values, uint64(n))
	}
}

// chargeError charges the syntax error e (BudgetControl.errs).
func (p *Parser) chargeError(e *sxirvalue.SyntaxError) {
	if p.countsMemory() {
		p.Control.errs = saturatingAdd(p.Control.errs, syntaxErrorSize(e))
	}
}

// syntaxErrorSize returns the bytes of the syntax error e and its lists.
func syntaxErrorSize(e *sxirvalue.SyntaxError) uint64 {
	return syntaxErrorBytes + uint64(cap(e.Expected)+cap(e.Messages))*stringBytes
}

// saturatingAdd returns a+b, or the largest count if the sum overflows.
func saturatingAdd(a, b uint64) uint64 {
	if math.MaxUint64-a < b {
		return math.MaxUint64
	}
	return a + b
}
