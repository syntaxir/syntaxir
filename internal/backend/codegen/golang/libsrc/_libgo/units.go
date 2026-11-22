package libgo

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync/atomic"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// Logical compute units (design record 076): a parser generated with them (Generator.Logical)
// counts the logical events of a parse in Parser.Units. The call protocol (Call, InvokePlain and
// the printed compositions they run) adds the calls of rules, their keys, the steps of growths and
// the entries of recovered errors taken; the generated code adds the calls it makes directly and the
// input that terminals advance over; the library adds what its matchers advance over and the other
// sized events (values built, string comparisons, the syntax error built). The counter is compared
// with the compute limit, and the context and the memory count are observed, only at checks: on
// cycles (calls of recursive rules, steps of growths and loop iterations), after sized events and at
// the end of the parse. The charges of the budget control are not made: the control of such a parse
// holds the memory limit only, which the checks observe.

// unitsState is the state of the counter of a parse with logical units.
type unitsState struct {
	on    bool
	limit uint64 // the resolved compute limit; 0 sets none
	// adaptive is set for a stream under its default limit, which rises with the furthest position
	// the parse demands (unitsDemand).
	adaptive bool
	ctx      context.Context
	// mem is the control that holds a finite memory limit, nil for none.
	mem *BudgetControl
	// obsAt is the count at which the next check observes the context and the memory count.
	obsAt uint64
	// polls counts the steps of work that count no unit (poll) since the last observation they made.
	polls uint64
	out   *uint64 // where the end of the parse reports the count (WithComputeReport)
	memo  *string // where the end of the parse reports the memo (SetMemoReport)
}

// memoReport is where the next parse with logical units stores the state of its memo (SetMemoReport).
var memoReport atomic.Pointer[string]

// SetMemoReport makes the parses that start after it, and that count logical units, store in *out,
// when they end without being stopped, the state of their memo that later calls read: the policy
// counts of each rule, the positions where each rule was called (the seen marks) and the stored
// entries, each chain in its order. A nil out stops the reports. It is a hook for the tests of
// logical units, which compare the memo between parsers that must leave the same state: it holds
// one destination for the whole process, so a test that uses it runs no parse beside it.
//
// SetMemoReport is for generated parsers; not a stable API.
func SetMemoReport(out *string) { memoReport.Store(out) }

// startUnits starts the counter of a parse with the options o.
func (p *Parser) startUnits(o parseOptions) {
	if !o.controls {
		p.units = unitsState{}
		p.UnitsCheck = math.MaxUint64
		return
	}
	p.Units = 0
	p.units = unitsState{on: true, limit: o.unitsLimit, adaptive: o.unitsAdaptive, ctx: o.unitsCtx, obsAt: budgetCheckInterval, out: o.computeOut, memo: memoReport.Load()}
	p.units.mem = p.Control.memoryControl()
	if p.units.ctx != nil {
		if p.units.ctx.Done() == nil {
			p.units.ctx = nil
		} else if err := contextErr(p.units.ctx); err != nil {
			panic(fatal{err: err})
		}
	}
	p.setUnitsCheck()
}

// setUnitsCheck sets the count above which a check looks at the limit, the context and the memory
// count.
func (p *Parser) setUnitsCheck() {
	c := uint64(math.MaxUint64)
	if p.units.limit != 0 {
		c = p.units.limit
	}
	if p.units.observes() && p.units.obsAt-1 < c {
		c = p.units.obsAt - 1
	}
	p.UnitsCheck = c
}

// observes reports whether the checks of the parse observe anything beside the compute limit: a
// context that can be canceled or a finite memory limit.
func (u *unitsState) observes() bool { return u.ctx != nil || u.mem != nil }

// observeUnits observes the context, then the memory count, and starts the interval to the next
// observation again (spec/runtime/parsing.md, Checks and Interruptions).
func (p *Parser) observeUnits() {
	if p.units.ctx != nil {
		if err := contextErr(p.units.ctx); err != nil {
			panic(fatal{err: err})
		}
	}
	if p.units.mem != nil {
		p.units.mem.checkMemory()
	}
	p.units.obsAt = p.Units + budgetCheckInterval
}

