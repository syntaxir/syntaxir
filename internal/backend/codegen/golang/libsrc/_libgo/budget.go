package libgo

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// The default budget of a parse is defaultBudgetPerUnit for each unit of input, and at least
// defaultBudgetFloor.
const (
	defaultBudgetFloor   = 1 << 20
	defaultBudgetPerUnit = 1024
)

// defaultBudget returns the budget of a parse whose Compute is zero, for an input of n units of the
// position unit of the parse. It counts the work that a parse of the input from scratch does; the
// work that only a Document does is left out of it (documentWork).
func defaultBudget(n uint64) uint64 {
	if n > math.MaxUint64/defaultBudgetPerUnit {
		return math.MaxUint64 - 1
	}
	return max(defaultBudgetFloor, n*defaultBudgetPerUnit)
}

// inputUnits returns the length of s in units of the position unit. Below the size at which the
// default budget leaves its floor the byte length stands in for the count of code points, which it
// bounds from above.
func inputUnits(s string, unit sxirinput.PositionUnit) uint64 {
	if unit == sxirinput.Bytes || len(s) <= defaultBudgetFloor/defaultBudgetPerUnit {
		return uint64(len(s))
	}
	return uint64(utf8.RuneCountInString(s))
}

// limitsDisabled reports whether l needs no control: Compute and Memory are zero or Unlimited. Input is
// checked against the length of the input and Depth by the parser, so neither needs one.
func limitsDisabled(l sxirruntime.Limits) bool {
	return (l.Compute == 0 || l.Compute == sxirruntime.Unlimited) && (l.Memory == 0 || l.Memory == sxirruntime.Unlimited)
}

// mergeLimits returns l with every nonzero field of o in place of the field of l: the limits of a
// later WithLimits over those of an earlier one. A zero field of o keeps the field of l.
func mergeLimits(l, o sxirruntime.Limits) sxirruntime.Limits {
	return sxirruntime.Limits{Compute: nonzero(o.Compute, l.Compute), Memory: nonzero(o.Memory, l.Memory), Input: nonzero(o.Input, l.Input), Depth: nonzero(o.Depth, l.Depth)}
}

// nonzero returns v, or def if v is zero.
func nonzero(v, def uint64) uint64 {
	if v != 0 {
		return v
	}
	return def
}

// budgetCheckInterval is the count of logical units between two observations of cancellation and
// of the memory count (PollUnits).
const budgetCheckInterval = uint64(256)

// BudgetControl is the control of a single parser invocation that has limit controls: a compute limit,
// a compute report, a context or a memory limit, so that a parse without one has none (Parser.Control
// is nil exactly when the parse runs without limit controls, as in the TypeScript library). With a
// finite memory limit it counts the memory of the parse (memoryCounts) and holds the parser state the
// memory check reads; the parser then counts memory (Parser.countsMemory). The compute limit and the
// context of a parse are its counter of logical units (unitsState), which observes the memory count
// at its polls.
// The conversion of a result to typed values runs outside that counter: its control carries the
// context of the parse as well (conversionControl), which Check observes.
//
// BudgetControl is for generated parsers; not a stable API.
type BudgetControl struct {
	limits      sxirruntime.Limits
	interrupted bool // distinguishes limit interruptions from ordinary parse errors
	// ctx is the context of the parse while its result is converted (conversionControl), nil
	// otherwise.
	ctx context.Context

	// memoryCounts are the memory the control counts itself; retained is what a typed Document
	// retains from earlier parses. The memory check adds the counters of the parser and the typed
	// state attached by attachMemory (memoryUsed).
	memoryCounts
	memP   *Parser
	memT   *TParser
	memExt memoryCounter
}

// newBudgetControl returns the control of the resolved limits, or nil when they set no compute or
// memory limit.
func newBudgetControl(limits sxirruntime.Limits) *BudgetControl {
	if limits.Compute == 0 && limits.Memory == 0 {
		return nil
	}
	return &BudgetControl{limits: limits}
}

