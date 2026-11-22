// Package lowerdata holds what the lowering from Core IR to Flow IR (flowlower) takes: the functions
// of a table, each in its form, with the options they are lowered with. It holds no pass code, so the
// planning code, the driver and the backends name these types without importing the lowering.
package lowerdata

import (
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/core"
)

// Form selects the function a Unit is lowered to.
type Form int

const (
	// Flat is a local function of a rule (flowlower.Lower): Node, Slots and Build are unused.
	Flat Form = iota
	// Frame is a framed function (flowlower.LowerFrame).
	Frame
	// Outline is a function that a CallOutline of a framed function runs (flowlower.LowerOutline).
	Outline
)

// Func is one function to lower, as the planner of a table (plandata.FuncSpec) lists it: the rule
// Rule in the form Form, named Name. A framed function or an outline lowers the node Body with the
// capture scope Slots, building its value if Build; a flat function lowers the body of Rule, and
// Body, Slots and Build are unused.
type Func struct {
	Form  Form
	Rule  *core.Rule
	Name  string
	Body  core.Node
	Slots []core.Slot
	Build bool
	// Stream lowers the #stream repetition (core.Stream) of a framed Node function as the repetition that
	// delivers its elements in a stream parse: it hands each committed element to the callback instead of
	// gathering it, when the invocation streams (StreamOn), and gathers it otherwise. The function is the
	// body of the start rule that ParseStream runs.
	Stream bool
	// Fallback is, for a flat function, the function that the table runs in its place if the
	// function is refused (Unit.Refuse), or nil if the rule has none.
	Fallback *Func
}

// Unit is what one run of the lowering takes: the Core module Module of a table, on which core.Analyze
// must have run, the options Opts of its functions, and the functions Funcs in order. The lowering
// only reads Module, so the units of the tables of a program may share it.
type Unit struct {
	Module *core.Module
	Opts   Options
	Funcs  []Func
	// Refuse, if not nil, decides on each flat function right after it is lowered, before any pass:
	// it is called with the index of the function in Funcs and the function as the lowering wrote
	// it, and a function that it refuses is replaced in the module by its lowered Fallback, or left
	// out if it has none. The planner of the table decides (flowplan.Refused).
	Refuse func(i int, f *code.Func) bool
}

// LogicalGuards selects what the guard of an alternative does under logical compute units.
type LogicalGuards uint8

const (
	// GuardsExact keeps a guard when skipping the alternative can leave the state that evaluating
	// it leaves: when core.GuardCalls describes the calls of the alternative, whose effects the skip
	// makes (flow.skippedCalls). Every other guard is left out, and the alternative is tried.
	GuardsExact LogicalGuards = iota
	// GuardsEvent keeps only the guards whose skip makes no call, so that every call is counted
	// where it happens: the reference of the tests of GuardsExact.
	GuardsEvent
	// GuardsKeep keeps every guard and makes none of the effects of the calls it skips. It is not
	// exact, and measures what the effects cost.
	GuardsKeep
)

// Options selects the forms the lowering writes.
type Options struct {
	// Budget charges one compute unit on each loop iteration and for each character a Peek reads;
	// the method that holds a rule's body charges its entry.
	Budget bool
	// Logical emits the step of each loop iteration without Budget, as the check of a counter of
	// logical compute units (design record 076); nothing else charges.
	Logical bool
	// Guards selects what the guards of choices do under Logical.
	Guards LogicalGuards
	// Recovers reports that the module can append recovered errors (flowlower.Recovers), so backtracking
	// points save their number.
	Recovers bool
	// Observe reports the alternatives that the guards of choices skip (ObserveSkip).
	Observe bool
	// Node builds the values of the Node runtime (NewTerminalNode, NewBranchNode) in place of the typed
	// ones, in the functions it lowers (flow.Attrs.Node). It holds for every function of a module's lowering,
	// so the options of the Node and recognizer tables are never shared with the typed table's.
	Node bool
	// Document lowers for a build with Documents. A parse of a Document records how far it examined the input
	// in the primitives that read it; the form that reads input in place (HasLit) is then valid only with
	// the recording primitive (MatchLit) as its alternative, which the target takes when the parse is a
	// Document's. The Flow verifier rejects an in-place read without one in a function lowered with it (flow.Attrs.Document).
	Document bool
	// Resume lowers the repetitions that a Document parse resumes (core.Repeat.Resume) with the
	// operations of their runs (flow.resumeStart and the rest), in the Node functions of a lowering for
	// Documents. It is transitional: it holds for the backends that implement the operations, so that
	// the others lower as before until they do, and goes away when every backend does, after which
	// every Document lowering resumes the runs.
	Resume bool
	// Traced lowers the traced copy of a rule that an observed parse runs (flow.Attrs.Traced): every read of the
	// input records how far the parse examined, and the Flow verifier rejects a read in place in the function.
	Traced bool
	// Outline, if not nil, selects the nodes of a framed function that run as functions of their own: the
	// function calls them (CallOutline) under its frame and lowers them with flowlower.LowerOutline. A node it selects
	// must be flowlower.Outlinable; the lowering keeps every node in a function lowered with Observe.
	Outline func(core.Node) bool
}
