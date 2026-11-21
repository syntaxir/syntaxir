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
	// out of the input, concatenated strings, rebound environments, memo keys and the error lists
	// of memo entries. It belongs to the nodes and memo entries built since, so a stream returns it
	// with their chunks.
	values uint64
	// errs counts the syntax errors the parse builds. A stream returns it at a commit that
	// releases the recovered errors to the diagnostic tail, which counts those it keeps; otherwise
	// the errors stay held to the end of the parse.
	errs uint64
}

// counted returns the sum of the counters.
func (m *memoryCounts) counted() uint64 {
	return saturatingAdd(saturatingAdd(m.retained, m.values), m.errs)
}

// chargeValueBytes charges n bytes of a value that only the controlled path builds
// (BudgetControl.values).
func (p *Parser) chargeValueBytes(n int) {
	if p.controlled() {
		p.Control.values = saturatingAdd(p.Control.values, uint64(n))
	}
}

// chargeError charges the syntax error e (BudgetControl.errs).
func (p *Parser) chargeError(e *sxirvalue.SyntaxError) {
	if p.controlled() {
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
