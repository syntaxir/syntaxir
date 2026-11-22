package libgo

import (
	"fmt"
	"strings"
)

// The charges of the work on values. Strings and lists can come from actions without referring to
// the input, so the work of copying and clearing them is charged by itself, in chunks of
// spendValueChunk elements, so that a limit or a cancellation is seen between them. Allocation,
// buffer growth and the garbage collector remain nonpreemptible. Each such operation has an
// uncontrolled fast path, which a parse without a budget (p.controlled) takes, and a controlled path
// of its own. A comparison of strings counts the compute units of its values only (compareText).

// spendValueChunk is the number of elements of a value that one charge covers.
const spendValueChunk = 128

// chargeValues spends n units of value work in chunks, so that a limit or cancellation is seen
// between them.
func (p *Parser) chargeValues(n int) {
	for n > 0 {
		c := min(spendValueChunk, n)
		p.spendValue(c)
		n -= c
	}
}

// compareText compares two strings bytewise, as strings.Compare does, malformed UTF-8 included. A parse
// that counts compute units counts the length in bytes of the shorter string, whatever the strings
// share (spec/runtime/parsing.md, Cost).
func (p *Parser) compareText(l, r string) int {
	p.valueUnits(min(len(l), len(r)))
	return strings.Compare(l, r)
}

// TextEqual reports whether two strings are equal (the operation str.eq). A parse that counts compute
// units counts the length in bytes of the shorter string, as compareText does.
func (p *Parser) TextEqual(l, r string) bool {
	p.valueUnits(min(len(l), len(r)))
	return l == r
}

// checkedTextSize returns size+width, the size of a text that grows by width bytes, and ends the
// parse when it exceeds the largest string.
func checkedTextSize(size, width int) int {
	if size < 0 || width < 0 || size > int(^uint(0)>>1)-width {
		abortParse(fmt.Errorf("input text exceeds maximum string size"))
	}
	return size + width
}

// copySpend copies src to dst. The callers use disjoint storage; overlapping moves need their own
// direction-aware implementation rather than a chunked forward copy.
func copySpend[T any](p *Parser, dst, src []T) {
	if p.countsMemory() {
		copyBudgetControlled(p, dst, src)
		return
	}
	copy(dst, src)
}

func copyBudgetControlled[T any](p *Parser, dst, src []T) {
	for off, end := 0, min(len(dst), len(src)); off < end; {
		n := min(spendValueChunk, end-off)
		p.spendValue(n)
		copy(dst[off:off+n], src[off:off+n])
		off += n
	}
}

// appendSpend appends src to dst.
func appendSpend[T any](p *Parser, dst, src []T) []T {
	if p.countsMemory() {
		return appendBudgetControlled(p, dst, src)
	}
	return append(dst, src...)
}

func appendBudgetControlled[T any](p *Parser, dst, src []T) []T {
	for off := 0; off < len(src); {
		n := min(spendValueChunk, len(src)-off)
		p.spendValue(n)
		dst = append(dst, src[off:off+n]...)
		off += n
	}
	return dst
}

// clearSpend clears values.
func clearSpend[T any](p *Parser, values []T) {
	if p.countsMemory() {
		clearBudgetControlled(p, values)
		return
	}
	clear(values)
}

func clearBudgetControlled[T any](p *Parser, values []T) {
	for off := 0; off < len(values); {
		n := min(spendValueChunk, len(values)-off)
		p.spendValue(n)
		clear(values[off : off+n])
		off += n
	}
}
