package state

import (
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// The engine's names, for its other packages, of the mechanisms it shares with the library of
// generated parsers (sharedsrc). The shared files use the library's names, most of which are not
// exported.

// RecEntry is an entry of the record of recovered errors, or the errors that a result holds
// (recEntry).
type RecEntry = recEntry

// ErrList is a list of recovered errors that results and the record share (errList).
type ErrList = errList

// ErrEntry returns the entry of the error e.
func ErrEntry(e *value.SyntaxError) RecEntry { return RecEntry{err: e} }

// None reports whether the entry holds no errors.
func (e recEntry) None() bool { return e.none() }

// Len returns the number of errors of the entry.
func (e recEntry) Len() int { return e.len() }

// AppendErrors appends the errors of the entry to dst, in order.
func (e recEntry) AppendErrors(dst []*value.SyntaxError) []*value.SyntaxError {
	return appendRecErrors(dst, []RecEntry{e})
}

// Errors returns the errors of the entry, in order.
func (e recEntry) Errors() []*value.SyntaxError { return e.AppendErrors(nil) }

// List returns the list that the entry is, or nil for an error or no errors.
func (e recEntry) List() *ErrList { return e.list }

// RecoveredErrors returns the recovered errors of the record, in order.
func (p *Parser) RecoveredErrors() []*value.SyntaxError { return p.recoveredErrors() }

// NewErrList returns a list of the errors errs, each an entry of its own, as rec.since makes one. It
// is exported only as a hook for tests in other packages.
func NewErrList(errs []*value.SyntaxError) RecEntry {
	l := &ErrList{entries: make([]RecEntry, len(errs)), n: len(errs)}
	for i, e := range errs {
		l.entries[i] = RecEntry{err: e}
	}
	return RecEntry{list: l}
}

// SpendValue spends n units of value work (spendValue).
func (p *Parser) SpendValue(n int) { p.spendValue(n) }

// CompareText compares two strings bytewise, charging the comparison under a budget (compareText).
func (p *Parser) CompareText(l, r string) int { return p.compareText(l, r) }

// CopySpend copies src to dst, charging the copy under a budget (copySpend).
func CopySpend[T any](p *Parser, dst, src []T) { copySpend(p, dst, src) }

// ClearSpend clears values, charging the work under a budget (clearSpend).
func ClearSpend[T any](p *Parser, values []T) { clearSpend(p, values) }

// D returns the representation of the node n (nodes.D), under the name the shared files use.
func D(n *value.Node) *nodes.Data { return nodes.D(n) }

// Diagnosed returns how many entries of Recovered the stream has delivered to its diagnostic
// callback or tail. It is exported only as a hook for tests in other packages.
func (p *Parser) Diagnosed() int { return p.diagnosed }

// SetDiagnosed sets how many entries of Recovered the stream has delivered. It is exported only as a
// hook for tests in other packages.
func (p *Parser) SetDiagnosed(n int) { p.diagnosed = n }

// TracePanic is a panic of the trace function, with the value it panicked with.
type TracePanic = tracePanic