// unitsExceeded stops the parse if the count exceeds the compute limit.
func (p *Parser) unitsExceeded() {
	if p.units.limit != 0 && p.Units > p.units.limit {
		panic(fatal{err: &sxirruntime.LimitError{Kind: sxirruntime.LimitCompute, Limit: p.units.limit, Used: p.Units, Hotspot: p.Observation.hotspot()}})
	}
}

// PollUnits is the slow path of a check of the counter, which the generated code makes when the
// count passes UnitsCheck: once at least budgetCheckInterval units have been counted since the last
// observation it observes the context, then the memory count, and it stops the parse if the count
// exceeds the limit. An error of the context wins over the memory limit, and both over the compute
// limit.
//
// PollUnits is for generated parsers; not a stable API.
func (p *Parser) PollUnits() {
	if !p.units.on {
		p.UnitsCheck = math.MaxUint64
		return
	}
	if p.units.observes() && p.Units >= p.units.obsAt {
		p.observeUnits()
	}
	p.unitsExceeded()
	p.setUnitsCheck()
}

// poll is a step of work that counts no unit (budget.poll), such as a reused element of a run, which
// does not advance the count: it observes the context and the memory count after each
// budgetCheckInterval of these steps.
func (p *Parser) poll() {
	if !p.units.observes() {
		return
	}
	if p.units.polls++; p.units.polls >= budgetCheckInterval {
		p.units.polls = 0
		p.observeUnits()
		p.setUnitsCheck()
	}
}

// reportUnits stores the units the parse counted in its report (WithComputeReport). Each driver
// defers it right after startUnits, so that every end of the parse reports, as the contract of the
// report requires: a success, a syntax error, a stop by a limit or a context, and an error that an
// action raises, which leaves the parse without its end check.
func (p *Parser) reportUnits() {
	if p.units.out != nil {
		*p.units.out = p.Units
	}
}

// endUnits makes the check at the end of a parse, which counts the syntax error it builds if failed:
// it observes the context and the memory count whatever the interval, then compares the count.
func (p *Parser) endUnits(failed bool) {
	if !p.units.on {
		return
	}
	if failed {
		p.Units++
	}
	p.observeUnits()
	p.unitsExceeded()
}

// unitsDemand is the check of a stream under its default limit before it demands the input up to
// the position n (spec/runtime/parsing.md, Checks and Interruptions, check 5): it compares the
// events counted so far with the limit before the demand, then raises the limit to the default of n
// units (defaultBudget). The counter holds every event before the demand: a terminal counts the input
// it advances over once it has matched, before it demands more, and a literal is one event when it
// has matched, so the demands inside it come before it. Under any other limit a demand checks
// nothing.
func (p *Parser) unitsDemand(n int) {
	if !p.units.adaptive || n <= 0 {
		return
	}
	if p.Units > p.UnitsCheck {
		p.PollUnits()
	}
	if b := defaultBudget(uint64(n)); b > p.units.limit {
		p.units.limit = b
		p.setUnitsCheck()
	}
}

// unitsObserve observes the context, then the memory count, whatever the units counted since they
// were last observed, as a stream does around the delivery of an element and before a read of its
// reader, and starts the interval to the next observation again.
func (p *Parser) unitsObserve() {
	p.observeUnits()
	p.setUnitsCheck()
}

// unitsSized counts a sized event of n units and checks the counter, before the work it covers.
func (p *Parser) unitsSized(n int) {
	if !p.units.on {
		return
	}
	p.Units += uint64(n)
	if p.Units > p.UnitsCheck {
		p.PollUnits()
	}
}

// valueUnits counts the n units of a sized event of a value (the bytes a string comparison or
// concatenation covers) when the parse counts logical compute units, and reports whether it does: a
// shared mechanism (value_spend.go, action_value.go) then neither charges the budget for the event nor
// reads the bytes in chunks.
func (p *Parser) valueUnits(n int) bool {
	if !p.units.on {
		return false
	}
	p.unitsSized(n)
	return true
}

// SkipUnits counts the n calls that a skipped alternative would have made, none of them of a
// memoized rule, each failing at the alternative's first terminal, and checks the counter.
//
// SkipUnits is for generated parsers; not a stable API.
func (p *Parser) SkipUnits(n uint64) {
	p.Units += n
	if p.Units > p.UnitsCheck {
		p.PollUnits()
	}
}

