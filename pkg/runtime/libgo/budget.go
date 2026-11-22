package libgo

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
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

// budgetCheckInterval is the spend between two observations of cancellation.
const budgetCheckInterval = uint64(256)

// BudgetControl is private state for a single parser invocation. Callers must
// split long operations into bounded reservations so cancellation can be
// observed between chunks.
//
// BudgetControl is for generated parsers; not a stable API.
type BudgetControl struct {
	ctx    context.Context
	limits sxirruntime.Limits

	// The units reserved are base, which the slow path of charge keeps, plus
	// what the fast path reserved since (used). room is the units the fast path
	// can still reserve without reaching a poll boundary or exceeding a limit,
	// out of the grant that refill computed, so that the common charge is one
	// subtraction that inlines into the matchers.
	base        uint64
	grant, room uint64

	sinceCheck  uint64
	interrupted bool // distinguishes budget interruptions from ordinary parse errors
	// polled is set when something is observed at the poll interval: a context that can be
	// canceled, or a finite memory limit.
	polled bool

	// adaptive is set for a stream whose budget is the default: the budget follows the furthest
	// position the parser has demanded from the stream (demand).
	adaptive bool

	// defaultCompute is set when the compute limit is the default of a Document parse. Under it the
	// limit counts only the work that a parse from scratch counts: the units reserved for work that
	// only a Document does (chargeDoc, freeBegin) are added to doc and to the limit, so they leave
	// room for the counted work. An explicit limit counts all units, and doc stays zero.
	defaultCompute bool
	// doc is the part of the units reserved that only a Document parse reserves, under a default limit.
	doc uint64
	// freeing is set between freeBegin and freeEnd; freed holds the limit that freeBegin lifted.
	freeing  bool
	freed    uint64
	freeMark uint64 // the units reserved when the open region began

	// memoryCounts are the memory the control counts itself; retained is what a typed Document
	// retains from earlier parses. The memory check adds the counters of the parser and the typed
	// state attached by attachMemory (memoryUsed).
	memoryCounts
	memP *Parser
	// obs is the observation of the parse, which names the hotspot of an exceeded compute limit.
	obs    *observation
	memT   *TParser
	memExt memoryCounter
}

// demand raises the default budget of a stream to that of n units, where n is the number of units up
// to the furthest position the parser has asked the stream for. It depends on the input and the
// grammar only, not on how the reader chunks the input. It does nothing for any other budget, and
// the budget never shrinks.
func (c *BudgetControl) demand(n int) {
	if c == nil || !c.adaptive || n <= 0 {
		return
	}
	if b := defaultBudget(uint64(n)); b > c.limits.Compute {
		c.settle()
		c.limits.Compute = b
		c.refill()
	}
}

// newBudgetControl returns nil when neither cancellation nor a finite budget or memory limit is
// active. Without a cancelable context or a memory limit the control never polls, so a parse with
// only a budget reaches the slow path of a charge only at its limit. A nil context is equivalent
// to context.Background.
func newBudgetControl(ctx context.Context, limits sxirruntime.Limits) *BudgetControl {
	if ctx != nil && ctx.Done() == nil {
		ctx = nil // it can never be canceled
	}
	if ctx == nil && limits.Compute == 0 && limits.Memory == 0 {
		return nil
	}
	c := &BudgetControl{ctx: ctx, limits: limits, polled: ctx != nil || limits.Memory != 0}
	c.refill()
	return c
}

// observe records obs, the observation of the parse, as the one whose profile names the hotspot of a
// compute limit error, and returns it. A nil control records nothing.
func (c *BudgetControl) observe(obs *observation) *observation {
	if c != nil {
		c.obs = obs
	}
	return obs
}

// budgetUsed returns the compute units reserved so far.
func (c *BudgetControl) budgetUsed() uint64 { return c.base + (c.grant - c.room) }

// settle moves what the fast path of charge reserved into the base and the
// poll counter, leaving no room.
func (c *BudgetControl) settle() {
	n := c.grant - c.room
	c.base += n
	c.sinceCheck += n
	c.grant, c.room = 0, 0
}

