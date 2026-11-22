package libgo

import (
	"context"
	"fmt"
	"math"
	"strings"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// Logical compute units (design record 076): a parser generated with them (Generator.Logical)
// counts the logical events of a parse in Parser.Units. The generated code
// adds the calls of rules and the input that terminals advance over; the library adds what its
// matchers advance over and the sized events (values built, string comparisons, keys, the syntax
// error built). The counter is checked against the compute limit and the context only at checks: on
// cycles (calls of recursive rules and loop iterations), after sized events and at the end of the
// parse. The charges of the budget control are not made: the control of such a parse holds the
// memory limit only.

// unitsState is the state of the counter of a parse with logical units.
type unitsState struct {
	on    bool
	limit uint64 // the resolved compute limit; 0 sets none
	ctx   context.Context
	ctxAt uint64  // the count at which the next check observes the context
	out   *uint64 // where the end of the parse reports the count (WithUnitsReport)
	memo  *string // where the end of the parse reports the memo (WithMemoReport)
}

// WithUnitsReport makes a parse with logical units store its count in *out when it ends, whether it
// succeeds, fails or is stopped by a limit. It is for tests and measurements; not a stable
// API.
func WithUnitsReport(out *uint64) ParseOption {
	return func(o *parseOptions) { o.unitsOut = out }
}

// WithMemoReport makes a parse with logical units store in *out, when it ends without being stopped,
// the state of its memo that later calls read: the policy counts of each rule, the positions where
// each rule was called (the seen marks) and the stored entries, each chain in its order. It is for
// the tests of logical units, which compare it between parsers that must leave the same state; not
// a stable API.
func WithMemoReport(out *string) ParseOption {
	return func(o *parseOptions) { o.memoOut = out }
}

// startUnits starts the counter of a parse with the options o.
func (p *Parser) startUnits(o parseOptions) {
	if !o.logical {
		p.units = unitsState{}
		p.UnitsCheck = math.MaxUint64
		return
	}
	p.Units = 0
	p.units = unitsState{on: true, limit: o.unitsLimit, ctx: o.unitsCtx, ctxAt: budgetCheckInterval, out: o.unitsOut, memo: o.memoOut}
	if p.units.ctx != nil {
		if p.units.ctx.Done() == nil {
			p.units.ctx = nil
		} else if err := contextErr(p.units.ctx); err != nil {
			panic(fatal{err: err})
		}
	}
	p.setUnitsCheck()
}

// setUnitsCheck sets the count above which a check looks at the limit and the context.
func (p *Parser) setUnitsCheck() {
	c := uint64(math.MaxUint64)
	if p.units.limit != 0 {
		c = p.units.limit
	}
	if p.units.ctx != nil && p.units.ctxAt-1 < c {
		c = p.units.ctxAt - 1
	}
	p.UnitsCheck = c
}

// PollUnits is the slow path of a check of the counter, which the generated code makes when the
// count passes UnitsCheck: it observes the context once every budgetCheckInterval units, then
// stops the parse if the count exceeds the limit. A context error wins over the limit.
//
// PollUnits is for generated parsers; not a stable API.
func (p *Parser) PollUnits() {
	if !p.units.on {
		p.UnitsCheck = math.MaxUint64
		return
	}
	if p.units.out != nil {
		*p.units.out = p.Units
	}
	if p.units.ctx != nil && p.Units >= p.units.ctxAt {
		if err := contextErr(p.units.ctx); err != nil {
			panic(fatal{err: err})
		}
		p.units.ctxAt = p.Units + budgetCheckInterval
	}
	if p.units.limit != 0 && p.Units > p.units.limit {
		panic(fatal{err: &sxirruntime.LimitError{Kind: sxirruntime.LimitCompute, Limit: p.units.limit, Used: p.Units}})
	}
	p.setUnitsCheck()
}

// endUnits checks the count at the end of a parse, which counts the syntax error it builds if failed.
func (p *Parser) endUnits(failed bool) {
	if !p.units.on {
		return
	}
	if failed {
		p.Units++
	}
	if p.units.out != nil {
		*p.units.out = p.Units
	}
	if p.units.limit != 0 && p.Units > p.units.limit {
		panic(fatal{err: &sxirruntime.LimitError{Kind: sxirruntime.LimitCompute, Limit: p.units.limit, Used: p.Units}})
	}
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
// fails at the position with the expectation exp, as the evaluation makes them (CallLogical and
// Call): each call counts its unit, and a call of a memoized rule the units of its key, checked
// where the evaluation checks them. A memoized rule records its first call (FirstCall). Unless that
// defers its memoization, an entry that answers the call ends it, merging the entry's expectations,
// and the calls it would make are not made; otherwise the rest of the calls run in a record of
// their own, and the call stores the failure entry with that record's expectations, after those of
// the calls inside it. The failing terminal records exp. The calls are outside every left-recursion
// context and Document, which logical units do not cover.
func (p *Parser) skipCalls(rules []*Rule, exp ExpID) {
	if len(rules) == 0 {
		p.ExpectOrdinary(p.Pos, exp)
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
		p.ExpectOrdinary(p.Pos, exp)
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

// CallLogical is Call that counts the unit of the call, and the key of a call of a memoized rule,
// and checks the counter.
//
// CallLogical is for generated parsers; not a stable API.
func (p *Parser) CallLogical(r *Rule, min int) (*sxirvalue.Node, bool) {
	p.Units++
	if (r.Memo || r.Leader) && len(r.varSlots) > 0 {
		p.Units += p.keySize(r)
	}
	if p.Units > p.UnitsCheck {
		p.PollUnits()
	}
	return p.Call(r, min)
}

// KeyUnits counts the key of a call of the memoized rule r, and checks the counter if it counts any.
//
// KeyUnits is for generated parsers; not a stable API.
func (p *Parser) KeyUnits(r *Rule) {
	if len(r.varSlots) > 0 {
		p.unitsSized(int(p.keySize(r)))
	}
}

// MatchLiteralLogical is MatchLiteralOrdinary that counts the input it advances over.
//
// MatchLiteralLogical is for generated parsers; not a stable API.
func (p *Parser) MatchLiteralLogical(rs []rune, text string, desc ExpID, build bool) (*sxirvalue.Node, bool) {
	start := p.Pos
	v, ok := p.MatchLiteralOrdinary(rs, text, desc, build)
	if ok {
		p.Units += uint64(p.Pos - start)
	}
	return v, ok
}

// MatchLiteralFoldLogical is MatchLiteralFoldOrdinary that counts the input it advances over.
//
// MatchLiteralFoldLogical is for generated parsers; not a stable API.
func (p *Parser) MatchLiteralFoldLogical(rs []rune, desc ExpID, build bool) (*sxirvalue.Node, bool) {
	start := p.Pos
	v, ok := p.MatchLiteralFoldOrdinary(rs, desc, build)
	if ok {
		p.Units += uint64(p.Pos - start)
	}
	return v, ok
}

// MatchAnyLogical is MatchAnyOrdinary that counts the input it advances over.
//
// MatchAnyLogical is for generated parsers; not a stable API.
func (p *Parser) MatchAnyLogical(build bool) (*sxirvalue.Node, bool) {
	start := p.Pos
	v, ok := p.MatchAnyOrdinary(build)
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
				p.ExpectOrdinary(start, ExpID(code[k+3]))
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

// reportMemo stores the report of WithMemoReport, with the lines of the entries of the memo that
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

// memoLines returns the lines of the report of WithMemoReport for the entries of the memo of a
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
