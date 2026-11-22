package state

import "syntaxir.dev/sxir/internal/runtime/limits"

// Poll counts one step of work that reserves no unit, such as the reuse of an element of a run of a
// Document, toward the interval at which the parse observes cancellation and its memory limit, and
// observes them when the interval ends, as a charge of one unit does (budget.poll). Work that charges
// nothing then still lets a cancellation end the parse within an interval of steps.
func (p *Parser) Poll() {
	if BudgetControlsEnabled && p.Control != nil {
		p.Control.Poll()
	}
}

// Poll is Parser.Poll on the control. It is safe on a nil receiver.
func (c *BudgetControl) Poll() {
	if c == nil {
		return
	}
	if !limits.ReachesInterval(c.SinceCheck, 1, BudgetCheckInterval) {
		c.SinceCheck++
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