// SkipCall makes the effects of a call of the memoized rule r that a skipped alternative would
// have made at the position: the call is the alternative's only call, and fails at r's first
// terminal, whose expectation is exp (skipCalls).
//
// SkipCall is for generated parsers; not a stable API.
func (p *Parser) SkipCall(r *Rule, exp ExpID) {
	p.skipCalls([]*Rule{r}, exp)
}

// SkipCalls makes the effects of the calls of rules that a skipped alternative would have made at
// the position: each rule calls the next before its first terminal, and the last fails at its
// first terminal, whose expectation is exp (skipCalls).
//
// SkipCalls is for generated parsers; not a stable API.
func (p *Parser) SkipCalls(exp ExpID, rules ...*Rule) {
	p.skipCalls(rules, exp)
}

// skipCalls makes the effects that the calls of rules make when the first terminal of the last
// fails at the position with the expectation exp, as the evaluation makes them (Call): each call counts its unit, and a call of a memoized rule the units of its key, checked
// where the evaluation checks them. A memoized rule records its first call (FirstCall). Unless that
// defers its memoization, an entry that answers the call ends it, merging the entry's expectations,
// and the calls it would make are not made; otherwise the rest of the calls run in a record of
// their own, and the call stores the failure entry with that record's expectations, after those of
// the calls inside it. The failing terminal records exp. The calls are outside every left-recursion
// context, and outside Documents: a Document's guards skip no call of a rule that it memoizes.
func (p *Parser) skipCalls(rules []*Rule, exp ExpID) {
	if len(rules) == 0 {
		p.Expect(p.Pos, exp)
		return
	}
	r := rules[0]
	p.Units++
	if r.Rec && p.Units > p.UnitsCheck {
		p.PollUnits()
	}
	if !r.Memo {
		p.skipCalls(rules[1:], exp)
		return
	}
	p.KeyUnits(r)
	if p.FirstCall(r) {
		p.skipCalls(rules[1:], exp)
		return
	}
	start := p.Pos
	env := p.memoEnv(r)
	if e := p.memoGet(r.Id, start, 0, env, false); e != nil && (!e.silent || p.Silent > 0) {
		p.MergeExpected(e.far, e.expected)
		return
	}
	m := p.Isolate(start)
	p.skipCalls(rules[1:], exp)
	far, inner := p.Unisolate(m)
	kept := p.Keep(inner)
	entry := p.memo.alloc()
	*entry = memoEntry{end: start, silent: p.Silent > 0, errs: p.recSince(len(p.Recovered))}
	entry = p.memoPut(r.Id, start, 0, env, false, entry)
	if !(p.Silent > 0) || entry.silent {
		entry.far, entry.expected = far, kept
	}
	p.MergeExpected(far, inner)
}

// SkipCall is Parser.SkipCall for the typed library, whose memo it stores in.
func (p *TParser) SkipCall(r *Rule, exp ExpID) {
	p.skipCalls([]*Rule{r}, exp)
}

// SkipCalls is Parser.SkipCalls for the typed library, whose memo it stores in.
func (p *TParser) SkipCalls(exp ExpID, rules ...*Rule) {
	p.skipCalls(rules, exp)
}

// skipCalls is Parser.skipCalls for the typed library, as the entries of typed rules and CallMemo
// make the calls.
func (p *TParser) skipCalls(rules []*Rule, exp ExpID) {
	if len(rules) == 0 {
		p.Expect(p.Pos, exp)
		return
	}
	r := rules[0]
	p.Units++
	if r.Rec && p.Units > p.UnitsCheck {
		p.PollUnits()
	}
	if !r.Memo {
		p.skipCalls(rules[1:], exp)
		return
	}
	p.KeyUnits(r)
	if p.FirstCall(r) {
		p.skipCalls(rules[1:], exp)
		return
	}
	start := p.Pos
	env := p.memoEnv(r)
	if e := p.memoGet(r.Id, start, 0, env, false); e != nil && (!e.silent || p.Silent > 0) {
		p.MergeExpected(e.far, e.expected)
		return
	}
	m := p.Isolate(start)
	p.skipCalls(rules[1:], exp)
	far, inner := p.Unisolate(m)
	kept := p.Keep(inner)
	entry := p.memoSlab.one()
	*entry = tmemoEntry{end: start, silent: p.Silent > 0, errs: p.recSince(len(p.Recovered))}
	entry = p.memoPut(r.Id, start, 0, env, false, entry)
	if !(p.Silent > 0) || entry.silent {
		entry.far, entry.expected = far, kept
	}
	p.MergeExpected(far, inner)
}

