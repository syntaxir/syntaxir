package state

import (
	"context"
	"errors"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/runtime/limits"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// BudgetCheckInterval is the number of compute units between two observations of cancellation.
const BudgetCheckInterval = uint64(256)

// BudgetControl is private state for a single parser invocation: the counter of its compute units,
// which it checks against the limit and between which it observes the context and the memory limit.
type BudgetControl struct {
	Ctx    context.Context
	Limits runtime.Limits

	// Used is the count of compute units so far; SinceCheck is the part of it since the last
	// observation of cancellation.
	Used       uint64
	SinceCheck uint64

	// Adaptive is set for a stream whose budget is the default: the budget follows the furthest
	// position the parser has demanded from the stream (Demand).
	Adaptive bool

	// Doc is the part of Used that only a Document parse reserves: its work on pending edits, entries,
	// reused values and runs is not an event, so it is no part of the count against any Compute
	// limit, the default or an explicit one (ChargeDoc, FreeBegin). Those units still advance
	// SinceCheck, so that the polls for cancellation and memory keep their cadence inside Document
	// work: a poll is a check, not a charge.
	Doc uint64
	// freeing is set between FreeBegin and FreeEnd; freed holds the limit that FreeBegin lifted.
	freeing  bool
	freed    uint64
	freeMark uint64 // Used when the open region began

	// memoryCounts are the memory the control counts itself; retained is the history a Document
	// retains outside its memo table when its parse starts (SetRetained). The memory check adds the
	// counters of the parser attached by AttachMemory (MemoryUsed).
	memoryCounts
	memP *Parser
	// hotP is the parser whose profile names the hotspot of an exceeded compute limit.
	hotP *Parser
}

// ResolveLimits returns the limits a parse runs under for an input of n units of its position
// unit: a zero Compute selects the default (limits.DefaultBudget), and Unlimited disables the
// budget, which the result leaves zero. An Input of zero, Unlimited or above limits.MaxInput is
// limits.MaxInput. A stream does not know n: its default starts at the floor
// and grows with the furthest position the parser demands (BudgetControl.Demand), which adaptive reports. The reference
// build has no controls to charge, so no default applies there.
func ResolveLimits(l runtime.Limits, n uint64, stream bool) (resolved runtime.Limits, adaptive bool) {
	l.Input = ResolveInput(l.Input)
	if l.Memory == runtime.Unlimited {
		l.Memory = 0 // the default of Memory is Unlimited
	}
	switch l.Compute {
	case runtime.Unlimited:
		l.Compute = 0
	case 0:
		switch {
		case !BudgetControlsEnabled:
		case stream:
			l.Compute, adaptive = limits.DefaultBudgetFloor, true
		default:
			l.Compute = limits.DefaultBudget(n)
		}
	}
	return l, adaptive
}

// ResolveInput returns the Input limit a parse runs under for an Input option of n: limits.MaxInput,
// the default and the longest input of any parse, for zero, Unlimited and any n above it.
func ResolveInput(n uint64) uint64 {
	if n == 0 || n > limits.MaxInput {
		return limits.MaxInput
	}
	return n
}

// Demand raises the default budget of a stream to that of n units, where n is the number of
// units up to the furthest position the parser has asked the stream for. The number depends on the
// input and the grammar only, not on how the reader chunks the input. It does nothing for any
// other budget, and on a nil receiver; the budget never shrinks.
func (c *BudgetControl) Demand(n int) {
	if c == nil || !c.Adaptive || n <= 0 {
		return
	}
	if b := limits.DefaultBudget(uint64(n)); b > c.Limits.Compute {
		c.Limits.Compute = b
	}
}

// NewBudgetControl returns nil when neither cancellation nor a finite compute budget or memory
// limit is active. A nil context is equivalent to context.Background. A context that can never be
// canceled (Done is nil) is never polled.
func NewBudgetControl(ctx context.Context, limits runtime.Limits) *BudgetControl {
	if ctx != nil && ctx.Done() == nil {
		ctx = nil
	}
	if ctx == nil && limits.Compute == 0 && limits.Memory == 0 {
		return nil
	}
	return &BudgetControl{Ctx: ctx, Limits: limits}
}

// NewReportControl returns a control with no context and the limits limits, which hold no compute or
// memory bound: the control of a parse that only counts its units (ParseOptions.ComputeReport).
func NewReportControl(limits runtime.Limits) *BudgetControl { return &BudgetControl{Limits: limits} }

// ContextPanic attributes a panic to caller-owned context code rather than VM
// execution. Parse boundaries re-raise its original value after cleanup.
type ContextPanic struct{ Value any }

// BuildCancelled is the error of a build, made for a parse on first use, that the parse's
// context ended: Err is the context's error, which the parse reports.
type BuildCancelled struct{ Err error }

func (e BuildCancelled) Error() string { return e.Err.Error() }

func contextErr(ctx context.Context) error {
	defer func() {
		if x := recover(); x != nil {
			switch x.(type) {
			case Fatal, ContextPanic:
				panic(x) // preserve engine interruption and an already attributed panic
			default:
				panic(ContextPanic{Value: x})
			}
		}
	}()
	return ctx.Err()
}

// Check observes cancellation, then the memory limit, whatever the units counted since they were
// last observed, as the check at the end of a parse and the engine's explicit checkpoints do. An
// observation that finds neither starts the interval of Charge's polls again. It is safe on a nil
// receiver.
func (c *BudgetControl) Check() {
	if c == nil {
		return
	}
	if c.Ctx != nil {
		if err := contextErr(c.Ctx); err != nil {
			panic(Fatal{Err: err})
		}
	}
	if c.Limits.Memory != 0 {
		c.checkMemory()
	}
	c.SinceCheck = 0
}

// Charge counts n compute units: the units of logical events that have happened, or of a sized
// event whose size is known and whose work follows (spec/runtime/parsing.md#compute-units). It is a
// check (spec/runtime/parsing.md#checks-and-interruptions): it then compares the counter with the
// limit, and observes the context once BudgetCheckInterval units have been counted since it was
// last observed. A counter above the limit stops the parse with a LimitError whose Used is the
// counter, which may exceed the limit by the units of this charge; the units are never taken back.
// It is safe on a nil receiver.
func (c *BudgetControl) Charge(n uint64) {
	if c == nil {
		return
	}
	logCharge(c, n)
	c.Used = limits.SaturatingAdd(c.Used, n)
	c.SinceCheck = limits.SaturatingAdd(c.SinceCheck, n)
	if c.SinceCheck >= BudgetCheckInterval || c.Limits.Compute != 0 && c.Used > c.Limits.Compute {
		c.chargeSlow()
	}
}

// noFree is the mark that FreeBegin returns when it opens no region.
const noFree = ^uint64(0)

// ChargeDoc reserves n units of work that only a Document parse does, as Charge does, and leaves
// them out of the count against the limit, whichever limit it is: the limit rises by n first, so the
// units leave the room that counted work has. An unlimited control has no limit to raise. The units
// are in Used and advance SinceCheck, so cancellation and memory are polled as for any work, and are
// in Doc, which LimitError leaves out. It is safe on a nil receiver.
func (c *BudgetControl) ChargeDoc(n uint64) {
	if c == nil {
		return
	}
	if !c.freeing {
		c.Doc += n
		if c.Limits.Compute != 0 {
			c.Limits.Compute = limits.SaturatingAdd(c.Limits.Compute, n)
		}
	}
	c.Charge(n)
}

// FreeBegin opens a region in which every reservation is work that only a Document parse does, as
// ChargeDoc reserves it, and returns the mark for FreeEnd. The region ends the work it covers before
// any counted work starts. It lifts the limit until FreeEnd; in a region it opens none and returns
// noFree. It is safe on a nil receiver.
func (c *BudgetControl) FreeBegin() uint64 {
	if c == nil || c.freeing {
		return noFree
	}
	c.freeing, c.freed, c.Limits.Compute = true, c.Limits.Compute, 0
	c.freeMark = c.Used
	return c.Used
}

// FreeEnd ends the region that FreeBegin opened (mark is what it returned, noFree if it opened none):
// the units reserved since the region began, or resumed after FreeHold, are Document units, and the
// limit is what it was plus them, or none if it was none.
func (c *BudgetControl) FreeEnd(mark uint64) {
	if mark == noFree || !c.freeing {
		return
	}
	n := c.Used - c.freeMark
	c.Doc += n
	c.Limits.Compute, c.freeing = c.freed, false
	if c.freed != 0 {
		c.Limits.Compute = limits.SaturatingAdd(c.freed, n)
	}
}

// FreeHold ends the open region for work that counts, and reports whether there was one; FreeResume
// opens it again. The FreeEnd of the region that was held takes the work from the resume on.
func (c *BudgetControl) FreeHold() bool {
	if c == nil || !c.freeing {
		return false
	}
	c.FreeEnd(c.freeMark)
	return true
}

// FreeResume opens again the region that FreeHold ended, if it ended one.
func (c *BudgetControl) FreeResume(held bool) {
	if held {
		c.FreeBegin()
	}
}

// chargeSlow is the check of Charge when the counter has reached a poll or exceeds the limit, kept
// out of line so that the counting every event makes stays small. At a poll, once
// BudgetCheckInterval units have been counted since the last observation, it observes the context
// and then the memory limit; it compares the compute limit last, so that a canceled invocation or an
// exceeded memory limit that the check observes wins over an exceeded compute limit
// (spec/runtime/parsing.md#checks-and-interruptions).
func (c *BudgetControl) chargeSlow() {
	if c.SinceCheck >= BudgetCheckInterval {
		c.Check()
	}
	if c.Limits.Compute != 0 && c.Used > c.Limits.Compute {
		panic(Fatal{Err: &runtime.LimitError{
			Kind: runtime.LimitCompute, Limit: c.Limits.Compute - c.Doc,
			Used: c.Used - c.Doc, Hotspot: c.hotspot(),
		}})
	}
}

// hotspot returns the hotspot of the profile that records the parse the control belongs to, or the
// empty string without one (Parser.Hotspot). It runs only when a limit is exceeded.
func (c *BudgetControl) hotspot() string {
	if c.hotP == nil {
		return ""
	}
	return c.hotP.Hotspot()
}

// InputUnits returns the length of s in units of the position unit. Below the size at which the
// default budget leaves its floor the byte length stands in for the count of code points, which it
// bounds from above.
func InputUnits(s string, unit input.PositionUnit) uint64 {
	if unit == input.Bytes || len(s) <= limits.DefaultBudgetFloor/limits.DefaultBudgetPerUnit {
		return uint64(len(s))
	}
	return uint64(utf8.RuneCountInString(s))
}

// ErrNoControls is returned in the reference build when a context or a finite limit is requested.
var ErrNoControls = errors.New("budget controls are unavailable in the reference build")

// CheckInput rejects an input of s that is longer than the resolved Input limit of l, in the position
// unit. The check is O(1) unless the byte length passes the limit in CodePoints, where the exact
// count decides: the byte length bounds the count from above.
func CheckInput(l runtime.Limits, s string, unit input.PositionUnit) error {
	n := uint64(len(s))
	if n > l.Input && unit == input.CodePoints {
		n = uint64(utf8.RuneCountInString(s))
	}
	return CheckInputUnits(l, n)
}

// CheckInputUnits rejects an input of n units that is longer than the resolved Input limit of l
// (ResolveLimits), which is at most limits.MaxInput.
func CheckInputUnits(l runtime.Limits, n uint64) error {
	if l.Input < limits.MaxInput && !BudgetControlsEnabled {
		// The reference build exists only for measurement, so it refuses any Input limit below
		// the longest input, which it still checks.
		return ErrNoControls
	}
	if n > l.Input {
		return &runtime.LimitError{Kind: runtime.LimitInput, Limit: l.Input, Requested: n}
	}
	return nil
}
