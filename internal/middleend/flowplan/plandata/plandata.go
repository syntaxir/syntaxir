// Package plandata holds what the planning of a rule table (flowplan) works with and gives: the
// configuration and the Core IR of a table, its plan, the functions of the plan by their use, and the
// table unit, which carries the plan and the module of the table through the stages of its pipeline,
// with the kinds and lenses that the pass manager knows them by. It holds no pass code, so the
// lowering, the driver and the backends name these types without importing the planning.
package plandata

import (
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/pass/pipe"
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
	// Outlined is a node that a function of the table runs as a function of its own
	// (flow.CallOutline), which the lowering of the table adds after the planned functions; Line
	// numbers the outlined nodes of a rule in the order the lowering reaches them.
	Outlined
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
	// Logical keeps the step of each loop iteration, as the check of the counter of logical compute
	// units (lowerdata.Options.Logical).
	Logical bool
	// Guards selects what the guards of choices do under Logical (lowerdata.Options.Guards).
	Guards lowerdata.LogicalGuards
	// Observe reports the rules a parse enters and the alternatives that guards skip.
	Observe bool
	// Document records how far each call examines the input, as a build with Documents and a traced
	// parse do.
	Document bool
	// Resume lowers the repetitions that a Document parse resumes with the operations of their runs
	// (lowerdata.Options.Resume): set for a parser with Documents.
	Resume bool
	// Traced selects the traced bodies.
	Traced bool
	// Inserts lowers the #missing sites with their insertions (lowerdata.Options.Inserts), for a
	// module that parses inserting missing tokens run; without it they lower as their literals.
	Inserts bool
}

// TableIR is the Core IR of a Node or recognizer table after its Core passes, the options that
// every function of the table is lowered with, and the Action module of the table.
type TableIR struct {
	// M is built with its origin map (core.Module.Origin), which the passes keep.
	M *core.Module
	// Values says that the actions of M build values (TableConfig.Values).
	Values bool
	Opts   lowerdata.Options
	// Actions is the Action module of M: the functions that evaluate the action expressions of its
	// sites (action.Site), as the Action segment of the pipeline lowered them; no module before it.
	Actions code.At[code.Action]
}

// TableCoreIR is the kind of a table as the Action segment lowers it: its Core IR with the options of
// the table.
var TableCoreIR = pipe.RegisterIR(&pipe.IR[*TableIR]{Name: "table@core"})
