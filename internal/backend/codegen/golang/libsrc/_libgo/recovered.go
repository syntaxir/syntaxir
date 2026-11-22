package libgo

import (
	"unsafe"

	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// The record of recovered errors (Parser.Recovered) is a sequence of entries: errors that recoveries
// made, and immutable lists that the results of calls, growths and Operators Rule attempts share. A
// result keeps its errors as one entry (recEntry), which the record takes again when the result is
// reused, so that a reuse costs the same however many errors it holds. See addRecovery in
// internal/ir/code/exec/exec.go and Lemma 14 of spec/proofs/linear-cost.md.

// recEntry is an entry of the record of recovered errors, or the errors that a result holds: an
// error, a shared list, or, as the zero value, no errors.
type recEntry struct {
	err  *sxirvalue.SyntaxError
	list *errList
}

// errList is a list of recovered errors: the entries that one recSince took. It never changes once
// made, so results and the record share it.
type errList struct {
	entries []recEntry
	n       int // the errors of the entries, an error counted where it occurs
}

// recEntryBytes is the size of an entry, for the memory limit.
const recEntryBytes = uint64(unsafe.Sizeof(recEntry{}))

// errListBytes is the size of a list without its entries, for the memory limit.
const errListBytes = uint64(unsafe.Sizeof(errList{}))

// none reports whether the entry holds no errors.
func (e recEntry) none() bool { return e.err == nil && e.list == nil }

// len returns the number of errors of the entry.
func (e recEntry) len() int {
	switch {
	case e.list != nil:
		return e.list.n
	case e.err != nil:
		return 1
	}
	return 0
}

// appendRecErrors appends the errors of the entries to dst, in order. It unrolls the lists without
// recursion, since they nest as deep as the steps of a growth go; listing k errors visits at most 2k
// entries (Lemma 14).
func appendRecErrors(dst []*sxirvalue.SyntaxError, entries []recEntry) []*sxirvalue.SyntaxError {
	type frame struct {
		entries []recEntry
		i       int
	}
	var stack []frame
	cur := frame{entries: entries}
	for {
		if cur.i == len(cur.entries) {
			if len(stack) == 0 {
				return dst
			}
			cur, stack = stack[len(stack)-1], stack[:len(stack)-1]
			continue
		}
		x := cur.entries[cur.i]
		cur.i++
		switch {
		case x.list != nil:
			stack = append(stack, cur)
			cur = frame{entries: x.list.entries}
		case x.err != nil:
			dst = append(dst, x.err)
		}
	}
}

// errReach counts the bytes of the lists, and optionally of the errors, that entries reach, each
// object once: lists share their parts, so the walk keeps the objects it has met. It is the
// recount of BudgetControl.memoHeld, which memory limits make at a memo prune. Its own sets are
// working space of the check and are not counted.
type errReach struct {
	lists  map[*errList]struct{}
	errors map[*sxirvalue.SyntaxError]struct{}
	stack  []recEntry
	// withErrors includes the errors themselves; otherwise only the lists count.
	withErrors bool
	bytes      uint64
}

// add counts what the entries reach.
func (r *errReach) add(entries ...recEntry) {
	r.stack = append(r.stack, entries...)
	for len(r.stack) > 0 {
		x := r.stack[len(r.stack)-1]
		r.stack = r.stack[:len(r.stack)-1]
		switch {
		case x.list != nil:
			if _, met := r.lists[x.list]; met {
				continue
			}
			if r.lists == nil {
				r.lists = make(map[*errList]struct{})
			}
			r.lists[x.list] = struct{}{}
			r.bytes += errListBytes + uint64(cap(x.list.entries))*recEntryBytes
			r.stack = append(r.stack, x.list.entries...)
		case x.err != nil && r.withErrors:
			if _, met := r.errors[x.err]; met {
				continue
			}
			if r.errors == nil {
				r.errors = make(map[*sxirvalue.SyntaxError]struct{})
			}
			r.errors[x.err] = struct{}{}
			r.bytes += syntaxErrorSize(x.err)
		}
	}
}

// countRecErrors returns the number of errors of the entries.
func countRecErrors(entries []recEntry) int {
	n := 0
	for _, e := range entries {
		n += e.len()
	}
	return n
}

// recoveredErrors returns the recovered errors of the record, in order.
func (c *core) recoveredErrors() []*sxirvalue.SyntaxError {
	if len(c.Recovered) == 0 {
		return nil
	}
	return appendRecErrors(make([]*sxirvalue.SyntaxError, 0, len(c.Recovered)), c.Recovered)
}

// recSince returns the errors recovered after the mark m (rec.since), charging one unit for each
// entry after it: none, the one entry after it, an error or a list, or a new list of the entries
// after it, which then takes their place in the record. A mark after m is no longer valid.
func (p *Parser) recSince(m int) recEntry {
	if len(p.Recovered) == m {
		return recEntry{}
	}
	return p.recSinceSlow(m)
}

func (p *Parser) recSinceSlow(m int) recEntry {
	if len(p.Recovered)-m == 1 {
		p.spendValue(1)
		return p.Recovered[m]
	}
	after := p.Recovered[m:]
	l := &errList{entries: appendSpend(p, make([]recEntry, 0, len(after)), after), n: countRecErrors(after)}
	p.chargeMemoHeld(errListBytes + uint64(cap(l.entries))*recEntryBytes)
	p.compactDiagnosed(m, l)
	p.Recovered = append(p.Recovered[:m], recEntry{list: l})
	return recEntry{list: l}
}

// recAdd records the errors of a result again, as one entry, at no cost (rec.add).
func (c *core) recAdd(e recEntry) {
	if !e.none() {
		c.Recovered = append(c.Recovered, e)
	}
}

// PushRecovered records the error e that a recovery made (rec.push). Generated parsers call it.
func (p *Parser) PushRecovered(e *sxirvalue.SyntaxError) {
	p.use(featureRecovery)
	p.Recovered = append(p.Recovered, recEntry{err: e})
}

// compactDiagnosed keeps the progress of the stream's reports (deliverRecovered) when the entries of
// the record from m become the list l. The reports deliver whole entries, and diagnosedSkip errors
// of the entry at diagnosed when it is diagnosedList, so a list that takes the place of reported
// entries counts their errors there. Without a stream, diagnosed is zero and nothing changes.
func (c *core) compactDiagnosed(m int, l *errList) {
	switch {
	case c.diagnosed > m:
		skip := c.partialDiagnosed() + countRecErrors(c.Recovered[m:min(c.diagnosed, len(c.Recovered))])
		c.diagnosed, c.diagnosedSkip, c.diagnosedList = m, skip, l
	case c.diagnosed == m && c.partialDiagnosed() > 0:
		c.diagnosedList = l
	}
}

// partialDiagnosed returns the errors of the entry at diagnosed that the stream has already
// reported.
func (c *core) partialDiagnosed() int {
	if c.diagnosedList == nil || c.diagnosed >= len(c.Recovered) || c.Recovered[c.diagnosed].list != c.diagnosedList {
		return 0
	}
	return c.diagnosedSkip
}

// deliverRecovered delivers the recovered errors committed since the last delivery to
// p.reportError, in order, and stops at the first error it returns. A stream runs it only at its
// boundaries, which recoveries in abandoned alternatives never reach.
func (p *Parser) deliverRecovered() error {
	if p.diagnosed > len(p.Recovered) {
		p.diagnosed = len(p.Recovered)
	}
	var errs []*sxirvalue.SyntaxError
	for p.diagnosed < len(p.Recovered) {
		if e := p.Recovered[p.diagnosed]; e.list == nil {
			// An error, delivered without listing it.
			if e.err != nil {
				if err := p.reportError(e.err); err != nil {
					// The error ends the parse.
					return err
				}
			}
			p.diagnosed++
			continue
		}
		errs = appendRecErrors(errs[:0], p.Recovered[p.diagnosed:p.diagnosed+1])
		for _, original := range errs[p.partialDiagnosed():] {
			if err := p.reportError(original); err != nil {
				return err
			}
		}
		p.diagnosed, p.diagnosedSkip, p.diagnosedList = p.diagnosed+1, 0, nil
	}
	return nil
}