// parseControl returns the control of a parse with the resolved limits, the compute report out and
// the context ctx, or nil when the parse has no limit control: no compute or memory limit, no report
// and no context that can be cancelled.
func parseControl(limits sxirruntime.Limits, out *uint64, ctx context.Context) *BudgetControl {
	if c := newBudgetControl(limits); c != nil {
		return c
	}
	if out != nil || ctx != nil && ctx.Done() != nil {
		return &BudgetControl{limits: limits}
	}
	return nil
}

// setControl makes c the control of the parse, and sets memoryLimited when c has a finite memory
// limit, once, so that the gate of the memory accounting (countsMemory) reads one field.
func (p *Parser) setControl(c *BudgetControl) {
	p.Control, p.memoryLimited = c, c != nil && c.limits.Memory != 0
}

// memoryControl returns c if it has a finite memory limit, and nil otherwise: the control that the
// steps of a conversion to typed values observe, which only a memory limit (or the context, which
// conversionControl adds) gives work to.
func (c *BudgetControl) memoryControl() *BudgetControl {
	if c == nil || c.limits.Memory == 0 {
		return nil
	}
	return c
}

// contextPanic attributes a panic to caller-owned context code rather than VM
// execution. Parse boundaries re-raise its original value after cleanup.
type contextPanic struct{ value any }

func contextErr(ctx context.Context) error {
	defer func() {
		if x := recover(); x != nil {
			switch x.(type) {
			case fatal, contextPanic:
				panic(x) // preserve engine interruption and an already attributed panic
			default:
				panic(contextPanic{value: x})
			}
		}
	}()
	return ctx.Err()
}

// Check observes, at an explicit checkpoint, the context of a conversion (conversionControl) and then
// the memory limit. It is safe on a nil receiver.
func (c *BudgetControl) Check() {
	if c == nil {
		return
	}
	if c.ctx != nil {
		if err := contextErr(c.ctx); err != nil {
			panic(fatal{err: err})
		}
	}
	if c.limits.Memory != 0 {
		c.checkMemory()
	}
}

// conversionControl returns the control that a conversion to typed values passes to the converter,
// whose steps observe it every 256 steps (Check): the control c of the parse's memory limit, which
// carries the context ctx of the parse while the conversion runs, or a control of the context alone.
// done ends the conversion: c forgets the context, so that a later parse with the same control
// observes its own.
func conversionControl(ctx context.Context, c *BudgetControl) (_ *BudgetControl, done func()) {
	c = c.memoryControl()
	if ctx == nil || ctx.Done() == nil {
		return c, func() {}
	}
	if c == nil {
		return &BudgetControl{ctx: ctx}, func() {}
	}
	c.ctx = ctx
	return c, func() { c.ctx = nil }
}

// countsMemory reports whether the parse counts the memory of the values it builds: whether it has a
// finite memory limit (memoryLimited), whose control holds the count.
func (p *Parser) countsMemory() bool { return usedFeatures&featureBudget != 0 && p.memoryLimited }

// memoControlled reports whether the memo of the parse takes the lookups and stores of a parse with
// controls (memoGetControlled, memoPutControlled), as the engine's memo does whenever the parse has
// a control: one with a memory limit, or one that counts logical compute units. A rule that only a
// Document memoizes is then looked up by the assignments of the strings of its key at a constant cost
// (identEqual) and in the first docOnlyScan entries of a chain, so that its hits, and with them the
// units the parse counts, are those of the engine.
func (p *Parser) memoControlled() bool {
	return p.countsMemory() || usedFeatures&featureBudget != 0 && p.units.on
}

// The members of the shared mechanisms (core.go) that charge their steps: in the library, as in the
// engine, the work they step through is no event of logical units (spec/runtime/parsing.md#compute-units),
// so they count nothing. A parse with limit controls counts the events of that work itself.

// spendValue is the step of the work on values done in chunks (value_spend.go).
func (p *Parser) spendValue(int) {}

// spendMemo is a step of the work of the dependency memo of left recursion (context_table.go).
func (p *Parser) spendMemo() {}

