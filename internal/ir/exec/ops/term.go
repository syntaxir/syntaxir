package ops

import "zeroparse.dev/zp/internal/ir/exec"

// The handle type of applying a term.
const (
	// Term is a term of the grammar, a predicate's or a binding's, in the form its target holds: code that a
	// Call with Fn applies to a context. The application returns the value of the term and its failure, which
	// is null when the term does not fail. A term that fails, on a list element or a conversion it cannot
	// make, may leave the values it gathered and made; term.dropKids and term.dropCreated release them.
	// An interruption ends the parse and leaves them too, for the next parse starts without them.
	Term = "Term"
)

// The components of what terms make.
const (
	// Gathered is the stack of the elements gathered for the lists and tuples that terms make.
	Gathered = "gathered"
	// Made is the list of the structs that terms and actions made.
	Made = "made"
)

// QDropped is the number of the entries above the mark of the call, which it drops: elements gathered
// (term.dropKids) or values made (term.dropCreated).
const QDropped = "dropped"

// AddTermCode adds the handle type and the operation of applying a term of a predicate or a binding to m,
// which declares the handles Datum, Fail and Act and the component Made (AddValue), the register Pos and the
// component Host (Pilot and Transitional). A term is lowered from Core IR to a function of Exec IR that each
// target holds in its own form; the type Term, whose application the verifier checks, is how Exec IR names
// that form without writing the function in the module. Applying a term charges nothing itself: what a term
// charges is what the operations of its code charge.
func AddTermCode(m *exec.Module) {
	datum, fail := exec.THandle(Datum), exec.THandle(Fail)
	act := exec.THandle(Act)
	m.AddCallable(Term, Host, "A term of a predicate or a binding, in the form the target holds: code that is applied to a context.",
		[]exec.Type{act}, []exec.Type{datum, fail})
	m.AddOp(&exec.Op{Name: "term.ctx", Component: Host, Results: []exec.Type{act}, Reads: []string{Host, Pos, Made}, Writes: []string{Host}, Host: true,
		Contract: `Returns the context in which a term runs: the current capture frame, an empty span at the
position, and the mark of the values made so far, as term.created returns it.`})
}

// addGathered adds the component Gathered and its operations, which push the elements of the lists and
// tuples that a term makes on the stack and drop the elements of a term that the parser abandons. A list
// or a tuple takes its elements off the stack in one piece when it is made, which the lowering of terms
// does (lower.OpTermElemsAbove). The stack is the parser's: grammar code that collects the elements of a
// repetition may use it too, above the elements it finds and taking off what it pushes, so that what a
// term finds is what it left.
func addGathered(m *exec.Module, act exec.Type, items exec.Type) {
	m.AddComponent(Gathered, `The elements gathered for the lists and tuples that terms make: a stack, on which
act.push and act.pushAll push, from which a list or a tuple takes its elements when it is made, and from which
term.dropKids drops the elements of a term that the parser abandons.`)
	c := exec.P("c", act)
	m.AddOp(&exec.Op{Name: "act.push", Component: Gathered, Params: []exec.Param{c, exec.P("e", exec.THandle(Elem))},
		Writes: []string{Gathered}, NoReturn: true,
		Contract: `Pushes e on the elements gathered, as the next element of the innermost list or tuple that c is
making.`})
	m.AddOp(&exec.Op{Name: "act.pushAll", Component: Gathered, Params: []exec.Param{c, exec.P("items", items)},
		Writes: []string{Gathered}, NoReturn: true, Cost: exec.Cost{Terms: []exec.CostTerm{{Coef: 1, Quantity: QSeqLen}}},
		Contract: `Pushes the elements of items in order, as act.push pushes each. A target that bounds the work charges one
unit for each element it copies.`,
		Requires: `items came from seq.items.`})
	m.AddOp(&exec.Op{Name: "term.kids", Component: Gathered, Results: []exec.Type{exec.TInt}, Reads: []string{Gathered},
		Contract: `Returns the number of the elements gathered, which term.dropKids takes as a mark.`})
	m.AddOp(&exec.Op{Name: "term.dropKids", Component: Gathered, Params: []exec.Param{exec.P("mark", exec.TInt)},
		Writes: []string{Gathered}, NoReturn: true, Cost: exec.Cost{Terms: []exec.CostTerm{{Coef: 1, Quantity: QDropped}}},
		Contract: `Drops the elements gathered after the mark, and charges one unit for each.`,
		Requires: `mark came from term.kids and is at most the number of the elements gathered.`})
}

// addMade adds the component Made and its operations, which record the structs that terms and actions make
// and forget those of a term that the parser abandons. The context of a term or an action holds the mark
// of the list when it is made (term.ctx); a struct recorded after the mark of an action's context that is
// the action's result takes the range of the rule, and the parser then forgets what the action recorded.
func addMade(m *exec.Module, act exec.Type, n exec.Param) {
	m.AddComponent(Made, `The values that terms and actions made: a list of the structs that act.made recorded,
which the context of a term or an action marks when it is made. The struct that an action returns takes the
range of the rule if the action recorded it, and what a term or an action recorded is forgotten when it ends.`)
	m.AddOp(&exec.Op{Name: "act.made", Component: Made, Params: []exec.Param{exec.P("c", act), n}, Writes: []string{Made}, NoReturn: true,
		Contract: `Records n after the values made, as a value that c made.`,
		Requires: `n is a struct that c made, and no act.made recorded it.`})
	m.AddOp(&exec.Op{Name: "term.created", Component: Made, Results: []exec.Type{exec.TInt}, Reads: []string{Made},
		Contract: `Returns the number of the values made, which term.dropCreated takes as a mark.`})
	m.AddOp(&exec.Op{Name: "term.dropCreated", Component: Made, Params: []exec.Param{exec.P("mark", exec.TInt)},
		Writes: []string{Made}, NoReturn: true, Cost: exec.Cost{Terms: []exec.CostTerm{{Coef: 1, Quantity: QDropped}}},
		Contract: `Forgets the values made after the mark, without changing them, and charges one unit for each.`,
		Requires: `mark came from term.created and is at most the number of the values made.`})
}
