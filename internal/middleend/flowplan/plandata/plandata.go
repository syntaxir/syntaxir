// Package plandata holds what the planning of a rule table (flowplan) works with and gives: the
// configuration and the Core IR of a table, and the functions of its plan by their use. It holds no
// pass code, so the lowering, the driver and the backends name these types without importing the
// planning.
package plandata

import (
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/middleend/flowlower/lowerdata"
)

// Use is what a planned function is in its table.
type Use uint8

const (
	// RuleBody is the body of a rule's table row: a flat rule or a framed body.
	RuleBody Use = iota
	// OperatorsSkip is the skip line of an Operators Rule.
	OperatorsSkip
	// OperatorsLine is a line of an Operators Rule: its operands in order, then its operators in the
	// order of their levels.
	OperatorsLine
	// StreamBody is the body of the start rule that ParseStream runs, whose #stream repetition delivers
	// the elements (lowerdata.Func.Stream).
	StreamBody
)

// Key names a planned function by its use, its rule's ID and, for an Operators Rule line, the line's
// index.
type Key struct {
	Use  Use
	Rule int
	Line int
}

// FuncSpec is one function of a plan: its use in the table and exactly what its lowering takes
// (lowerdata.Func), a flat function or a framed body.
type FuncSpec struct {
	Key Key
	lowerdata.Func
	// Fallback is the framed body of the rule of a flat function, or nil if the rule has none. The plan
	// holds it in place of a flat function that flowplan.Refused rejects.
	Fallback *FuncSpec
}

// TableConfig says how the functions of a Node or recognizer table run: what the parser that runs
// them charges and records.
type TableConfig struct {
	// Values selects the Node table, whose Core IR gathers the values of projected repetitions as its
	// actions read them; false selects the recognizer, whose actions read no projections.
	Values bool
	// Budget charges the budget controls of a parse (lowerdata.Options.Budget).
	Budget bool
	// Observe reports the rules a parse enters and the alternatives that guards skip.
	Observe bool
	// Document records how far each call examines the input, as a build with Documents and a traced
	// parse do.
	Document bool
	// Traced selects the traced bodies.
	Traced bool
}

// TableIR is the Core IR of a Node or recognizer table after its Core passes, and the options that
// every function of the table is lowered with.
type TableIR struct {
	// M is built with its origin map (core.Module.Origin), which the passes keep.
	M    *core.Module
	Opts lowerdata.Options
}