// abortParse ends the parse with the error err.
func abortParse(err error) { panic(fatal{err: err}) }

func (p *Parser) literalHasReplacement(rs []rune) bool {
	return slices.Contains(rs, utf8.RuneError)
}

func (p *Parser) hasLiteralPrefix(text string) bool {
	return strings.HasPrefix(p.bs[p.Pos:], text)
}

// RunParseConverted shares admission and counters with a Node-to-typed converter.
//
// RunParseConverted is for generated parsers; not a stable API.
func RunParseConverted[T any](r *Rule, input string, o parseOptions, conv func(*sxirvalue.Node, *BudgetControl, error) T) (res T, err error) {
	defer recoverParse(&res, &err)
	o = prepareText(o, input)
	n, err := parseConversionInput(r, input, o)
	c, done := conversionControl(o.unitsCtx, o.control)
	defer done()
	c.Check()
	res = conv(n, c, err)
	c.Check()
	return res, err
}

// Ordinary runtime/depth errors retain the configured ParseValue converter's
// historical nil mapping. Budget interruptions bypass conversion without calling
// methods on a caller-owned error to classify it.
func parseConversionInput(r *Rule, input string, o parseOptions) (n *sxirvalue.Node, err error) {
	defer func() {
		if x := recover(); x != nil {
			if f, ok := x.(fatal); ok && (o.control == nil || !o.control.interrupted) {
				n, err = nil, f.err
			} else {
				panic(x)
			}
		}
	}()
	return parseResolvedSpend(r, input, o)
}

// compareStrings applies the comparison operator with the code op (OpEq ... OpGe) to two strings as the
// composition ActionBinary does, without the values boxed.
func (p *Parser) compareStrings(op int, l, r string) (bool, bool) {
	switch op {
	case OpEq:
		return p.TextEqual(l, r), true
	case OpNe:
		return !p.TextEqual(l, r), true
	case OpLt:
		return p.compareText(l, r) < 0, true
	case OpLe:
		return p.compareText(l, r) <= 0, true
	case OpGt:
		return p.compareText(l, r) > 0, true
	case OpGe:
		return p.compareText(l, r) >= 0, true
	}
	return false, false
}

// detachedText returns the text of the input from start to end, normalized, when malformed source
// bytes prevent slicing the original string, and counts its bytes as values of the parse.
func (p *Parser) detachedText(start, end int) string {
	s := string(p.In[start:end])
	p.chargeValueBytes(len(s))
	return s
}

// rawValueEqual preserves the interface equality of memo keys and bindings.
// In particular, a typed nil pointer differs from a nil interface. Action
// valueEqual has different nil semantics and must not be used for these keys.
func (p *Parser) rawValueEqual(l, r any) bool {
	if s, ok := l.(string); ok {
		t, ok := r.(string)
		return ok && len(s) == len(t) && p.compareText(s, t) == 0
	}
	return l == r
}

// identEqual compares two key values for a lookup of a rule that only a Document parse memoizes
// (memoBindings): two strings are equal if the same assignment bound them, the stamps of the
// assignments being equal, which costs a constant, or if no assignment bound either and they are equal,
// as the initial values of a variable are, which the grammar bounds. A string that another assignment
// bound, even an equal one, is a miss, and a miss is sound, since the rule is then evaluated and that
// work counts.
func (p *Parser) identEqual(l, r any) bool {
	switch l := l.(type) {
	case assigned:
		m, ok := r.(assigned)
		return ok && l.stamp == m.stamp
	case string:
		m, ok := r.(string)
		return ok && l == m
	}
	return l == r
}

// memoKeyIdentical is keysEqual under identEqual.
func (p *Parser) memoKeyIdentical(l, r []any) bool {
	if len(l) != len(r) {
		return false
	}
	for i, v := range l {
		if !p.identEqual(v, r[i]) {
			return false
		}
	}
	return true
}