// callUnit counts the unit of a call that does not go through Call, and checks the counter if the
// rule is on a cycle of the call graph (rec), as the call protocol does.
func (p *Parser) callUnit(rec bool) {
	p.Units++
	if rec && p.Units > p.UnitsCheck {
		p.PollUnits()
	}
}

// KeyUnits counts the key of a call of the memoized rule r, and checks the counter if it counts any.
// A parse that does not count logical units leaves its key unread.
//
// KeyUnits is for generated parsers; not a stable API.
func (p *Parser) KeyUnits(r *Rule) {
	if p.units.on && len(r.varSlots) > 0 {
		p.unitsSized(int(p.keySize(r)))
	}
}

// The matchers of logical units below count the input they advance over. A parse that tracks the
// examined input (a Document, or an observed parse, which runs the traced bodies) matches with the
// traced form, which records it also on a stream; any other parse with the ordinary form.

// MatchLiteralLogical is MatchLiteralOrdinary, or MatchLiteralTraced in a parse that tracks the
// examined input, that counts the input it advances over.
//
// MatchLiteralLogical is for generated parsers; not a stable API.
func (p *Parser) MatchLiteralLogical(rs []rune, text string, desc ExpID, build bool) (v *sxirvalue.Node, ok bool) {
	start := p.Pos
	if p.track {
		v, ok = p.MatchLiteralTraced(rs, text, desc, build)
	} else {
		v, ok = p.MatchLiteralOrdinary(rs, text, desc, build)
	}
	if ok {
		p.Units += uint64(p.Pos - start)
	}
	return v, ok
}

// MatchLiteralFoldLogical is MatchLiteralFoldOrdinary, or MatchLiteralFoldTraced in a parse that
// tracks the examined input, that counts the input it advances over.
//
// MatchLiteralFoldLogical is for generated parsers; not a stable API.
func (p *Parser) MatchLiteralFoldLogical(rs []rune, desc ExpID, build bool) (v *sxirvalue.Node, ok bool) {
	start := p.Pos
	if p.track {
		v, ok = p.MatchLiteralFoldTraced(rs, desc, build)
	} else {
		v, ok = p.MatchLiteralFoldOrdinary(rs, desc, build)
	}
	if ok {
		p.Units += uint64(p.Pos - start)
	}
	return v, ok
}

// MatchAnyLogical is MatchAnyOrdinary, or MatchAny in a parse that tracks the examined input, that
// counts the input it advances over.
//
// MatchAnyLogical is for generated parsers; not a stable API.
func (p *Parser) MatchAnyLogical(build bool) (v *sxirvalue.Node, ok bool) {
	start := p.Pos
	if p.track {
		v, ok = p.MatchAny(build)
	} else {
		v, ok = p.MatchAnyOrdinary(build)
	}
	if ok {
		p.Units += uint64(p.Pos - start)
	}
	return v, ok
}

// ClassFastLogical is ClassFast that counts the character it consumes.
//
// ClassFastLogical is for generated parsers; not a stable API.
func (p *TParser) ClassFastLogical(c *Class) bool {
	if p.ClassFast(c) {
		p.Units++
		return true
	}
	return false
}

// ClassSkipLogical is ClassSkip that counts the character it consumes.
//
// ClassSkipLogical is for generated parsers; not a stable API.
func (p *TParser) ClassSkipLogical(c *Class, id ExpID) bool {
	start := p.Pos
	if p.ClassSkip(c, id) {
		p.Units += uint64(p.Pos - start)
		return true
	}
	return false
}

// ClassesSkipLogical is ClassesSkip that counts the characters it consumes, also those of the
// classes that match before one fails.
//
// ClassesSkipLogical is for generated parsers; not a stable API.
func (p *TParser) ClassesSkipLogical(steps []ClassStep) bool {
	start := p.Pos
	ok := p.ClassesSkip(steps)
	p.Units += uint64(p.Pos - start)
	return ok
}

// SkipTerminalLogical is SkipTerminal that counts the input that each of its terminals advances over,
// again after the program backtracks.
//
// SkipTerminalLogical is for generated parsers; not a stable API.
func (p *TParser) SkipTerminalLogical(t *TerminalProgram) bool {
	return p.skipTerminalL(t, 0)
}