// refill grants the fast path of charge the room left by the settled counters.
func (c *BudgetControl) refill() {
	interval := budgetCheckInterval
	if !c.polled {
		interval, c.sinceCheck = unpolledGrant, 0
	}
	c.grant = budgetRoom(c.base, c.sinceCheck, c.limits.Compute, interval)
	c.room = c.grant
}

// unpolledGrant is the room granted at a time when nothing observes the context.
// It is far below the overflow of the counters.
const unpolledGrant = uint64(1) << 62

// budgetRoom returns the smallest request that reaches the poll interval
// (reachesInterval) or exceeds the limit (exceedsLimit).
func budgetRoom(used, since, limit, interval uint64) uint64 {
	var room uint64
	if since < interval {
		room = interval - since
	}
	if limit != 0 {
		if used > limit {
			return 0
		}
		if l := limit - used + 1; l != 0 && l < room {
			room = l
		}
	}
	return room
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

// Check observes cancellation, then the memory limit, at an explicit engine checkpoint. It is safe
// on a nil receiver.
func (c *BudgetControl) Check() {
	if c == nil {
		return
	}
	if c.ctx != nil {
		if err := contextErr(c.ctx); err != nil {
			c.interrupted = true
			panic(fatal{err: err})
		}
		c.settle()
		c.sinceCheck = 0
		c.refill()
	}
	if c.limits.Memory != 0 {
		c.checkMemory()
	}
}

// Charge atomically reserves n compute units. Cancellation wins when it is
// observed at a poll or when the reservation would fail. A failed reservation
// leaves the counter unchanged. It is safe on a nil receiver.
func (c *BudgetControl) Charge(n uint64) {
	if c != nil && n < c.room {
		c.room -= n
		return
	}
	c.chargeSlow(n)
}

// Take reserves n compute units one at a time, as n one-unit reservations do, and returns true so
// that it can end a condition. A run that fits without reaching a poll boundary is reserved at once;
// otherwise takeSlow reserves it, so that a failed run reports the unit that does not fit: Used is
// the limit and Requested one. It does nothing on a nil receiver.
func (c *BudgetControl) Take(n uint64) bool {
	if c == nil {
		return true
	}
	if n < c.room {
		c.room -= n
		return true
	}
	c.takeSlow(n)
	return true
}

// noFree is the mark that FreeBegin returns when it opens no region.
const noFree = ^uint64(0)

// ChargeDoc reserves n units of work that only a Document parse does, as Charge does, and under a
// default limit leaves them out of the count against it: the limit rises by n first, so the units
// leave the room that counted work has. Under an explicit limit it is Charge. It is safe on a nil
// receiver.
func (c *BudgetControl) ChargeDoc(n uint64) {
	if c == nil {
		return
	}
	if c.defaultCompute && !c.freeing {
		c.settle()
		c.doc = saturatingAdd(c.doc, n)
		c.limits.Compute = saturatingAdd(c.limits.Compute, n)
		c.refill()
	}
	c.Charge(n)
}

// FreeBegin opens a region in which every reservation is work that only a Document parse does, as
// ChargeDoc reserves it, and returns the mark for FreeEnd, if on holds. The region ends the work it
// covers before any counted work starts. Under a default limit it lifts the limit until FreeEnd;
// otherwise, or in a region, it opens none and returns noFree. It is safe on a nil receiver.
func (c *BudgetControl) FreeBegin(on bool) uint64 {
	if c == nil || !on || !c.defaultCompute || c.freeing {
		return noFree
	}
	c.settle()
	c.freeing, c.freed, c.limits.Compute = true, c.limits.Compute, 0
	c.freeMark = c.base
	c.refill()
	return c.base
}

// FreeEnd ends the region that FreeBegin opened (mark is what it returned, noFree if it opened none):
// the units reserved since the region began are Document units, and the limit is what it was plus
// them.
func (c *BudgetControl) FreeEnd(mark uint64) {
	if mark == noFree || !c.freeing {
		return
	}
	c.settle()
	n := c.base - c.freeMark
	c.doc = saturatingAdd(c.doc, n)
	c.limits.Compute, c.freeing = saturatingAdd(c.freed, n), false
	c.refill()
}

// takeSlow is Take at a poll boundary or a limit: the units between two polls are reserved at once,
// so its time grows with the polls the units reach, not with n, and the polls fall where the one-unit
// reservations put them. A run that does not fit then fails at the unit that does not.
func (c *BudgetControl) takeSlow(n uint64) {
	c.settle()
	fit := n
	if c.limits.Compute != 0 {
		fit = min(n, c.limits.Compute-min(c.base, c.limits.Compute))
	}
	for left := fit; left > 0; {
		if !c.polled {
			c.base = saturatingAdd(c.base, left)
			break
		}
		// k units reach the next poll: k-1 before it, then the unit that polls.
		k := budgetCheckInterval - c.sinceCheck
		if left < k {
			c.base = saturatingAdd(c.base, left)
			c.sinceCheck += left
			break
		}
		c.base = saturatingAdd(c.base, k-1)
		c.sinceCheck += k - 1
		c.chargeSlow(1)
		c.settle()
		left -= k
	}
	c.refill()
	if fit < n {
		c.chargeSlow(1)
	}
}

// chargeStep reserves one unit for a rule call, a loop iteration or a matcher
// step; it does nothing on a nil receiver. n < room is room > 1 for n = 1.
func (c *BudgetControl) chargeStep() {
	if c == nil {
		return
	}
	if c.room > 1 {
		c.room--
	} else {
		c.chargeSlow(1)
	}
}

// chargeSlow is charge at a poll boundary or a limit, or on a nil receiver.
func (c *BudgetControl) chargeSlow(n uint64) {
	if c == nil {
		return
	}
	c.settle()

	exceeded := exceedsLimit(c.base, n, c.limits.Compute)
	poll := c.polled && reachesInterval(c.sinceCheck, n, budgetCheckInterval)

	// Check on each poll boundary and on a failed reservation so an already
	// canceled invocation wins over a simultaneously exhausted budget.
	if poll || exceeded {
		if c.ctx != nil {
			if err := contextErr(c.ctx); err != nil {
				c.interrupted = true
				panic(fatal{err: err})
			}
		}
		if poll && c.limits.Memory != 0 {
			c.checkMemory()
		}
	}
	if exceeded {
		c.interrupted = true
		panic(fatal{err: &sxirruntime.LimitError{
			Kind: sxirruntime.LimitCompute, Limit: c.limits.Compute - c.doc,
			Used: c.base - c.doc, Requested: n, Hotspot: c.obs.hotspot(),
		}})
	}

	c.base = saturatingAdd(c.base, n)
	if poll {
		c.sinceCheck = 0
	} else {
		c.sinceCheck = saturatingAdd(c.sinceCheck, n)
	}
	c.refill()
}

// exceedsLimit checks a reservation without computing a potentially
// overflowing used+requested sum. The limit is already resolved: zero, which
// Unlimited resolves to, sets no bound.
func exceedsLimit(used, requested, limit uint64) bool {
	return limit != 0 && (used > limit || requested > limit-used)
}

// reachesInterval checks whether adding requested would meet or cross interval
// without overflowing. Progress counters are kept below their interval.
func reachesInterval(used, requested, interval uint64) bool {
	return used >= interval || requested >= interval-used
}

// Spend helpers keep long engine-owned scans between bounded reservations.
//
// SpendRead charges the read of n units. It records no input: in a Document parse each matcher
// records the exact end of what it read itself (documentTouch).
func (p *Parser) SpendRead(n uint64) {
	if usedFeatures&featureBudget != 0 && p.Control != nil {
		p.Control.Charge(n)
	}
}

// spendReadOrdinary is SpendRead in an ordinary parse (ExpectOrdinary), which tracks no input.
func (p *Parser) spendReadOrdinary(uint64) {}

// controlled reports whether the parse charges a budget: the grammar uses budgets and the parse
// has a BudgetControl.
func (p *Parser) controlled() bool { return usedFeatures&featureBudget != 0 && p.Control != nil }

// abortParse ends the parse with the error err.
func abortParse(err error) { panic(fatal{err: err}) }

func (p *Parser) Spend() {
	p.Control.chargeStep()
}

// sourceChunk is the bytes of a source that one charge of its preparation covers: one poll
// interval, so that cancellation is observed as often as when each byte is charged alone.
const sourceChunk = int(budgetCheckInterval)

func (p *Parser) setControlledSource(s string) {
	n, valid := 0, true
	// Outside a Document, charge the control directly: SpendRead is too large to inline.
	c := p.Control
	if usedFeatures&featureDocument != 0 && p.Document != nil {
		c = nil
	}
	// Each of the two passes charges one unit per byte of the source: outside a Document in
	// chunks, before the bytes of a chunk are decoded; in a Document by the size
	// of each character.
	for off, charged := 0, 0; off < len(s); {
		if c != nil && off >= charged {
			k := min(sourceChunk, len(s)-charged)
			c.Charge(uint64(k))
			charged += k
		}
		if c != nil {
			// The bytes of the charged chunk that are ASCII are one character each.
			for off < charged && s[off] < utf8.RuneSelf {
				n++
				off++
			}
			if off >= charged {
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[off:])
		if c == nil {
			p.SpendRead(uint64(size))
		}
		if r == utf8.RuneError && size == 1 {
			valid = false
		}
		n++
		off += size
	}
	if cap(p.In) >= n {
		p.In = p.In[:n]
	} else {
		p.In = make([]rune, n)
	}
	p.n = n
	tabled := valid && len(s) <= offsLimit
	var offs []int32
	if tabled {
		if cap(p.offs) >= n+1 {
			offs = p.offs[:n+1]
		} else {
			offs = make([]int32, n+1)
		}
		offs[n] = int32(len(s))
	}
	for off, i, charged := 0, 0, 0; off < len(s); i++ {
		if c != nil && off >= charged {
			k := min(sourceChunk, len(s)-charged)
			c.Charge(uint64(k))
			charged += k
		}
		if c != nil {
			for off < charged && s[off] < utf8.RuneSelf {
				p.In[i] = rune(s[off])
				if tabled {
					offs[i] = int32(off)
				}
				off++
				i++
			}
			if off >= charged {
				i-- // the loop header advances i
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[off:])
		if c == nil {
			p.SpendRead(uint64(size))
		}
		p.In[i] = r
		if tabled {
			offs[i] = int32(off)
		}
		off += size
	}
	p.src, p.offs, p.wide, p.decoded = "", nil, nil, false
	if valid {
		p.src = s
		if tabled {
			p.offs = offs
		}
	}
}

func (p *Parser) literalHasReplacement(rs []rune) bool {
	if usedFeatures&featureBudget != 0 && p.Control != nil {
		return p.literalHasReplacementControlled(rs)
	}
	return slices.Contains(rs, utf8.RuneError)
}

func (p *Parser) literalHasReplacementControlled(rs []rune) bool {
	for _, r := range rs {
		p.Spend()
		if r == utf8.RuneError {
			return true
		}
	}
	return false
}

func (p *Parser) hasLiteralPrefix(text string) bool {
	src := p.bs[p.Pos:]
	if p.Control == nil {
		return strings.HasPrefix(src, text)
	}
	if len(src) < len(text) {
		p.SpendRead(1)
		return false
	}
	for off := 0; off < len(text); {
		n := min(4096, len(text)-off)
		p.SpendRead(uint64(n))
		if src[off:off+n] != text[off:off+n] {
			return false
		}
		off += n
	}
	return true
}

// RunParseConverted shares admission and counters with a Node-to-typed converter.
//
// RunParseConverted is for generated parsers; not a stable API.
func RunParseConverted[T any](r *Rule, input string, o parseOptions, conv func(*sxirvalue.Node, *BudgetControl, error) T) (res T, err error) {
	defer recoverParse(&res, &err)
	o = prepareText(o, input)
	n, err := parseConversionInput(r, input, o)
	o.control.Check()
	res = conv(n, o.control, err)
	o.control.Check()
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

// BudgetRange is for generated parsers; not a stable API.
type BudgetRange struct{ Lo, Hi rune }

// BudgetClass is a character class with more ranges than a matcher tests inline. Testing a
// character charges one unit per range tried, up to the range that holds it.
//
// BudgetClass is for generated parsers; not a stable API.
type BudgetClass struct {
	ranges  []BudgetRange
	negated bool
	// ascii holds, for each character below 128, the units its test charges
	// shifted left by one, with the result of the test in bit 0.
	ascii [128]uint16
}

// NewBudgetClass returns the class of ranges, negated or not.
func NewBudgetClass(ranges []BudgetRange, negated bool) *BudgetClass {
	c := &BudgetClass{ranges: ranges, negated: negated}
	if len(ranges) >= 1<<15 {
		return c
	}
	for ch := rune(0); ch < 128; ch++ {
		cost, reject := len(ranges), !negated
		for i, r := range ranges {
			if ch >= r.Lo && ch <= r.Hi {
				cost, reject = i+1, negated
				break
			}
		}
		c.ascii[ch] = uint16(cost) << 1
		if reject {
			c.ascii[ch] |= 1
		}
	}
	return c
}

// Reject charges the test of ch and reports whether the class rejects it.
func (p *Parser) RejectBudgetClass(ch rune, c *BudgetClass) bool {
	if uint32(ch) < 128 && len(c.ranges) < 1<<15 {
		t := c.ascii[ch]
		p.Control.Take(uint64(t >> 1))
		return t&1 != 0
	}
	for _, r := range c.ranges {
		p.Spend()
		if ch >= r.Lo && ch <= r.Hi {
			return c.negated
		}
	}
	return !c.negated
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

// detachedText constructs normalized text when malformed source bytes prevent
// slicing the original string. Allocation and Grow remain nonpreemptible.
func (p *Parser) detachedText(start, end int) string {
	src := p.In[start:end]
	size := 0
	for off := 0; off < len(src); {
		n := min(spendValueChunk, len(src)-off)
		p.Control.Charge(2 * uint64(n))
		for _, r := range src[off : off+n] {
			width := utf8.RuneLen(r)
			if width < 0 {
				width = utf8.RuneLen(utf8.RuneError)
			}
			size = checkedTextSize(size, width)
		}
		off += n
	}
	var out strings.Builder
	out.Grow(size)
	p.chargeValueBytes(size)
	for off := 0; off < len(src); {
		n := min(spendValueChunk, len(src)-off)
		p.Control.Charge(2 * uint64(n))
		for _, r := range src[off : off+n] {
			out.WriteRune(r)
		}
		off += n
	}
	return out.String()
}

// sortExpected sorts a newly constructed, parser-owned diagnostic in the order of
// compareExpected. Comparisons and explicit moves are bounded; allocation and GC
// remain nonpreemptible.
// Published or memoized errors must never be passed to this helper.
func (p *Parser) sortExpected(values []string) {
	if usedFeatures&featureBudget != 0 && p.Control != nil {
		p.sortExpectedControlled(values)
		return
	}
	sortExpectedEntries(values)
}

func (p *Parser) sortExpectedControlled(values []string) {
	if len(values) < 2 {
		return
	}
	src, dst := values, make([]string, len(values))
	for width := 1; width < len(values); {
		for lo := 0; lo < len(values); {
			mid := lo + min(width, len(values)-lo)
			hi := mid + min(width, len(values)-mid)
			i, j, k := lo, mid, lo
			for i < mid && j < hi {
				order := expectedKind(src[i]) - expectedKind(src[j])
				if order == 0 {
					order = p.compareText(src[i], src[j])
				}
				p.Spend()
				if order <= 0 {
					dst[k] = src[i]
					i++
				} else {
					dst[k] = src[j]
					j++
				}
				k++
			}
			copySpend(p, dst[k:k+mid-i], src[i:mid])
			k += mid - i
			copySpend(p, dst[k:hi], src[j:hi])
			lo = hi
		}
		src, dst = dst, src
		// Stop before doubling can overflow; this pass merged the last run.
		if width >= len(values)-width {
			break
		}
		width *= 2
	}
	if &src[0] != &values[0] {
		copySpend(p, values, src)
	}
}

// RecoveryErrorText is only for engine-owned Error-node materialization.
// Public post-return Error/Message calls remain caller spend. The local builder
// owns its immutable output and is never pooled or written after return.
func (p *Parser) RecoveryErrorText(e *sxirvalue.SyntaxError) string {
	if usedFeatures&featureBudget != 0 && p.Control != nil {
		return p.recoveryErrorTextControlled(e)
	}
	return e.Error()
}

func (p *Parser) recoveryErrorTextControlled(e *sxirvalue.SyntaxError) string {
	prefix := fmt.Sprintf("%d:%d: ", e.Line, e.Col) // bounded integer formatting
	intro, sep, tail := "", "; ", ""
	parts := e.Messages
	if len(parts) == 0 {
		var rest int
		parts, rest = e.ExpectedInMessage()
		sep = ", "
		intro = "syntax error"
		if len(parts) > 0 {
			intro += ": expected "
		}
		if rest > 0 {
			tail = expectedTail(rest) // bounded integer formatting
		}
	}
	size := checkedDiagnosticSize(len(prefix), len(intro))
	for i, part := range parts {
		p.Spend()
		if i > 0 {
			size = checkedDiagnosticSize(size, len(sep))
		}
		size = checkedDiagnosticSize(size, len(part))
	}
	size = checkedDiagnosticSize(size, len(tail))
	var out strings.Builder
	out.Grow(size)
	p.writeDiagnosticText(&out, prefix)
	p.writeDiagnosticText(&out, intro)
	for i, part := range parts {
		p.Spend()
		if i > 0 {
			p.writeDiagnosticText(&out, sep)
		}
		p.writeDiagnosticText(&out, part)
	}
	p.writeDiagnosticText(&out, tail)
	return out.String()
}

func (p *Parser) writeDiagnosticText(out *strings.Builder, text string) {
	for off := 0; off < len(text); {
		n := min(spendValueChunk, len(text)-off)
		p.spendValue(n)
		out.WriteString(text[off : off+n])
		off += n
	}
}

func checkedDiagnosticSize(size, width int) int {
	if size < 0 || width < 0 || size > int(^uint(0)>>1)-width {
		panic(fatal{err: fmt.Errorf("diagnostic text exceeds maximum string size")})
	}
	return size + width
}

// makeControlledError owns any partially assembled diagnostic until completion.
// Interrupted invocations release their partial line table with parser scratch.
func (p *Parser) makeControlledError(pos int, expected []ExpID) *sxirvalue.SyntaxError {
	e := &sxirvalue.SyntaxError{Pos: pos}
	for _, x := range expected {
		p.Spend()
		if x == ExpFar {
			continue
		}
		if x&MsgBit != 0 {
			e.Messages = append(e.Messages, p.g.Descs[x & ^MsgBit])
		} else {
			e.Expected = append(e.Expected, p.g.Descs[x])
		}
	}
	p.sortExpected(e.Expected)
	p.chargeError(e)
	if p.Stream != nil {
		e.Line, e.Col = p.streamLineCol(pos)
		return e
	}
	if p.lines == nil {
		p.lines = append(p.lines, 0)
		for i := 0; i < p.n; i++ {
			p.SpendRead(1)
			if p.unit == sxirinput.Bytes && p.bs[i] == '\n' || p.unit == sxirinput.CodePoints && p.In[i] == '\n' {
				p.lines = append(p.lines, i+1)
			}
		}
	}
	pos = min(pos, p.n)
	lo, hi := 0, len(p.lines)
	for lo < hi {
		p.Spend()
		mid := lo + (hi-lo)/2
		if p.lines[mid] < pos+1 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	e.Line, e.Col = lo, pos-p.lines[lo-1]+1
	p.Control.Check()
	return e
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

// identEqual compares two key values for a lookup of a rule that only a Document parse memoizes: strings
// are equal if they are the same bytes of the same memory, which costs a constant; a value that is not
// identical is a miss, and a miss is sound, since the rule is then evaluated and that work counts.
func (p *Parser) identEqual(l, r any) bool {
	if s, ok := l.(string); ok {
		t, ok := r.(string)
		return ok && sameData(s, t)
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
// the call paid for the lookup of its key (ChargeKey), which the size of the key bounds. A lookup of a
// rule that only a Document memoizes compares by identity, which costs a constant per variable.
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

// stamp marks an entry as current in this parse.
func (p *Parser) stamp() uint32 {
	if p.Document == nil {
		return 0
	}
	return p.Document.epoch
}

func (p *Parser) memoKeyEqual(l, r []any) bool {
	if len(l) != len(r) {
		return false
	}
	for i, v := range l {
		p.Spend()
		if !p.rawValueEqual(v, r[i]) {
			return false
		}
	}
	return true
}

// bindControlled is bind under budget controls. It charges one unit for the lookup of the old
// value and one for each node that a rebind copies, which are the levels+1 nodes on the path to
// the slot; comparing the values is charged by rawValueEqual.
func (p *Parser) bindControlled(slot int, val any) {
	p.Spend()
	if p.rawValueEqual(p.Env.get(slot, p.envDepth), val) {
		return
	}
	for l := 0; l <= p.envDepth; l++ {
		p.Spend()
	}
	// Saved environments are immutable; only publish a completed path.
	p.Env = p.Env.set(slot, p.envDepth, val)
	p.chargeValueBytes(int(uint64(p.envDepth+1) * envNodeBytes))
}

func (p *Parser) controlledEnvValues(slots []int) []any {
	if len(slots) == 0 {
		return nil
	}
	if p.Env == nil && len(slots) <= len(noVals) {
		return noVals[:len(slots):len(slots)]
	}
	vals := make([]any, len(slots))
	for i, slot := range slots {
		p.Spend()
		if slot >= 0 {
			vals[i] = p.variable(slot)
		}
	}
	return vals
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
	if usedFeatures&featureBudget != 0 && p.Control != nil {
		return p.memoGetControlled(rule, pos, min, key, docOnly)
	}
	return p.memo.get(rule, pos, min, key)
}

// memoGetControlled is memoGet under budget controls. The lookup charges nothing: the call paid for
// the lookup of its key (ChargeKey). The walk compares at most memoScan entries and then probes the
// index of the position, so it costs at most a constant plus the size of the key.
func (p *Parser) memoGetControlled(rule, pos, min int, key []any, ident bool) *memoEntry {
	// ident is set for a rule that only a Document parse memoizes: the lookup compares keys by identity and
	// walks docOnlyScan entries (see there).
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
	p.unlinkLagged(i, link, -1)
	if *link == nil {
		return nil
	}
	ix := indexAt(&t.index, &t.indexBytes, i, t.slots[i], func(e *memoEntry) *memoEntry { return e.next }, entryHash)
	_, x := ix.probe(keyHash(int32(rule), int32(min), key), func(x *memoEntry) bool {
		return x.rule == int32(rule) && x.min == int32(min) && keysEqual(x.env, key)
	})
	return x
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
// lookup of its key (ChargeKey), and an entry is hashed once, when it joins the index.
func keyHash(rule, min int32, env []any) uint64 {
	h, _ := memoKeyHash(rule, min, false, env)
	return h
}

// entryHash is keyHash for the key of e.
func entryHash(e *memoEntry) uint64 { return keyHash(e.rule, e.min, e.env) }

// ChargeKey charges the lookup of the memo key of a call of r, a rule that a parse from scratch
// memoizes (budget.chargeKey): one unit for each variable r reads and one for each byte of each
// string value among them, whether or not the call looks the key up.
func (p *Parser) ChargeKey(r *Rule) {
	if usedFeatures&featureBudget != 0 && p.Control != nil && len(r.varSlots) > 0 {
		p.Control.Take(p.keySize(r))
	}
}

// keySize returns the units of the memo key of a call of r (ChargeKey).
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
	h, bytes := memoKeyHash(rule, min, false, env)
	for range 1 + len(env) {
		p.Spend()
	}
	if bytes > 0 {
		p.spendValue(bytes)
	}
	return h
}

// memoPut stores e under the key, unless the join with the entry stored there keeps that entry
// (joinStored), and returns the entry it keeps.
func (p *Parser) memoPut(rule, pos, min int, key []any, docOnly bool, e *memoEntry) *memoEntry {
	if p.Stream != nil {
		if usedFeatures&featureBudget != 0 && p.Control != nil {
			p.Control.noteStored(pos)
		}
		if pos -= p.Stream.memoBase; pos < 0 {
			return e
		}
	}
	if usedFeatures&featureBudget != 0 && p.Control != nil {
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

// memoPutControlled is memoPut under budget controls. The store charges nothing: it follows the
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
		p.unlinkLagged(i, link, -1)
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
	if usedFeatures&featureBudget != 0 && p.Control != nil {
		return p.valueFieldControlled(fs, name)
	}
	return fs.Get(name)
}

func (p *Parser) valueFieldControlled(fs valueimpl.Fields, name string) (any, bool) {
	for _, f := range fs {
		p.Spend()
		if p.rawValueEqual(f.Name, name) {
			return f.Value, true
		}
	}
	return nil, false
}
func (p *Parser) setValueField(fs *valueimpl.Fields, name string, v any) {
	if usedFeatures&featureBudget != 0 && p.Control != nil {
		p.setValueFieldControlled(fs, name, v)
		return
	}
	valueimpl.SetField(fs, name, v)
}

func (p *Parser) setValueFieldControlled(fs *valueimpl.Fields, name string, v any) {
	for i := range *fs {
		p.Spend()
		if p.rawValueEqual((*fs)[i].Name, name) {
			(*fs)[i].Value = v
			return
		}
	}
	// Adding the field is constant work, which the step that sets it has paid.
	*fs = append(*fs, valueimpl.NodeField{Name: name, Value: v})
}
func (p *Parser) pushKid(v *sxirvalue.Node) { p.KidStack = append(p.KidStack, v) }

// undoCaptures is frame.undo, which charges nothing: captures are not an item of the cost.
func (p *Parser) undoCaptures(from int) {
	for i := len(p.Trail) - 1; i >= from; i-- {
		u := p.Trail[i]
		u.f.Vals[u.slot] = u.old
	}
}

// actionErrorText accounts for eager engine-owned error text, including errors
// discarded by predicates. Counter reservations precede each fragment copy.
func (p *Parser) actionErrorText(parts ...string) string {
	size := 0
	for _, part := range parts {
		p.Spend()
		size = checkedDiagnosticSize(size, len(part))
	}
	var out strings.Builder
	out.Grow(size)
	for _, part := range parts {
		p.Spend()
		p.writeDiagnosticText(&out, part)
	}
	return out.String()
}

// evalErrorValue returns the failure of an action expression with the message that format makes of args, which a
// composition returns (see actionError).
func (p *Parser) evalErrorValue(format string, args ...any) evalError {
	if p != nil && usedFeatures&featureBudget != 0 && p.Control != nil {
		return p.evalErrorfControlled(format, args...)
	}
	return evalError{fmt.Sprintf(format, args...)}
}

func (p *Parser) evalErrorfControlled(format string, args ...any) evalError {
	parts := make([]string, 0, 2*len(args)+1)
	arg, start := 0, 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		parts = append(parts, format[start:i])
		i++
		if i >= len(format) || arg >= len(args) {
			panic("invalid internal action-error format")
		}
		switch format[i] {
		case 's':
			parts = append(parts, args[arg].(string))
		case 'd':
			parts = append(parts, strconv.Itoa(args[arg].(int)))
		default:
			panic("unsupported internal action-error format")
		}
		arg++
		start = i + 1
	}
	if arg != len(args) {
		panic("invalid internal action-error arguments")
	}
	parts = append(parts, format[start:])
	return evalError{p.actionErrorText(parts...)}
}
func (p *Parser) wrapEvalError(where, message string) error {
	if p.Control == nil {
		return fmt.Errorf("action in %s: %s", where, message)
	}
	return errors.New(p.actionErrorText("action in ", where, ": ", message))
}