// keyMatches compares a memo key under the lookup that ident selects. The comparison charges nothing:
// the call paid for the lookup of its key (keySize), which the size of the key bounds. A lookup of a
// rule that only a Document memoizes compares the assignments of strings (identEqual), which costs a
// constant per variable.
func (p *Parser) keyMatches(l, r []any, ident bool) bool {
	if ident {
		return p.memoKeyIdentical(l, r)
	}
	return keysEqual(l, r)
}

// keysEqual reports whether two memo keys are equal under the interface equality of rawValueEqual,
// without charges.
func keysEqual(l, r []any) bool {
	if len(l) != len(r) {
		return false
	}
	for i, v := range l {
		if s, ok := v.(string); ok {
			if t, ok := r[i].(string); !ok || s != t {
				return false
			}
		} else if v != r[i] {
			return false
		}
	}
	return true
}

func (p *Parser) memoKeyEqual(l, r []any) bool {
	if len(l) != len(r) {
		return false
	}
	for i, v := range l {
		if !p.rawValueEqual(v, r[i]) {
			return false
		}
	}
	return true
}

func (p *Parser) memoGet(rule, pos, min int, key []any, docOnly bool) *memoEntry {
	p.use(featureMemo)
	if p.Stream != nil {
		// A call at a position before the pruned memo needs no entry (a
		// start rule called at 0).
		if pos -= p.Stream.memoBase; pos < 0 {
			return nil
		}
	}
	if p.memoControlled() {
		return p.memoGetControlled(rule, pos, min, key, docOnly)
	}
	return p.memo.get(rule, pos, min, key)
}

// memoGetControlled is memoGet under a memory limit. The lookup charges nothing: the call paid for
// the lookup of its key (keySize). The walk compares at most memoScan entries and then probes the
// index of the position, so it costs at most a constant plus the size of the key.
func (p *Parser) memoGetControlled(rule, pos, min int, key []any, ident bool) *memoEntry {
	// ident is set for a rule that only a Document parse memoizes: the lookup compares the assignments of
	// the strings of keys (identEqual) and walks docOnlyScan entries (see there).
	t := &p.memo
	i := t.slot(pos)
	if i >= len(t.slots) {
		return nil
	}
	scan := memoScan
	if ident {
		scan = docOnlyScan
	}
	link := p.unlinkLagged(i, &t.slots[i], scan)
	for e := t.slots[i]; e != nil && e != *link; e = e.next {
		if e.rule == int32(rule) && e.min == int32(min) && p.keyMatches(e.env, key, ident) {
			return e
		}
	}
	if *link == nil || ident {
		return nil
	}
	p.dropLagged(i, link)
	if *link == nil {
		return nil
	}
	ix := indexAt(&t.index, &t.indexBytes, i, t.slots[i], func(e *memoEntry) *memoEntry { return e.next }, entryHash)
	_, x := ix.probe(keyHash(int32(rule), int32(min), key), func(x *memoEntry) bool {
		return x.rule == int32(rule) && x.min == int32(min) && keysEqual(x.env, key)
	})
	return x
}

// mayLag reports whether an entry can lag behind more than pendingEditLimit edits: only after more than
// that many edits of a Document (gen), so that no parse of a text, and no Document parse before its
// pendingEditLimit-th edit, has an entry to unlink.
func (p *Parser) mayLag() bool { return p.gen > pendingEditLimit }

// dropLagged removes the entries that lag from the rest of the chain at the slot i, from the link on, where
// the caller needs nothing of the walk but the removal.
func (p *Parser) dropLagged(i int, link **memoEntry) {
	if p.mayLag() {
		p.unlinkLagged(i, link, -1)
	}
}

// lagged reports whether a lookup treats e as a miss because it lags behind more than pendingEditLimit
// edits (advanceEntry).
func (p *Parser) lagged(e *memoEntry) bool {
	return p.gen-e.vgen > pendingEditLimit
}

