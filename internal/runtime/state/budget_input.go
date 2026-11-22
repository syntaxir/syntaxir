package state

import (
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// controlled reports whether the parse charges a budget: budget controls are compiled in and the
// parse has a BudgetControl.
func (p *Parser) controlled() bool { return BudgetControlsEnabled && p.Control != nil }

// countsMemory is the member of the shared mechanisms that reports whether the parse counts the memory
// of the values it builds. The engine counts it whenever the parse has a budget control (controlled).
func (p *Parser) countsMemory() bool { return p.controlled() }

// valueUnits is the member of the shared mechanisms that counts the n units of a sized event of a
// value (the bytes that a string comparison or concatenation covers, or the recovered-error entries
// that a store takes), and reports whether the parse counts compute units. The engine counts them in
// its budget control, so a shared mechanism that it reports true to neither charges the event again
// nor reads the bytes in chunks.
func (p *Parser) valueUnits(n int) bool {
	if !p.controlled() {
		return false
	}
	p.Control.Charge(uint64(n))
	return true
}

// spendValue is the member of the shared mechanisms (value_spend.go) that charges the work on values
// done in chunks. In the engine that work is no event, so it counts nothing: the events among it are
// counted where they happen (valueUnits, and the entries of recovered errors that a store takes,
// rec.entries).
func (p *Parser) spendValue(int) {}

// spendMemo is the member of the shared mechanisms (context_table.go) that charges a step of the
// work of the dependency memo of left recursion. That work is no event, so it counts nothing in the
// engine (spec/runtime/parsing.md#compute-units).
func (p *Parser) spendMemo() {}

// stepElem is seq.step: a step over an element of a list that an action expression makes (hull) or checks against
// a type (elemsConform) is no event, so it counts nothing; the actions count the elements of the
// lists they build by the units operations (spec/runtime/parsing.md#compute-units).
func (p *Parser) stepElem() {}

// CheckUnits is units.check. The engine checks each count as it makes it (BudgetControl.Charge), as
// a parse may, so a check after the counts has nothing left to compare.
func (p *Parser) CheckUnits() {}

// countUnits counts the n units of a sized event of the parse, such as the position units of input
// that a terminal advanced over or the elements of a list that an action built
// (spec/runtime/parsing.md#compute-units), and checks the counter.
func (p *Parser) countUnits(n int) {
	if BudgetControlsEnabled && p.Control != nil && n > 0 {
		p.Control.Charge(uint64(n))
	}
}

// Advance moves the position past the size units of a character that a terminal matched, and counts
// them.
func (p *Parser) Advance(size int) {
	p.Pos += size
	p.countUnits(size)
}

// RangeText is node.rangeText: the input of the range [start, end) of a node. In a stream, the text
// of a node is an event of one unit for each position unit of its result
// (spec/runtime/parsing.md#compute-units), which it counts before it makes the text.
func (p *Parser) RangeText(start, end int) string {
	if p.Stream {
		p.countUnits(end - start)
	}
	return p.Text(start, end)
}

// HeldText is node.heldText: the text that the terminal node n holds, and whether it holds one. In a
// stream, the text that text takes from a string node is the event that RangeText counts, of the
// width of the node's range; an error node, whose text is the input of its range, counts there.
func (p *Parser) HeldText(n *value.Node) (string, bool) {
	d := nodes.D(n)
	if d.Terminal && p.Stream && nodes.ShapeOf(n) != valueimpl.ShapeError {
		p.countUnits(int(d.End - d.Start))
	}
	return d.Text, d.Terminal
}

// abortParse ends the parse with the error err.
func abortParse(err error) { panic(Fatal{Err: err}) }

// Spend counts one compute unit, the unit of an event such as a call of a rule, and checks the
// counter, behind a statically removable boundary.
func (p *Parser) Spend() {
	if BudgetControlsEnabled && p.Control != nil {
		p.Control.Charge(1)
	}
}

// SpendN counts n compute units, the units of a sized event, and checks the counter.
func (p *Parser) SpendN(n uint64) {
	if BudgetControlsEnabled && p.Control != nil {
		p.Control.Charge(n)
	}
}

// NewControlledTextInput prepares the input s of a parse with controls. Preparing the input is no
// event of the parse (spec/runtime/parsing.md#compute-units): the Input limit bounds it, so it counts
// no unit and prepares the input as a parse without controls does, keeping the control for the parse.
func NewControlledTextInput(s string, unit input.PositionUnit, offsets bool, sc *scratch, c *BudgetControl) Input {
	in := newTextInputIn(s, unit, offsets, sc)
	if BudgetControlsEnabled && c != nil {
		in.Control = c
	}
	return in
}

// SpendDoc charges one unit of work that only a Document parse does (BudgetControl.ChargeDoc).
func (p *Parser) SpendDoc() {
	if BudgetControlsEnabled && p.Control != nil {
		p.Control.ChargeDoc(1)
	}
}

// FreeBegin opens a region of Document work (BudgetControl.FreeBegin) if on holds, and returns its mark.
func (p *Parser) FreeBegin(on bool) uint64 {
	if on && BudgetControlsEnabled && p.Control != nil {
		return p.Control.FreeBegin()
	}
	return noFree
}

// FreeEnd ends the region that FreeBegin opened.
func (p *Parser) FreeEnd(mark uint64) {
	if mark != noFree && BudgetControlsEnabled && p.Control != nil {
		p.Control.FreeEnd(mark)
	}
}

// takeEntries counts the entries recorded after the mark m that recSince takes as the list of a
// stored result, one unit for each (spec/runtime/parsing.md#compute-units), as rec.entries does for
// the printed compositions. The iterative VM's growth calls it where the printed growth counts them.
func (p *Parser) takeEntries(m int) {
	if n := len(p.Recovered) - m; n > 0 {
		p.SpendN(uint64(n))
	}
}