func (p *TParser) skipTerminalL(t *TerminalProgram, pc int) bool {
	code := t.Code
	switch code[pc] {
	case TerminalClass:
		c := t.Classes[code[pc+2]]
		start := p.Pos
		if p.ClassFast(c) || p.classSkip(c, ExpID(code[pc+3])) {
			p.Units += uint64(p.Pos - start)
			return true
		}
		return false
	case TerminalLiteral:
		lit := &t.Literals[code[pc+2]]
		start := p.Pos
		_, ok := p.Parser.MatchLiteralOrdinary(lit.Runes, lit.Text, ExpID(code[pc+3]), false)
		if ok {
			p.Units += uint64(p.Pos - start)
		}
		return ok
	case TerminalSeq:
		return p.skipTerminalSeqL(t, pc+2, pc+int(code[pc+1]))
	case TerminalChoice:
		return p.skipTerminalChoiceL(t, pc)
	case TerminalOptional:
		start := p.Pos
		if !p.skipTerminalL(t, pc+2) {
			p.Pos = start
		}
		return true
	case TerminalNot:
		start := p.Pos
		p.Silent++
		ok := p.skipTerminalL(t, pc+2)
		p.Silent--
		p.Pos = start
		return !ok
	}
	panic("sxir: unknown terminal operation")
}

func (p *TParser) skipTerminalSeqL(t *TerminalProgram, pc, end int) bool {
	code := t.Code
	for ; pc < end; pc += int(code[pc+1]) {
		if !p.skipTerminalL(t, pc) {
			return false
		}
	}
	return true
}

func (p *TParser) skipTerminalChoiceL(t *TerminalProgram, pc int) bool {
	code := t.Code
	start, end := p.Pos, pc+int(code[pc+1])
	var ch rune
	size, peeked, more := 0, false, false
	k := pc + 3
	if slot := code[pc+2]; slot != TerminalNoTable && (p.Silent > 0 || start < p.farthest) {
		ch, size, more = p.NextOrdinary()
		peeked = true
		if table := &t.tables[slot]; table.ok && more && uint32(ch) < 128 {
			for _, at := range table.alts[table.at[ch]:table.at[ch+1]] {
				if p.skipTerminalAltL(t, pc+int(at), size) {
					return true
				}
				p.Pos = start
			}
			k = pc + int(table.end)
		}
	}
	for ; k < end; k += int(code[k+1]) {
		if op := code[k]; op == TerminalGuard || op == TerminalGuardClass {
			if !peeked {
				ch, size, more = p.NextOrdinary()
				peeked = true
			}
			if !more || !t.Classes[code[k+2]].Has(ch) {
				p.Expect(start, ExpID(code[k+3]))
				continue
			}
			if p.skipTerminalAltL(t, k, size) {
				return true
			}
		} else if p.skipTerminalL(t, k) {
			return true
		}
		p.Pos = start
	}
	return false
}

func (p *TParser) skipTerminalAltL(t *TerminalProgram, pc, size int) bool {
	code := t.Code
	if code[pc] == TerminalGuardClass {
		p.Pos += size
		p.Units += uint64(size)
		return p.skipTerminalSeqL(t, pc+4, pc+int(code[pc+1]))
	}
	return p.skipTerminalL(t, pc+4)
}

// MakeErrorLogical is MakeError that counts the syntax error it builds for a recovery, an event of one
// compute unit (flow.makeError).
//
// MakeErrorLogical is for generated parsers; not a stable API.
func (p *Parser) MakeErrorLogical(pos int, expected []ExpID) *sxirvalue.SyntaxError {
	p.Units++
	return p.MakeError(pos, expected)
}

// ActionHullLogical is ActionHull that counts one unit for each element of the list or tuple it builds.
//
// ActionHullLogical is for generated parsers; not a stable API.
func (p *Parser) ActionHullLogical(c *ActCtx, tuple bool, kids []*sxirvalue.Node) *sxirvalue.Node {
	p.unitsSized(len(kids))
	return p.ActionHull(c, tuple, kids)
}

// ActionHullLogical is the typed ActionHull that counts one unit for each element it builds.
func (p *TParser) ActionHullLogical(c *TActCtx, tuple bool, kids []any) TVal {
	p.unitsSized(len(kids))
	return p.ActionHull(c, tuple, kids)
}

