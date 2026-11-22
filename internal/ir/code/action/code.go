package action

import "syntaxir.dev/sxir/internal/ir/code"

// The handle type of applying an action expression.
const (
	// Action is an action expression of the grammar, a predicate's or a binding's, in the form its target holds: code that a
	// Call with Fn applies to a context. The application returns the value of the action expression and its failure, which
	// is null when the action expression does not fail. An action expression that fails, on a list element or a conversion it cannot
	// make, may leave the values it gathered and made; action.dropKids and action.dropCreated release them.
	// An interruption ends the parse and leaves them too, for the next parse starts without them.
	Action = "Action"
)

// The components of what action expressions make.
const (
	// Gathered is the stack of the elements gathered for the lists and tuples that action expressions make.
	Gathered = "gathered"
	// Made is the list of the structs that action expressions and actions made.
	Made = "made"
)

// QDropped is the number of the entries above the mark of the call, which it drops: elements gathered
// (action.dropKids) or values made (action.dropCreated), or which it takes off the elements gathered
// (action.elemsAbove).
const QDropped = "dropped"

// addActionCode adds the handle type and the operation of applying an action expression of a predicate or a binding to m,
// which declares the handles Datum, Fail and Act and the component Made (addValue), the register Pos and the
// component Host (memoizedCall and addParsing). An action expression is lowered from Core IR to a function of Exec IR that each
// target holds in its own form; the type Action, whose application the verifier checks, is how Exec IR names
// that form without writing the function in the module. Applying an action expression costs nothing itself: the work of an action
// expression is that of the operations of its code. A target that bounds the work (the budget feature)
// charges it; a parse that counts compute units counts only the units operations of the expression's
// action (spec/runtime/parsing.md#compute-units).
func addActionCode(m *code.Module) {
	datum, fail := code.THandle(Datum), code.THandle(Fail)
	act := code.THandle(Act)
	m.AddCallable(Action, Host, "An action expression of a predicate or a binding, in the form the target holds: code that is applied to a context.",
		[]code.Type{act}, []code.Type{datum, fail})
	m.AddOp(&code.Op{Name: "action.ctx", Component: Host, Results: []code.Type{act}, Reads: []string{Host, Pos, Made}, Writes: []string{Host}, Kind: code.OpHost,
		Contract: `Returns the context in which an action expression runs: the current capture frame, an empty span at the
position, and the mark of the values made so far, as action.created returns it.`})
}

// addGathered adds the component Gathered and its operations, which push the elements of the lists and
// tuples that an action expression makes on the stack, take them off in one piece when the list or the tuple is made,
// and drop the elements of an action expression that the parser abandons. The stack is the parser's: grammar code that
// collects the elements of a repetition may use it too, above the elements it finds and taking off what
// it pushes, so that what an action expression finds is what it left.
func addGathered(m *code.Module, act code.Type, items code.Type) {
	m.AddComponent(Gathered, `The elements gathered for the lists and tuples that action expressions make: a stack, on which
act.push, act.pushAll and action.keep push, from which a list or a tuple takes its elements when it is made
(action.elemsAbove), and from which action.dropKids drops the elements of an action expression that the parser abandons.`)
	c := code.P("c", act)
	m.AddOp(&code.Op{Name: "act.push", Component: Gathered, Params: []code.Param{c, code.P("e", code.THandle(Elem))},
		Writes: []string{Gathered}, NoReturn: true,
		Contract: `Pushes e on the elements gathered, as the next element of the innermost list or tuple that c is
making.`})
	m.AddOp(&code.Op{Name: "act.pushAll", Component: Gathered, Params: []code.Param{c, code.P("items", items)},
		Writes: []string{Gathered}, NoReturn: true, Cost: code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QSeqLen}}},
		Contract: `Pushes the elements of items in order, as act.push pushes each. A target that bounds the work charges one
unit for each element it copies.`,
		Requires: `items came from seq.items.`})
	m.AddOp(&code.Op{Name: "action.kids", Component: Gathered, Results: []code.Type{code.TInt}, Reads: []string{Gathered},
		Contract: `Returns the number of the elements gathered, which action.dropKids takes as a mark.`})
	m.AddOp(&code.Op{Name: "action.dropKids", Component: Gathered, Params: []code.Param{code.P("mark", code.TInt)},
		Writes: []string{Gathered}, NoReturn: true, Cost: code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QDropped}}},
		Contract: `Drops the elements gathered after the mark, at a cost of one step for each.`,
		Requires: `mark came from action.kids and is at most the number of the elements gathered.`})
	m.AddOp(&code.Op{Name: "action.elemsAbove", NoReturn: true, Component: Gathered, Params: []code.Param{code.P("mark", code.TInt)}, Results: []code.Type{items},
		Reads: []string{Gathered}, Writes: []string{Gathered}, Cost: code.Cost{Terms: []code.CostTerm{{Coef: 2, Quantity: QDropped}}},
		Contract: `Takes the elements gathered after the mark off the elements gathered and returns them in order, for a
list or a tuple to take over. It costs two steps for each: one for copying it and one for taking it off, as
action.dropKids costs for dropping it, which a target that bounds the work charges in one reservation before
it moves any.`,
		Requires: `mark came from action.kids and is at most the number of the elements gathered.`})
	m.AddOp(&code.Op{Name: "action.keep", Component: Gathered, Params: []code.Param{code.P("items", items), code.P("i", code.TInt)},
		Writes: []string{Gathered}, NoReturn: true,
		Contract: `Pushes element i of items as it is (a box stays a box), as act.push pushes an element, as the next
element of the list that filter makes.`,
		Requires: `items came from seq.items, and i is at least 0 and less than the number of its elements.`})
}

// addMade adds the component Made and its operations, which record the structs that action expressions and actions make
// and forget those of an action expression that the parser abandons. The context of an action expression or an action holds the mark
// of the list when it is made (action.ctx); a struct recorded after the mark of an action's context that is
// the action's result takes the range of the rule, and the parser then forgets what the action recorded.
func addMade(m *code.Module, act code.Type, n code.Param) {
	m.AddComponent(Made, `The values that action expressions and actions made: a list of the structs that act.made recorded,
which the context of an action expression or an action marks when it is made. The struct that an action returns takes the
range of the rule if the action recorded it, and what an action expression or an action recorded is forgotten when it ends.`)
	m.AddOp(&code.Op{Name: "act.made", Component: Made, Params: []code.Param{code.P("c", act), n}, Writes: []string{Made}, NoReturn: true,
		Contract: `Records n after the values made, as a value that c made.`,
		Requires: `n is a struct that c made, and no act.made recorded it.`})
	m.AddOp(&code.Op{Name: "action.created", Component: Made, Results: []code.Type{code.TInt}, Reads: []string{Made},
		Contract: `Returns the number of the values made, which action.dropCreated takes as a mark.`})
	m.AddOp(&code.Op{Name: "action.dropCreated", Component: Made, Params: []code.Param{code.P("mark", code.TInt)},
		Writes: []string{Made}, NoReturn: true, Cost: code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QDropped}}},
		Contract: `Forgets the values made after the mark, without changing them, at a cost of one step for each.`,
		Requires: `mark came from action.created and is at most the number of the values made.`})
}
