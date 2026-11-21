package libgo

import (
	"context"
	"math"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// Logical compute units, a prototype of design record 076 for measurement: a parser generated with
// them (Generator.Logical) counts the logical events of a parse in Parser.Units. The generated code
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
}

// WithUnitsReport makes a parse with logical units store its count in *out when it ends, whether it
// succeeds, fails or is stopped by a limit. It is for the measurement of the prototype; not a stable
// API.
func WithUnitsReport(out *uint64) ParseOption {
	return func(o *parseOptions) { o.unitsOut = out }
}

// startUnits starts the counter of a parse with the options o.
func (p *Parser) startUnits(o parseOptions) {
	if !o.logical {
		p.units = unitsState{}
		p.UnitsCheck = math.MaxUint64
		return
	}
	p.Units = 0
	p.units = unitsState{on: true, limit: o.unitsLimit, ctx: o.unitsCtx, ctxAt: budgetCheckInterval, out: o.unitsOut}
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
// shared mechanism (value_spend.go, term_value.go) then neither charges the budget for the event nor
// reads the bytes in chunks.
func (p *Parser) valueUnits(n int) bool {
	if !p.units.on {
		return false
	}
	p.unitsSized(n)
	return true
}

// AddUnits adds n units and returns true, so that it can end the condition of a guard that skips an
// alternative: the skip counts the calls of the chain of rules that the alternative begins with.
//
// AddUnits is for generated parsers; not a stable API.
func (p *Parser) AddUnits(n uint64) bool {
	p.Units += n
	return true
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

// TermHullLogical is TermHull that counts one unit for each element of the list or tuple it builds.
//
// TermHullLogical is for generated parsers; not a stable API.
func (p *Parser) TermHullLogical(c *ActCtx, tuple bool, kids []*sxirvalue.Node) *sxirvalue.Node {
	p.unitsSized(len(kids))
	return p.TermHull(c, tuple, kids)
}

// TermHullLogical is the typed TermHull that counts one unit for each element it builds.
func (p *TParser) TermHullLogical(c *TActCtx, tuple bool, kids []any) TVal {
	p.unitsSized(len(kids))
	return p.TermHull(c, tuple, kids)
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