// TextEqualLogical is TextEqual that counts the length of the shorter string.
//
// TextEqualLogical is for generated parsers; not a stable API.
func (p *Parser) TextEqualLogical(l, r string) bool {
	p.unitsSized(min(len(l), len(r)))
	return l == r
}

// NewListLogical is NewList for the list that map makes of a projection: it counts the elements that
// map visits and those of the list it builds.
//
// NewListLogical is for generated parsers; not a stable API.
func (c *ActCtx) NewListLogical(items []*sxirvalue.Node) *sxirvalue.Node {
	c.P.unitsSized(2 * len(items))
	return c.NewList(items)
}

// NewListLogical is the typed NewList that counts as ActCtx.NewListLogical does.
func (c *TActCtx) NewListLogical(items []any) any {
	c.P.unitsSized(2 * len(items))
	return c.NewList(items)
}

// UnitsSized counts a sized event of n units and checks the counter.
//
// UnitsSized is for generated parsers; not a stable API.
func (p *Parser) UnitsSized(n int) { p.unitsSized(n) }

// UnitsOfItems counts k units for each element of the list or tuple v, and checks the counter.
//
// UnitsOfItems is for generated parsers; not a stable API.
func (p *Parser) UnitsOfItems(v any, k int) {
	if items, ok := valItems(v); ok {
		p.unitsSized(k * len(items))
	}
}

// UnitsOfItems is the typed Parser.UnitsOfItems.
func (p *TParser) UnitsOfItems(v any, k int) {
	if items, ok := tvalItems(v); ok {
		p.unitsSized(k * len(items))
	}
}

// reportMemo stores the report of SetMemoReport, with the lines of the entries of the memo that
// the parse stored them in.
func (p *Parser) reportMemo(entries []string) {
	if !p.units.on || p.units.memo == nil {
		return
	}
	var b strings.Builder
	t := &p.memo
	for s, c := range t.calls {
		fmt.Fprintf(&b, "policy %d: calls %d repeats %d eager %t\n", s, c.calls, c.repeats, c.eager)
	}
	for s := 0; s < t.stride; s++ {
		var at []int
		for pos := 0; pos <= p.n; pos++ {
			if t.isSeen(pos, s) {
				at = append(at, pos)
			}
		}
		if len(at) > 0 {
			fmt.Fprintf(&b, "seen %d: %v\n", s, at)
		}
	}
	for _, l := range entries {
		b.WriteString(l)
	}
	*p.units.memo = b.String()
}

// isSeen reports whether the rule with the seen index s was called at pos (markSeen).
func (t *memoTable) isSeen(pos, s int) bool {
	if t.sparse {
		if s >= len(t.pages) || pos>>10 >= len(t.pages[s].pages) {
			return false
		}
		page := t.pages[s].pages[pos>>10]
		return page != nil && page[(pos>>6)&15]&(uint64(1)<<(pos&63)) != 0
	}
	i := pos*t.stride + s
	return i>>6 < len(t.seen) && t.seen[i>>6]&(uint64(1)<<(i&63)) != 0
}

// memoLines returns the lines of the report of SetMemoReport for the entries of the memo of a
// Node parse: per position, its chain in order, with the number of Document edits each entry
// accounts for.
func (p *Parser) memoLines() []string {
	var out []string
	t := &p.memo
	for i, e := range t.slots {
		pos := i
		if i >= t.gap {
			pos -= t.gapLen
		}
		for ; e != nil; e = e.next {
			out = append(out, fmt.Sprintf("entry %d: rule %d min %d env %v ok %t end %d silent %t far %d expected %v errs %d vgen %d present %d\n",
				pos, e.rule, e.min, e.env, e.ok, e.end, e.silent, e.far, e.expected, e.errs.len(), e.vgen, e.present))
		}
	}
	return out
}

// memoLines is Parser.memoLines for the memo of a typed parse.
func (p *TParser) memoLines() []string {
	var out []string
	for i, e := range p.memoSlots {
		pos := i
		if i >= p.memoGap.gap {
			pos -= p.memoGap.gapLen
		}
		for ; e != nil; e = e.next {
			out = append(out, fmt.Sprintf("entry %d: rule %d min %d env %v ok %t end %d silent %t far %d expected %v errs %d vgen %d present %d\n",
				pos, e.rule, e.min, e.env, e.ok, e.end, e.silent, e.far, e.expected, e.errs.len(), e.vgen, e.present))
		}
	}
	return out
}