// unlinkLagged removes from the chain at the slot i, starting at the link, the entries that lag behind
// (lagged) among the next limit entries that do not, or among all of them if limit is negative, and
// reports where the walk ends: the link before the first entry it did not visit. Removing an entry
// charges nothing, as the walk does not (memo.lookup): each entry is removed once, after the counted call
// that stored it. The index of the slot, if there is one, no longer matches the chain and is dropped. A
// Document drops such entries where a walk meets them, so the entries of earlier texts that it retains,
// and the work of a walk that compares them, stay bounded by that many edits.
func (p *Parser) unlinkLagged(i int, link **memoEntry, limit int) **memoEntry {
	if !p.mayLag() {
		// No entry lags, so the walk only steps over the kept ones.
		for kept := 0; *link != nil && kept != limit; kept++ {
			link = &(*link).next
		}
		return link
	}
	t := &p.memo
	for kept := 0; *link != nil && kept != limit; {
		e := *link
		if !p.lagged(e) {
			link, kept = &e.next, kept+1
			continue
		}
		*link = e.next
		if ix := t.index[i]; ix != nil {
			t.indexBytes -= min(t.indexBytes, indexBytes(ix))
			delete(t.index, i)
		}
		t.retire(e)
	}
	return link
}

// keyHash hashes a memo key for the index of a long chain, without charges: the call paid for the
// lookup of its key (keySize), and an entry is hashed once, when it joins the index.
func keyHash(rule, min int32, env []any) uint64 {
	h, _ := memoKeyHash(rule, min, false, env)
	return h
}

// entryHash is keyHash for the key of e.
func entryHash(e *memoEntry) uint64 { return keyHash(e.rule, e.min, e.env) }

// keySize returns the units of the memo key of a call of r (keySize).
func (p *Parser) keySize(r *Rule) uint64 {
	n := uint64(len(r.varSlots))
	for _, slot := range r.varSlots {
		if slot < 0 {
			continue
		}
		if s, ok := p.Env.get(slot, p.envDepth).(string); ok {
			n += uint64(len(s))
		}
	}
	return n
}

// hashMemoKey hashes the key of a record of a skip run for the index of its records. The hash costs
// one step plus one per variable of the key, and the bytes of string values as a value comparison
// would, so a lookup through the index costs the size of the key, not the number of records.
func (p *Parser) hashMemoKey(rule, min int32, env []any) uint64 {
	h, _ := memoKeyHash(rule, min, false, env)
	return h
}

// memoPut stores e under the key, unless the join with the entry stored there keeps that entry
// (joinStored), and returns the entry it keeps.
func (p *Parser) memoPut(rule, pos, min int, key []any, docOnly bool, e *memoEntry) *memoEntry {
	if p.Stream != nil {
		if p.countsMemory() {
			p.Control.noteStored(pos)
		}
		if pos -= p.Stream.memoBase; pos < 0 {
			return e
		}
	}
	if p.memoControlled() {
		return p.memoPutControlled(rule, pos, min, key, docOnly, e)
	}
	return p.memo.put(rule, pos, min, key, e, p.joinStored)
}

// joinStored returns the entry that a store of e keeps under the key of x, the entry stored there:
// the join of the two, which answers every call that either answers. A store never narrows the
// calls that a current entry answers (see "Evaluations of Rules" in spec/runtime/parsing.md). x
// stays if it was computed outside a lookahead and e inside one; otherwise e replaces it and takes
// its present set, to which the caller adds that of e's context. Both read no growing result, so
// each is the result of the call outside every context: neither calls a leader of either present
// set at the position, and the result holds in every context whose leaders are in their union.
// An entry that a lookup would not return, one that lags behind an edit it cannot be advanced past
// (advanceEntry), is not current: e replaces it as before.
func (p *Parser) joinStored(x, e *memoEntry) *memoEntry {
	if x == e || x.vgen != p.gen && !advanceEntry(x, p.edits) {
		return e
	}
	if !x.silent && e.silent {
		return x
	}
	e.present = x.present
	return e
}

// memoPutControlled is memoPut under a memory limit. The store charges nothing: it follows the
// evaluation of a body that counted, and its walk costs what memoGetControlled's does.
func (p *Parser) memoPutControlled(rule, pos, min int, key []any, ident bool, e *memoEntry) *memoEntry {
	t := &p.memo
	i := t.slot(pos)
	slots := t.slots
	if i >= len(slots) {
		// Growing the slot array charges no unit (it grows to positions of counted calls, by
		// doubling). Its memory is charged by its length, not its capacity, which a pooled parser keeps.
		t.memBytes += uint64(i+1-len(slots)) * ptrBytes
		if i < cap(slots) {
			slots = slots[:i+1]
		} else {
			grown := make([]*memoEntry, i+1, max(2*cap(slots), i+1, 64))
			copy(grown, slots)
			slots = grown
		}
	}
	link := &slots[i]
	var replaced *memoEntry
	same := func(x *memoEntry) bool {
		return x == e || x.rule == int32(rule) && x.min == int32(min) && p.keyMatches(x.env, key, ident)
	}
	scan := memoScan
	if ident {
		scan = docOnlyScan
	}
	end := p.unlinkLagged(i, link, scan)
	for x := *link; x != nil && link != end; link, x = &x.next, x.next {
		if same(x) {
			replaced = x
			break
		}
	}
	var ix memoIndex[memoEntry]
	var ixSlot uint64
	if replaced == nil && *link != nil && !ident {
		// A long chain: the index finds the entry to replace.
		p.dropLagged(i, link)
	}
	if replaced == nil && *link != nil && !ident {
		ix = indexAt(&t.index, &t.indexBytes, i, slots[i], func(e *memoEntry) *memoEntry { return e.next }, entryHash)
		ixSlot, replaced = ix.probe(keyHash(int32(rule), int32(min), key), same)
		if replaced != nil {
			link = &slots[i]
			for *link != replaced {
				link = &(*link).next
			}
		}
	} else if replaced != nil {
		// The entry replaced among the first of a long chain is indexed too.
		if ix = t.index[i]; ix != nil {
			ixSlot, _ = ix.probe(keyHash(replaced.rule, replaced.min, replaced.env), func(y *memoEntry) bool { return y == replaced })
		}
	}
	if replaced != nil && p.joinStored(replaced, e) == replaced {
		return replaced
	}
	// Commit the completed key/slot array and link together, with no further
	// checkpoint between mutations. A resident self-entry retains its successor.
	t.slots = slots
	e.rule, e.min, e.env = int32(rule), int32(min), key
	if ix != nil {
		n := len(ix)
		ix[ixSlot] = e
		t.indexBytes += uint64(len(ix)-n) * memoIndexEntryBytes
	}
	if replaced != nil {
		e.next = replaced.next
		*link = e
		return e
	}
	e.next = slots[i]
	slots[i] = e
	return e
}

// Graph helpers account for normal construction and capture restoration.
// Release/recycle, DropKids/DropTrail and arena reset remain unconditional.
func (p *Parser) valueField(fs valueimpl.Fields, name string) (any, bool) {
	return fs.Get(name)
}

func (p *Parser) setValueField(fs *valueimpl.Fields, name string, v any) {
	valueimpl.SetField(fs, name, v)
}
func (p *Parser) pushKid(v *sxirvalue.Node) { p.KidStack = append(p.KidStack, v) }

// undoCaptures is frame.undo, which charges nothing: captures are not an item of the cost.
func (p *Parser) undoCaptures(from int) {
	for i := len(p.Trail) - 1; i >= from; i-- {
		u := p.Trail[i]
		u.f.Vals[u.slot] = u.old
	}
}

// evalErrorValue returns the failure of an action expression with the message that format makes of args, which a
// composition returns (see actionError).
func (p *Parser) evalErrorValue(format string, args ...any) evalError {
	return evalError{fmt.Sprintf(format, args...)}
}

// wrapEvalError is the error that ends the parse when the action in where fails with message.
func (p *Parser) wrapEvalError(where, message string) error {
	return fmt.Errorf("action in %s: %s", where, message)
}
