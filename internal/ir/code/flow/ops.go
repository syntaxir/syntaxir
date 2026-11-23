package flow

import (
	"reflect"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/core"
)

// The Flow dialect of Code IR.
//
// Grammar code at the Flow stage is a module of Code IR whose functions call the operations that this
// file declares: matching the input, backtracking to a mark, recording expectations, calling rules,
// building values, evaluating action expressions and counting compute units. The structure (blocks,
// loops, branches, exits, assignments and the arithmetic of conditions) is the base's. Each operation
// has a contract, a cost bound, the features it exists in, laws that passes may use, and an
// immediate operand of a declared Go type that holds its static data (a literal, a class, a rule);
// its dynamic operands are arguments. execlower (internal/middleend/execlower) converts each call
// into the operation's reference expansion at the Exec stage.
//
// The cost bound of an operation bounds its work. A function lowered with logical units
// (lowerdata.Options.Logical) counts compute units: each operation counts the logical events that it
// makes (spec/runtime/parsing.md#compute-units), such as the input a terminal advances over, a call
// with its key, a syntax error built or a suffix copy, and nothing else.

// Dialect is the name of the Flow dialect, the Dialect of its operations.
const Dialect = "flow"

// State is the component of the Flow operations: the parser state that grammar code reads and
// writes, as one component. The reference expansion of each operation names the Exec components
// it acts on.
const State = "parser"

// Quantities of the cost bounds of Flow operations.
const (
	// QLit is the number of code points of the operation's literal.
	QLit = "lit"
	// QTerminal is the number of steps of the operation's terminal program.
	QTerminal = "terminal"
	// QClasses is the number of classes of the operation's class run.
	QClasses = "classes"
	// QBody is the units the body of a called rule, outlined node or action expression charges.
	QBody = "body"
	// QText is the number of code points of the input a value copies.
	QText = "text"
	// QMerge is the units that the compositions over the expectation record (expect, keep and
	// mergeExpected of spec/exec-ir/compositions.md#expectations) charge for the expectations that the
	// call records, keeps or merges, as the quantity merge of the Exec stage measures them.
	QMerge = "merge"
	// QCall is the units of the call protocol around the body of the called rule (the composition call
	// of spec/exec-ir/compositions.md#calls), as the operations of the Exec stage that the protocol calls
	// declare them: the unit of the call and the key of a memoized rule. The first-call record, the
	// memo's lookups and stores and the growth of their tables charge nothing.
	QCall = "call"
	// QVals is the number of values that the call returns in its list, which bounds the elements of a
	// record that it copies behind the values of its walk (spec/runtime/parsing.md#run-sites).
	QVals = "vals"
	// QError is the work of building a syntax error: one unit for each expectation that the call is
	// passed, the steps of sorting their descriptions and of finding the line of the error's position
	// in a table of the line starts, and, at the first error of a parse of a whole input, one unit for
	// each code point of the input, which builds that table (spec/runtime/parsing.md#cost).
	QError = "error"
	// QRanges is the number of ranges of the operation's class: of the immediate prim.ClassID, or of
	// the class of the immediate prim.Guard, 0 for a literal.
	QRanges = "ranges"
	// QStream is the units of the work of a stream parse at the call: loading the input into its buffer,
	// compacting the buffer and scanning it for line starts (stream_scan of the Exec stage), and for a
	// delivered element the commit of the composition streamCommit
	// (spec/exec-ir/compositions.md#streams); 0 outside a stream parse.
	QStream = "stream"
	// QErrorText is the size of the message of the recovered error that a value holds: the bytes of its
	// text and one for each of its parts, which a target that bounds the work assembles piece by piece
	// (the quantity error_parts of the Exec stage); spec/runtime/parsing.md#cost charges the message as a
	// value of that size, with one unit more for the value.
	QErrorText = "error_text"
	// QKey is the work of the run key of a walk of a run site that is not free of the environment
	// (spec/runtime/parsing.md#run-sites): reading the values of the variables of the key and hashing
	// them, comparing them with the key of a record, and binding the variables that the site's element
	// binds to the values of a record that the walk takes, each charged as for a memo key; 0 for a
	// skip-run site and a run site free of the environment.
	QKey = "key"
	// QRecords is the number of records that the end of a walk of a skip-run or run site stores: one
	// for each position that the walk visited while recording (spec/runtime/parsing.md#skip-runs).
	QRecords = "records"
	// QWalkVals is the number of values that a walk of a run site whose iterations have values holds
	// at its end: those its iterations gathered and those of the record it took, which the end of the
	// walk lays out for its records, copying those of the record where it cannot share them
	// (spec/runtime/parsing.md#run-sites).
	QWalkVals = "walk_vals"
	// QMade is the number of the values that the context of an action made (spec/exec-ir/values.md#values-made)
	// which finishing the action's value searches for the value: those up to it, all of them when it
	// is a struct that the context did not make, and none when it is no struct.
	QMade = "made"
	// QDeps is the number of the dependencies that a lookahead which ends in a context of left
	// recursion stores with its outcome, as the quantity deps of the Exec stage measures them; 0
	// outside such a context and for a lookahead that read no seed.
	QDeps = "deps"
	// QTable is the work of storing the outcome of a lookahead in the context table at its position,
	// as the quantity table of the Exec stage measures it: one for the store, one for each dependency
	// set of the head it scans and one for each result under the values the set selects that it
	// compares; 0 where QDeps is.
	QTable = "table"
)

// Op names of the Flow dialect.
const (
	OpStep            = "flow.step"
	OpSilent          = "flow.silent"
	OpPeek            = "flow.peek"
	OpAdvance         = "flow.advance"
	OpExpect          = "flow.expect"
	OpObserveSkip     = "flow.observeSkip"
	OpSkippedCalls    = "flow.skippedCalls"
	OpInDocument      = "flow.inDocument"
	OpRestore         = "flow.restore"
	OpMergeExpected   = "flow.mergeExpected"
	OpMergeRenamed    = "flow.mergeRenamed"
	OpPushKid         = "flow.pushKid"
	OpDropKids        = "flow.dropKids"
	OpStreamStart     = "flow.streamStart"
	OpSkipBegin       = "flow.skipBegin"
	OpSkipVisit       = "flow.skipVisit"
	OpSkipEnd         = "flow.skipEnd"
	OpRunBegin        = "flow.runBegin"
	OpRunWalked       = "flow.runWalked"
	OpSkipValue       = "flow.skipValue"
	OpScanEnd         = "flow.scanEnd"
	OpEmitStream      = "flow.emitStream"
	OpAppend          = "flow.append"
	OpSetWhere        = "flow.setWhere"
	OpDropCreated     = "flow.dropCreated"
	OpUnfresh         = "flow.unfresh"
	OpReset           = "flow.reset"
	OpSetCapture      = "flow.setCapture"
	OpClearVals       = "flow.clearVals"
	OpDropTrail       = "flow.dropTrail"
	OpFreeFrame       = "flow.freeFrame"
	OpAppendError     = "flow.appendError"
	OpDeclareNames    = "flow.declareNames"
	OpEnv             = "flow.env"
	OpSetEnv          = "flow.setEnv"
	OpFrame           = "flow.frame"
	OpSetFrame        = "flow.setFrame"
	OpRecLen          = "flow.recLen"
	OpKidsLen         = "flow.kidsLen"
	OpStreamOn        = "flow.streamOn"
	OpSkipHit         = "flow.skipHit"
	OpRunHot          = "flow.runHot"
	OpSkipKids        = "flow.skipKids"
	OpScanReach       = "flow.scanReach"
	OpScanKids        = "flow.scanKids"
	OpResumeOn        = "flow.resumeOn"
	OpResumeStart     = "flow.resumeStart"
	OpResumeMore      = "flow.resumeMore"
	OpResumeNext      = "flow.resumeNext"
	OpResumeBegin     = "flow.resumeBegin"
	OpResumeEnd       = "flow.resumeEnd"
	OpResumeFinish    = "flow.resumeFinish"
	OpHasLit          = "flow.hasLit"
	OpMatchLit        = "flow.matchLit"
	OpMatchClass      = "flow.matchClass"
	OpInClass         = "flow.inClass"
	OpGuardFails      = "flow.guardFails"
	OpGuardLitFails   = "flow.guardLitFails"
	OpAtAnchor        = "flow.atAnchor"
	OpSkipTerminal    = "flow.skipTerminal"
	OpSkipClasses     = "flow.skipClasses"
	OpCallRule        = "flow.callRule"
	OpCallOutline     = "flow.callOutline"
	OpIsolate         = "flow.isolate"
	OpUnisolate       = "flow.unisolate"
	OpText            = "flow.text"
	OpLiteralText     = "flow.literalText"
	OpStr             = "flow.str"
	OpNewMatch        = "flow.newMatch"
	OpNewNode         = "flow.newNode"
	OpNewTerminalNode = "flow.newTerminalNode"
	OpNewBranchNode   = "flow.newBranchNode"
	OpNewVals         = "flow.newVals"
	OpKids            = "flow.kids"
	OpShapeSingle     = "flow.shapeSingle"
	OpShapeCapture    = "flow.shapeCapture"
	OpScopeValue      = "flow.scopeValue"
	OpAttachCaptures  = "flow.attachCaptures"
	OpAttachFrame     = "flow.attachFrame"
	OpMarkState       = "flow.markState"
	OpMarkPos         = "flow.markPos"
	OpNewFrame        = "flow.newFrame"
	OpFrameVal        = "flow.frameVal"
	OpKeep            = "flow.keep"
	OpMakeError       = "flow.makeError"
	OpErrorValue      = "flow.errorValue"
	OpNewErrorNode    = "flow.newErrorNode"
	OpFreshNode       = "flow.freshNode"
	OpConvertResult   = "flow.convertResult"
	OpUseCtx          = "flow.useCtx"
	OpPred            = "flow.pred"
	OpBind            = "flow.bind"
	OpEval            = "flow.eval"
	OpCtxFinish       = "flow.ctxFinish"
)

// NewModule returns an empty module of the Flow and Action dialects with the declarations of the
// Flow dialect, at no stage: flowlower makes it a module at the Flow stage (code.EnterFlow).
func NewModule(name string) *code.Module {
	m := code.NewModule(name)
	m.Dialects = []string{Dialect, "action"}
	Declare(m)
	return m
}

// Declare adds the declarations of the Flow dialect to m: its component, registers, handle types and
// operations.
func Declare(m *code.Module) {
	m.AddComponent(State, "The parser state that grammar code reads and writes.")
	m.AddRegister(prim.Pos, code.TInt, "The position.")
	m.AddRegister(prim.Cut, code.TBool, "The cut flag of the scope the runtime entered last.")
	for _, h := range prim.HandleTypes {
		m.AddHandle(h, State, "A "+h+" of the parser.")
	}
	for _, o := range Ops() {
		m.AddOp(o)
	}
}

func immOf[T any]() reflect.Type { return reflect.TypeFor[T]() }

// Ops returns new declarations of the operations of the Flow dialect.
func Ops() []*code.Op {
	h := code.THandle
	value, env, expmark, expids, vals, ctx := h(prim.TValue), h(prim.TEnv), h(prim.TExpMark), h(prim.TExpIDs), h(prim.TVals), h(prim.TCtx)
	mark, frame, errT, runeT, run := h(prim.TMark), h(prim.TFrame), h(prim.TError), h(prim.TRune), h(prim.TRun)
	b, i := code.TBool, code.TInt
	P := code.P
	all := []string{State, prim.Pos, prim.Cut}
	pos := []string{State, prim.Pos}
	unit := code.Cost{Const: 1}
	per := func(q string) code.Cost { return code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: q}}} }
	// plus adds the term q to the cost c.
	plus := func(c code.Cost, q string) code.Cost {
		c.Terms = append(slices.Clone(c.Terms), code.CostTerm{Coef: 1, Quantity: q})
		return c
	}
	// value1 is the cost of an operation that builds the value of a recovered error: a unit for the
	// value (spec/runtime/parsing.md#cost). The values that the parse builds otherwise charge nothing.
	value1 := unit
	pure := "pure: the operation reads the state and changes nothing, so a call whose result is unused may be removed."
	var ops []*code.Op
	op := func(o *code.Op) *code.Op {
		o.Dialect, o.Component = Dialect, State
		if o.Reads == nil {
			o.Reads = []string{State}
		}
		ops = append(ops, o)
		return o
	}
	withImm := func(o *code.Op, doc string, t reflect.Type) *code.Op {
		o.Imm, o.ImmType, o.FormatImm = doc, t, prim.ImmString
		return o
	}

	// Steps and input.
	op(&code.Op{Name: OpStep, Writes: []string{State}, NoReturn: true, Cost: unit,
		Contract: `The step of an iteration. It counts nothing: the step of an iteration is no event (spec/runtime/parsing.md#compute-units), and it checks the counter of the units, which a
target whose counter checks itself at each event it counts may leave out. It is the step of the loop whose
body holds it, once per iteration, and an iteration that goes on to the next step has advanced the position, so
each step of a loop finds the position past the one of the loop's last step; a target that runs code it did not
make itself, as the bytecode machines do, requires it and ends the parse with an error of invalid code
otherwise (spec/bytecode/README.md#flow-bodies).`})
	withImm(op(&code.Op{Name: OpSilent, Writes: []string{State}, NoReturn: true,
		Cost: code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QTable}, {Coef: 1, Quantity: QDeps}}},
		Contract: `Adds Delta to the silent depth, inside which expectations are not recorded. For a lookahead
(Site is not 0) it begins the lookahead of the site after it enters the depth, or ends it, whose body
matched if Ok, before it leaves. The end of a lookahead in a context of left recursion whose body read
a seed stores its outcome with its dependencies in the context's table, and charges for them as
ctx.keep does (spec/exec-ir/operations.md).`}), "the change of the depth and the lookahead (Silence)", immOf[prim.Silence]())
	op(&code.Op{Name: OpPeek, Results: []code.Type{runeT, i, b}, Reads: pos, Writes: []string{State}, NoReturn: true, Cost: plus(unit, QStream),
		Contract: `Returns the code point at the position, its size and whether there is one, without consuming it.
In a stream parse the read costs what loading the input costs; in a Document parse it records how far the parse examined the input. It
counts no compute unit: examining input without advancing is no event (spec/runtime/parsing.md#compute-units).`})
	op(&code.Op{Name: OpAdvance, Params: []code.Param{P("n", i)}, Reads: pos, Writes: []string{prim.Pos},
		Contract: "Moves the position forward by n. In a function lowered with logical units it counts n units, the input that a terminal advances over (spec/runtime/parsing.md#compute-units).", Requires: "n code units of input follow the position."})
	withImm(op(&code.Op{Name: OpExpect, NoReturn: true, Params: []code.Param{P("at", i)}, Writes: []string{State}, Cost: per(QMerge),
		Contract: "Records the expectation at the position at, unless the silent depth is positive, as the composition expect does (spec/exec-ir/compositions.md#expectations)."}),
		"the expectation (Expectation)", immOf[prim.Expectation]())
	withImm(op(&code.Op{Name: OpObserveSkip, Writes: []string{State}, NoReturn: true,
		Contract: "Reports to the observation of the parse that the guard of a choice skips the alternative, which begins with the rule call that the immediate names."}),
		"the skipped call (ObservedSkip)", immOf[prim.ObservedSkip]())
	withImm(op(&code.Op{Name: OpSkippedCalls, Writes: []string{State}, NoReturn: true,
		Contract: "Makes the effects of the calls that the alternative a guard skips would make under logical compute units, as the evaluation of the alternative makes them: one unit for each call and the units of the key of each memoized rule, the seen mark and policy counts of each memoized call, and the failure entry that a call stores when its memoization is not deferred, with the expectation of the terminal. A call that a stored entry answers ends the calls, as it ends the evaluation. It checks the counter. Only a lowering with logical units makes the call."}),
		"the calls (SkippedCalls)", immOf[prim.SkippedCalls]())
	op(&code.Op{Name: OpInDocument, Results: []code.Type{b}, Laws: []string{pure},
		Contract: "Reports that the parse is a Document's. A guard whose skip passes over a call of a rule that a Document memoizes skips only outside Documents: a Document parse evaluates the alternative, where the memo records how far each entry examined the input and what later parses reuse (spec/runtime/documents.md#reuse). It counts no event and charges nothing."})
	op(&code.Op{Name: OpRestore, Params: []code.Param{P("pos", i), P("rec", i)}, Writes: []string{State, prim.Pos},
		Contract: "Moves the position back to pos and drops the recovered errors after the first rec."})
	op(&code.Op{Name: OpMergeExpected, NoReturn: true, Params: []code.Param{P("far", i), P("exps", expids)}, Writes: []string{State}, Cost: per(QMerge),
		Contract: "Merges the expectations exps, recorded at far, into the enclosing expectation scope, as the composition mergeExpected does (spec/exec-ir/compositions.md#expectations)."})
	withImm(op(&code.Op{Name: OpMergeRenamed, NoReturn: true, Params: []code.Param{P("far", i), P("exps", expids)}, Writes: []string{State}, Cost: per(QMerge),
		Contract: "Merges the expectations exps, recorded at far, into the enclosing scope, renamed to the expectation of the immediate (@diagnostic), as the composition expect does for it (spec/exec-ir/compositions.md#expectations)."}),
		"the expectation (prim.ExpID)", immOf[prim.ExpID]())
	op(&code.Op{Name: OpPushKid, Params: []code.Param{P("x", value)}, Writes: []string{State},
		Contract: "Pushes x on the stack of gathered values. It charges nothing: the values that the parse gathers are not an item of the cost (spec/runtime/parsing.md#cost)."})
	op(&code.Op{Name: OpDropKids, Params: []code.Param{P("base", i)}, Writes: []string{State},
		Contract: "Truncates the stack of gathered values to base.", Requires: "base is at most the height of the stack."})
	op(&code.Op{Name: OpStreamStart, Writes: []string{State}, NoReturn: true,
		Contract: "Begins the stream of a stream parse: the repetition that delivers its elements is about to run.",
		Requires: "flow.streamOn holds."})
	withImm(op(&code.Op{Name: OpSkipBegin, Writes: []string{State},
		Contract: `Begins a walk of the skip-run site at the position: the iterations that follow are its visits, until
flow.skipEnd. Beginning a walk charges nothing (spec/runtime/parsing.md#skip-runs).`}),
		"the site (Site)", immOf[prim.Site]())
	withImm(op(&code.Op{Name: OpRunBegin, Writes: []string{State},
		Contract: `Begins a walk of the hot run site at the position, as flow.skipBegin does for a skip-run site: the
iterations that follow are its visits, until flow.skipEnd or flow.skipKids. With Build its iterations have
values, which flow.skipValue adds and flow.skipKids gathers in a list; with KeyEnv its records are kept for
the position and the values of the variables of the site's key, otherwise for the position alone. Beginning
a walk charges nothing (spec/runtime/parsing.md#run-sites).`,
		Requires: "flow.runHot of the site holds."}),
		"the walk (RunWalk)", immOf[prim.RunWalk]())
	op(&code.Op{Name: OpSkipVisit, NoReturn: true, Reads: pos, Writes: []string{State}, Cost: code.Cost{Terms: []code.CostTerm{{Coef: 2, Quantity: QMerge}, {Coef: 1, Quantity: QKey}}},
		Contract: `Ends the iteration of the walk before, if any, and begins one at the position. In a recording walk
it keeps the expectations of the iteration before and records them in the enclosing result, and reads the
key of the walk at the position (spec/runtime/parsing.md#skip-runs, #run-sites).`})
	op(&code.Op{Name: OpSkipValue, Params: []code.Param{P("x", value)}, Writes: []string{State},
		Contract: `Adds the value x of the iteration that matched to the walk begun with Build, and marks x, if it is a
node, as no longer owned by its maker: the records of the walk share it. Adding the value charges nothing;
flow.skipKids charges for it (spec/runtime/parsing.md#run-sites).`,
		Requires: "the walk in progress was begun with Build."})
	op(&code.Op{Name: OpSkipEnd, NoReturn: true, Reads: pos, Writes: []string{State},
		Cost: code.Cost{Terms: []code.CostTerm{{Coef: 2, Quantity: QMerge}, {Coef: 1, Quantity: QKey}, {Coef: 1, Quantity: QRecords}, {Coef: 1, Quantity: QWalkVals}}},
		Contract: `Ends the iteration of the walk before, if any, ends the walk and records the rest of it from each
position it visited while recording, a unit for each record stored (spec/runtime/parsing.md#skip-runs). A
walk begun with Build lays out the values it holds for its records and drops them, which charges nothing,
and copies the elements of the record it took where it cannot share them, a unit for each
(spec/runtime/parsing.md#run-sites).`})
	withImm(op(&code.Op{Name: OpRunWalked, Params: []code.Param{P("start", i)}, Reads: pos, Writes: []string{State},
		Contract: `Ends the walk of the run site that was not hot (flow.runHot), which began at start and ends at the
position: a walk that began below the furthest position that the walks of the site passed, and walked
RunHotSpan or more of the input below it, turns the site hot; the furthest position then moves to the
position. A walk shorter than RunHotSpan changes nothing. Noting the end of a walk at a site that is not
hot is free (spec/runtime/parsing.md#run-sites).`,
		Requires: "start is at most the position."}), "the site (Site)", immOf[prim.Site]())
	withImm(op(&code.Op{Name: OpScanEnd, Params: []code.Param{P("start", i), P("count", i)}, Reads: pos, Writes: []string{State},
		NoReturn: true, Cost: code.Cost{Const: 1},
		Contract: `Ends a walk of the value-free scan site that began at start and matched count characters, at least
ScanHotSpan: unless the walk took the rest from a record, it records itself at a hot site, which charges a
unit for the record, or notes that it walked at a site that is not hot, which is free
(spec/runtime/parsing.md#scan-sites).`}), "the site (Site)", immOf[prim.Site]())
	op(&code.Op{Name: OpEmitStream, Params: []code.Param{P("x", value)}, Reads: pos, Writes: []string{State}, NoReturn: true, Cost: per(QStream),
		Contract: "Delivers the committed element x (nil for a value-free repetition) to the callback of a stream parse, which has consumed the input up to the position."})
	withImm(op(&code.Op{Name: OpAppend, Params: []code.Param{P("vals", vals), P("x", value)}, Results: []code.Type{vals},
		Contract: "Returns the list vals with x appended; with Spread, the children of the tuple node x."}),
		"whether the children are appended (Spread)", immOf[prim.Spread]())
	withImm(op(&code.Op{Name: OpSetWhere, Writes: []string{State},
		Contract: "Names the rule whose action runs, for the messages of its errors."}), "the rule's name (Chars)", immOf[prim.Chars]())
	op(&code.Op{Name: OpDropCreated, Params: []code.Param{P("c", ctx)}, Writes: []string{State},
		Contract: "Drops the structs made since the context c began."})
	op(&code.Op{Name: OpUnfresh, Params: []code.Param{P("x", value)}, Writes: []string{State},
		Contract: "Marks the node x, if x is one, as no longer owned by its maker."})
	op(&code.Op{Name: OpReset, Params: []code.Param{P("m", mark)}, Writes: []string{State, prim.Pos},
		Contract: "Returns to the parser state of the mark m: the position and the parts that flow.markState named. Undoing the captures written after m charges nothing, as the composition reset does (spec/exec-ir/compositions.md)."})
	withImm(op(&code.Op{Name: OpSetCapture, Params: []code.Param{P("x", value)}, Writes: []string{State},
		Contract: "Sets the capture slot of the current frame to x."}), "the slot (Slot)", immOf[prim.Slot]())
	op(&code.Op{Name: OpClearVals, Params: []code.Param{P("f", frame)}, Writes: []string{State},
		Contract: "Sets the values of the frame f to nil."})
	op(&code.Op{Name: OpDropTrail, Params: []code.Param{P("m", mark)}, Writes: []string{State},
		Contract: "Drops the trail entries recorded since the mark m."})
	op(&code.Op{Name: OpFreeFrame, Params: []code.Param{P("f", frame)}, Writes: []string{State},
		Contract: "Returns the frame f to the runtime.", Requires: "f is not used after the call."})
	op(&code.Op{Name: OpAppendError, Params: []code.Param{P("x", errT)}, Writes: []string{State},
		Contract: "Appends the syntax error x to the recovered errors."})
	withImm(op(&code.Op{Name: OpDeclareNames,
		Contract: "Declares the slot names of a frame scope, which framed repetitions name before their element and declare after it. It does nothing to the state."}),
		"the names (*Names)", immOf[*prim.Names]())

	// Registers that hold handles.
	op(&code.Op{Name: OpEnv, Results: []code.Type{env}, Contract: "Returns the variable environment.", Laws: []string{pure}})
	op(&code.Op{Name: OpSetEnv, Params: []code.Param{P("e", env)}, Writes: []string{State}, Contract: "Sets the variable environment to e."})
	op(&code.Op{Name: OpFrame, Results: []code.Type{frame}, Contract: "Returns the current capture frame.", Laws: []string{pure}})
	op(&code.Op{Name: OpSetFrame, Params: []code.Param{P("f", frame)}, Writes: []string{State}, Contract: "Makes f the current capture frame."})

	// Expressions.
	op(&code.Op{Name: OpRecLen, Results: []code.Type{i}, Contract: "Returns the number of recovered errors.", Laws: []string{pure}})
	op(&code.Op{Name: OpKidsLen, Results: []code.Type{i}, Contract: "Returns the height of the stack of gathered values.", Laws: []string{pure}})
	op(&code.Op{Name: OpStreamOn, Results: []code.Type{b},
		Contract: "Reports that the invocation delivers the elements of its #stream repetition: a stream parse with a callback, at the top call of its start rule.", Laws: []string{pure}})
	op(&code.Op{Name: OpSkipHit, NoReturn: true, Results: []code.Type{b}, Reads: pos, Writes: []string{State, prim.Pos},
		Cost: code.Cost{Const: 2, Terms: []code.CostTerm{{Coef: 3, Quantity: QMerge}, {Coef: 1, Quantity: QKey}}},
		Contract: `Ends the iteration of the walk before, if any, and reports whether the walk has a record of the
position for its key; it then takes the rest of the walk from the record, which moves the position to its
end, records the recorded expectations again, binds the variables that the element of a keyed run site
binds to the recorded values, and the walk stops. A walk begun with Build takes no record without values.
In a recording walk it charges a unit for the lookup and one more when the slot is recorded
(spec/runtime/parsing.md#skip-runs), besides the expectations and the key.`})
	withImm(op(&code.Op{Name: OpRunHot, Results: []code.Type{b}, Laws: []string{pure},
		Contract: `Reports whether the walks of the run site keep records, separately inside and outside lookaheads. A
walk of a site that is not hot is an ordinary repetition, which flow.runWalked ends. Testing whether the
site is hot is free (spec/runtime/parsing.md#run-sites).`}), "the site (Site)", immOf[prim.Site]())
	op(&code.Op{Name: OpSkipKids, NoReturn: true, Results: []code.Type{vals}, Reads: pos, Writes: []string{State},
		Cost: code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QVals}, {Coef: 2, Quantity: QMerge}, {Coef: 1, Quantity: QKey}, {Coef: 1, Quantity: QRecords}}},
		Contract: `Ends the walk begun with Build as flow.skipEnd does, and returns the child list of the values of its
iterations, then those of the record it took, as flow.kids does for a repetition without records; the list
may share its elements with the lists and records of other walks. Taking the values off charges nothing
(spec/runtime/parsing.md#cost); it charges the expectations, the key, a unit for each record stored and a
unit for each element of the record that it copies (spec/runtime/parsing.md#run-sites).`,
		Requires: "the walk in progress was begun with Build."})
	withImm(op(&code.Op{Name: OpScanReach, Params: []code.Param{P("start", i), P("count", i)}, Results: []code.Type{i}, Reads: pos, Writes: []string{State, prim.Pos},
		NoReturn: true, Cost: code.Cost{Const: 2},
		Contract: `For a walk of the scan site that began at start and has matched ScanHotSpan characters, count of
them: at a hot site with a record of start it moves the position to the recorded end and returns the
recorded count; otherwise it returns count. At a hot site it charges a unit for the lookup and one more for
the record that it finds; at a site that is not hot it charges nothing (spec/runtime/parsing.md#scan-sites).`}), "the site (Site)", immOf[prim.Site]())
	withImm(op(&code.Op{Name: OpScanKids, Params: []code.Param{P("start", i), P("base", i), P("count", i)}, Results: []code.Type{vals}, Reads: pos, Writes: []string{State},
		NoReturn: true, Cost: code.Cost{Const: 1},
		Contract: `Returns the elements of a walk of the scan site that began at start, gathered on the stack from
base, which matched count characters: for a walk that reached ScanHotSpan characters it ends the walk as
flow.scanEnd does, and the result is the elements of the record that the walk took, if it took one (the
stack is dropped to base), or the gathered elements, which the record that it makes shares. It charges a
unit for the record that it makes, as flow.scanEnd does (spec/runtime/parsing.md#scan-sites), and nothing
for the elements: the values that the parse gathers are not an item of the cost
(spec/runtime/parsing.md#cost).`}), "the site (Site)", immOf[prim.Site]())

	// The runs of the repetitions that a Document parse resumes (spec/runtime/documents.md#reuse-model),
	// each the composition of the same name with the prefix run (spec/exec-ir/compositions.md#repetition-runs).
	// They count no event and declare no cost: a reused element charges no unit, and flow.resumeNext polls.
	op(&code.Op{Name: OpResumeOn, Results: []code.Type{b}, Laws: []string{pure},
		Contract: `Reports that the repetitions resume their runs: the parse is a Document's, outside lookaheads and
the evaluation of a skip expression (the silent depth is 0).`})
	withImm(op(&code.Op{Name: OpResumeStart, Results: []code.Type{run}, Reads: pos, Writes: []string{State},
		Contract: `Starts the run of the repetition at the position and returns it, with the record of the run that it
takes over, as the composition runStart does.`,
		Requires: "flow.resumeOn holds."}), "the repetition (Resume)", immOf[prim.Resume]())
	op(&code.Op{Name: OpResumeMore, Params: []code.Param{P("r", run)}, Results: []code.Type{b}, Laws: []string{pure},
		Contract: "Reports whether the run r may take another element: no reused element ended it, and it has fewer than its maximum (runMore)."})
	op(&code.Op{Name: OpResumeNext, Params: []code.Param{P("r", run)}, Results: []code.Type{value, b}, Reads: pos, Writes: []string{State, prim.Pos}, NoReturn: true,
		Contract: `Reuses the next element of the run r that the record it took over holds, as the composition runNext
does: it replays what evaluating the element did (the position, the examined input, the expectations and the
statistics) and returns its value and true, for the caller to keep. It returns false when the repetition
evaluates its next element instead. A reused element counts no unit (spec/runtime/parsing.md#compute-units): the
call checks the limits, which may end the parse, in place of the step of the iteration that keeps the element.`,
		Requires: "flow.resumeMore of r holds."})
	op(&code.Op{Name: OpResumeBegin, Params: []code.Param{P("r", run)}, Reads: pos, Writes: []string{State},
		Contract: `Starts evaluating an element of the run r, after the step of its iteration: its examined input and its
expectations are recorded apart from those of the enclosing call (runBegin).`})
	op(&code.Op{Name: OpResumeEnd, Params: []code.Param{P("r", run), P("ok", b), P("x", value)}, Reads: pos, Writes: []string{State},
		Contract: `Ends evaluating the element of the run r that flow.resumeBegin started, with its success ok and its
value x, before a failed element resets the state: it adds the element's examined input and expectations to
those of the enclosing call, and records the element if it matched (runEnd).`})
	op(&code.Op{Name: OpResumeFinish, Params: []code.Param{P("r", run)}, Writes: []string{State},
		Contract: `Records the run r of a repetition that matched, before its value is built (runFinish). A repetition that
fails drops its run without a call.`})
	withImm(op(&code.Op{Name: OpHasLit, Results: []code.Type{b}, Reads: pos, Writes: []string{State}, NoReturn: true, Cost: per(QLit),
		Contract: `Reports that the input at the position begins with the literal, read in place without recording
anything; false where the input cannot be read in place.`, Laws: []string{"implies flow.matchLit: where it holds, flow.matchLit of the literal matches."}}),
		"the literal (prim.Lit)", immOf[prim.Lit]())
	withImm(op(&code.Op{Name: OpMatchLit, Results: []code.Type{value, b}, Reads: pos, Writes: []string{State, prim.Pos}, NoReturn: true, Cost: plus(per(QLit), QMerge),
		Contract: `Matches the literal at the position and moves past it, recording its expectation on failure as the
composition expect does. Its results are its value (not used) and whether it matched; on failure the position
is unchanged. It counts the position units it
advances over, none on failure (spec/runtime/parsing.md#compute-units).`}),
		"the literal (prim.Lit)", immOf[prim.Lit]())
	withImm(op(&code.Op{Name: OpMatchClass, Results: []code.Type{b}, Reads: pos, Writes: []string{State, prim.Pos}, NoReturn: true, Cost: plus(unit, QMerge),
		Contract: "Matches one character of the class through its membership table and moves past it, recording the class's expectation on failure as the composition expect does; reports whether it matched. It counts the position units of the character it advances over, none on failure (spec/runtime/parsing.md#compute-units)."}),
		"the class (prim.ClassID)", immOf[prim.ClassID]())
	withImm(op(&code.Op{Name: OpInClass, NoReturn: true, Params: []code.Param{P("ch", runeT)}, Results: []code.Type{b}, Cost: per(QRanges),
		Contract: "Reports whether the code point ch is in the class. It counts no compute unit: the test examines a character without advancing.", Laws: []string{pure}}), "the class (prim.ClassID)", immOf[prim.ClassID]())
	withImm(op(&code.Op{Name: OpGuardFails, NoReturn: true, Params: []code.Param{P("ch", runeT), P("more", b)}, Results: []code.Type{b}, Cost: per(QRanges),
		Contract: "Reports that the guard of a class skips its alternative: the character that more (there is one) and ch describe is not in the class, and the nesting limit allows the guard's depth of calls. A guard counts no compute unit: it examines the next character without advancing, which is no event (spec/runtime/parsing.md#compute-units).",
		Laws:     []string{pure}}), "the guard (prim.Guard)", immOf[prim.Guard]())
	withImm(op(&code.Op{Name: OpGuardLitFails, Results: []code.Type{b}, Reads: pos, Writes: []string{State}, NoReturn: true, Cost: plus(unit, QStream),
		Contract: `Reports that the guard of a literal skips its alternative: the input at the position does not begin with
the literal's first character, and the nesting limit allows the guard's depth of calls. It compares the character
as the literal does and examines what the literal would: in code points the character at the position; in bytes
the UTF-8 encoding of the character byte by byte, up to the first byte that differs, or the end of the input,
except U+FFFD, which also matches one invalid byte and so decodes the character; a folded character matches an
ASCII letter in either case. It reads no further input than it examines. In a stream parse the read costs what
loading the input costs; in a Document parse it records how far the parse examined the input. A
guard counts no compute unit: it examines the next character without advancing, which is no event
(spec/runtime/parsing.md#compute-units).`}),
		"the guard (prim.Guard)", immOf[prim.Guard]())
	withImm(op(&code.Op{Name: OpAtAnchor, NoReturn: true, Results: []code.Type{b}, Reads: pos, Cost: unit,
		Contract: "Reports whether the position is at the anchor. It counts no compute unit: it examines input without advancing.", Laws: []string{pure}}), "the anchor (prim.Anchor)", immOf[prim.Anchor]())
	withImm(op(&code.Op{Name: OpSkipTerminal, Results: []code.Type{b}, Reads: pos, Writes: []string{State, prim.Pos}, NoReturn: true, Cost: per(QTerminal),
		Contract: "Runs the terminal region as one program from the position and reports whether it matched; on a match the position moves past it. It counts the position units that each of its terminals advances over, again after the program backtracks (spec/runtime/parsing.md#compute-units)."}),
		"the terminal region (*core.Terminal)", immOf[*core.Terminal]())
	withImm(op(&code.Op{Name: OpSkipClasses, Results: []code.Type{b}, Reads: pos, Writes: []string{State, prim.Pos}, NoReturn: true, Cost: per(QClasses),
		Contract: `Matches the classes of the run in turn and reports whether they matched; on a match the position moves past them.
On failure the position is past the classes that matched, at the one that did not, whose expectation it records
there; the code that fails returns to its mark. It counts the position units of each character it advances
over, those before a class that fails included (spec/runtime/parsing.md#compute-units).`}),
		"the class run (prim.ClassRun)", immOf[prim.ClassRun]())
	withImm(op(&code.Op{Name: OpCallRule, Results: []code.Type{value, b}, Reads: all, Writes: all, NoReturn: true,
		Cost: code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QBody}, {Coef: 1, Quantity: QCall}}},
		Contract: `Calls the rule from the Operators Rule level through the composition call
(spec/exec-ir/compositions.md#calls). Its results are the rule's value and whether it matched; on a match the
position is the end of the rule's match, on failure it is unchanged. Under logical units the call counts its unit
and the key of a memoized rule, as the composition call does (spec/runtime/parsing.md#compute-units).`}),
		"the rule and the level (RuleCall)", immOf[prim.RuleCall]())
	withImm(op(&code.Op{Name: OpCallOutline, Results: []code.Type{value, b}, Reads: all, Writes: all, NoReturn: true, Cost: per(QBody),
		Contract: "Runs the node of the outlined call, which a function of its own lowers, under the current frame. Its results are the value of the node, built if Build, and whether it matched."}),
		"the outlined call (Outline)", immOf[prim.Outline]())
	op(&code.Op{Name: OpIsolate, Results: []code.Type{expmark}, Reads: pos, Writes: []string{State},
		Contract: "Begins an expectation scope at the position and returns its mark."})
	op(&code.Op{Name: OpUnisolate, Params: []code.Param{P("m", expmark)}, Results: []code.Type{i, expids}, Writes: []string{State},
		Contract: "Ends the expectation scope m. Its results are the farthest failure inside it and its expectations. It charges nothing, as exp.unisolate (spec/exec-ir/operations.md): each expectation was charged when it was recorded, and a merge of them charges each again.",
		Requires: "m is the innermost scope that has not ended."})
	op(&code.Op{Name: OpText, NoReturn: true, Params: []code.Param{P("start", i)}, Results: []code.Type{value}, Reads: pos, Cost: plus(unit, QText),
		Contract: "Returns the input from start to the position. In a stream parse it charges at most a unit and the length of its result, and elsewhere at most a unit (spec/runtime/parsing.md#cost).", Laws: []string{pure}})
	withImm(op(&code.Op{Name: OpLiteralText, NoReturn: true, Params: []code.Param{P("start", i)}, Results: []code.Type{value}, Reads: pos, Cost: plus(unit, QText),
		Contract: "Returns the text of the literal matched from start, which differs from the literal where it holds the replacement character. Where it differs it takes the input from start to the position, charged as flow.text charges it.", Laws: []string{pure}}),
		"the literal's text (Chars)", immOf[prim.Chars]())
	withImm(op(&code.Op{Name: OpStr, Results: []code.Type{value}, Contract: "Returns the string constant.", Laws: []string{pure}}),
		"the string (Chars)", immOf[prim.Chars]())
	op(&code.Op{Name: OpNewMatch, Params: []code.Param{P("start", i), P("text", value)}, Results: []code.Type{value}, Reads: pos, Writes: []string{State},
		Contract: "Returns a match value from start to the position with the text."})
	withImm(op(&code.Op{Name: OpNewNode, Params: []code.Param{P("start", i), P("kids", vals)}, Results: []code.Type{value}, Reads: pos, Writes: []string{State},
		Contract: "Returns a tuple node, or with List a list node, from start to the position with the children kids."}),
		"whether the node is a list (List)", immOf[prim.List]())
	op(&code.Op{Name: OpNewTerminalNode, Params: []code.Param{P("start", i), P("text", value)}, Results: []code.Type{value}, Reads: pos, Writes: []string{State},
		Contract: "Returns a terminal of the Node runtime from start to the position, with the text."})
	withImm(op(&code.Op{Name: OpNewBranchNode, Params: []code.Param{P("start", i), P("kids", vals)}, Results: []code.Type{value}, Reads: pos, Writes: []string{State},
		Contract: "Returns a branch of the Node runtime from start to the position: a sequence, or with List a list, of the children kids."}),
		"whether the node is a list (List)", immOf[prim.List]())
	withImm(op(&code.Op{Name: OpNewVals, Results: []code.Type{vals}, Writes: []string{State},
		Contract: "Returns an empty list of values with room for the immediate."}), "the room (Room)", immOf[prim.Room]())
	op(&code.Op{Name: OpKids, Params: []code.Param{P("base", i)}, Results: []code.Type{vals}, Writes: []string{State},
		Contract: "Returns the list of the values gathered above base, which it removes from the stack. It charges nothing: the values that the parse gathers are not an item of the cost (spec/runtime/parsing.md#cost)."})
	withImm(op(&code.Op{Name: OpShapeSingle, Params: []code.Param{P("x", value), P("start", i)}, Results: []code.Type{value, b}, Reads: pos, Writes: []string{State},
		Contract: "Returns the value of x, matched from start by an expression of the shape, where it takes one element, and whether it is a single element (not used)."}),
		"the shape (prim.Shape)", immOf[prim.Shape]())
	withImm(op(&code.Op{Name: OpShapeCapture, Params: []code.Param{P("x", value), P("start", i)}, Results: []code.Type{value}, Reads: pos, Writes: []string{State},
		Contract: "Returns the value of x, matched from start by an expression of the shape, where it is captured."}),
		"the shape (prim.Shape)", immOf[prim.Shape]())
	withImm(op(&code.Op{Name: OpScopeValue, Params: []code.Param{P("x", value), P("start", i)}, Results: []code.Type{value}, Reads: pos, Writes: []string{State},
		Contract: "Returns the value of a scope whose body, of the shape, matched x from start, building a tuple for it if the shape needs one."}),
		"the shape (prim.Shape)", immOf[prim.Shape]())
	withImm(op(&code.Op{Name: OpAttachCaptures, Params: []code.Param{P("x", value), P("start", i), P("caps", value)}, Variadic: true,
		Results: []code.Type{value}, Reads: pos, Writes: []string{State},
		Contract: "Returns x, matched from start, with the captures of the scope attached: the values caps of the scope's slots."}),
		"the names and the source of the captures (Attach)", immOf[prim.Attach]())
	withImm(op(&code.Op{Name: OpAttachFrame, Params: []code.Param{P("x", value), P("start", i), P("f", frame)},
		Results: []code.Type{value}, Reads: pos, Writes: []string{State},
		Contract: "Returns x, matched from start, with the captures of the scope attached: the values of the frame f."}),
		"the names, with Frame set (Attach)", immOf[prim.Attach]())
	withImm(op(&code.Op{Name: OpMarkState, Results: []code.Type{mark}, Reads: all, Writes: []string{State},
		Contract: "Returns a mark of the parser state, which flow.reset returns to: the position, and the variable environment, the capture trail and the recovered errors as the immediate names them."}),
		"the parts the mark restores (StateMark)", immOf[prim.StateMark]())
	op(&code.Op{Name: OpMarkPos, Params: []code.Param{P("m", mark)}, Results: []code.Type{i},
		Contract: "Returns the position of the mark m.", Laws: []string{pure}})
	withImm(op(&code.Op{Name: OpNewFrame, Results: []code.Type{frame}, Writes: []string{State},
		Contract: "Returns a new frame for the scope of the names."}), "the names (*Names)", immOf[*prim.Names]())
	withImm(op(&code.Op{Name: OpFrameVal, Params: []code.Param{P("f", frame)}, Results: []code.Type{value},
		Contract: "Returns the value of the slot of the frame f.", Laws: []string{pure}}), "the slot (Slot)", immOf[prim.Slot]())
	op(&code.Op{Name: OpKeep, NoReturn: true, Params: []code.Param{P("exps", expids)}, Results: []code.Type{expids}, Writes: []string{State}, Cost: per(QMerge),
		Contract: "Returns the expectations exps, kept beyond the scope that recorded them, as the composition keep does (spec/exec-ir/compositions.md#expectations)."})
	op(&code.Op{Name: OpMakeError, NoReturn: true, Params: []code.Param{P("far", i), P("exps", expids)}, Results: []code.Type{errT}, Writes: []string{State}, Cost: per(QError),
		Contract: "Returns the syntax error at far with the expectations exps, which a recovery builds: one compute unit, a syntax error built (spec/runtime/parsing.md#compute-units). The cost bound is the work of building it (spec/runtime/parsing.md#cost)."})
	op(&code.Op{Name: OpErrorValue, NoReturn: true, Params: []code.Param{P("start", i), P("x", errT)}, Results: []code.Type{value}, Reads: pos, Writes: []string{State}, Cost: plus(value1, QErrorText),
		Contract: "Returns the value of the recovered error x over the input from start to the position. Its message is a value that the parse builds, which counts nothing (spec/runtime/parsing.md#compute-units); the cost bound is the work of building it."})
	op(&code.Op{Name: OpNewErrorNode, NoReturn: true, Params: []code.Param{P("start", i), P("x", errT)}, Results: []code.Type{value}, Reads: pos, Writes: []string{State}, Cost: plus(value1, QErrorText),
		Contract: "Returns the error node of the recovered error x in the Node runtime, over the input from start to the position. Its message is a value that the parse builds, which counts nothing (spec/runtime/parsing.md#compute-units); the cost bound is the work of building it."})
	op(&code.Op{Name: OpFreshNode, Params: []code.Param{P("x", value)}, Results: []code.Type{value},
		Contract: "Returns x if it is a node its maker still owns, or nil.", Laws: []string{pure}})
	withImm(op(&code.Op{Name: OpConvertResult, Params: []code.Param{P("x", value), P("owned", value)}, Variadic: true, Results: []code.Type{value}, Writes: []string{State}, NoReturn: true,
		Contract: "Returns the value x of the rule converted to the rule's node type; with Owned, the only owned argument is the fresh node of the body, which converts in place."}),
		"the rule and whether the body's node is passed (Conversion)", immOf[prim.Conversion]())
	op(&code.Op{Name: OpUseCtx, Params: []code.Param{P("start", i)}, Results: []code.Type{ctx}, Reads: pos, Writes: []string{State},
		Contract: "Returns a new context for the action of the rule entered at start."})
	withImm(op(&code.Op{Name: OpPred, Params: []code.Param{P("caps", value)}, Variadic: true, Results: []code.Type{b}, Reads: all, Writes: []string{State}, NoReturn: true, Cost: per(QBody),
		Contract: "Evaluates the predicate with the captures caps (or the current frame) and reports whether it holds."}),
		"the action site of the predicate and its captures (Predicate)", immOf[prim.Predicate]())
	withImm(op(&code.Op{Name: OpBind, Params: []code.Param{P("caps", value)}, Variadic: true, Results: []code.Type{b}, Reads: all, Writes: []string{State}, NoReturn: true, Cost: per(QBody),
		Contract: "Binds the variable of the binding to the value of its expression, evaluated with the captures caps (or the current frame), and reports whether the evaluation succeeded."}),
		"the variable and the action site of the binding, and its captures (Binding)", immOf[prim.Binding]())
	withImm(op(&code.Op{Name: OpEval, Params: []code.Param{P("c", ctx), P("caps", value)}, Variadic: true, Results: []code.Type{value}, Reads: all, Writes: []string{State}, NoReturn: true, Cost: per(QBody),
		Contract: "Returns the value of the action of the function's rule, evaluated in the context c with the captures caps (or the current frame)."}),
		"the action site of the rule and its captures (Action)", immOf[prim.Action]())
	op(&code.Op{Name: OpCtxFinish, NoReturn: true, Params: []code.Param{P("c", ctx), P("x", value)}, Results: []code.Type{value}, Writes: []string{State}, Cost: per(QMade),
		Contract: "Returns the value x of an action finished in the context c: a struct that c made takes the range of the rule, and the values that c made are forgotten. It searches the values that c made for x, and charges one unit for each it compares with x."})
	return ops
}

// Attrs are the attributes of a function of grammar code at the Flow stage, which code.Func.Attrs
// holds.
type Attrs struct {
	// Rule is the ID of the rule whose body, or part of it, the function matches.
	Rule int
	// Vars are the locals that are variables of the function, in the order they were made; the
	// other locals are its entry variables Start, Result and Ctx (empty where it has none), which
	// the caller declares: the position at which the rule was entered, the rule's value, and the
	// context of the rule's action. Start is set before the body runs. A framed function has none.
	Vars               []string
	Start, Result, Ctx string
	// Fail is the label of the block that the body leaves on failure. Running to the end of the block
	// is success.
	Fail string
	// Cut is the local of a framed function's cut flag of its outermost scope, or empty. The function
	// reads it from the Cut register on entry and publishes it to the register again on both returns.
	Cut string
	// Binds reports that the body may bind variables, which the entry restores.
	Binds bool
	// Frame reports a framed function, which returns its value and true on success and the null value
	// and false on failure.
	Frame bool
	// Node reports a function that builds the values of the Node runtime (newTerminalNode,
	// newBranchNode). A function builds the values of one runtime: a Node function has no typed value
	// operation, and a typed function has no Node one (VerifyModule).
	Node bool
	// Document reports a function lowered for a build with Documents; VerifyModule then checks that
	// every in-place read has the recording operation as its alternative.
	Document bool
	// Traced reports a function of the traced copies that an observed parse runs. Every read of the
	// input in it records how far the parse examined, so VerifyModule rejects each operation that reads
	// in place (hasLit, matchClass, skipTerminal, skipClasses), with or without a recording alternative.
	Traced bool
	// Forms record the forms of the lowering that a Flow pass may replace, until flowpass.Finishing
	// removes them.
	Forms *Forms
}

// AttrsOf returns the Flow attributes of f, or nil.
func AttrsOf(f *code.Func) *Attrs {
	a, _ := f.Attrs.(*Attrs)
	return a
}

// FuncOf returns the function of m, a module of one lowered function.
func FuncOf(m *code.Module) *code.Func { return m.Funcs[0] }

// VarsOf returns the variables of the lowered function f (Attrs.Vars), in order, with their types.
func VarsOf(f *code.Func) []code.Param {
	a := AttrsOf(f)
	out := make([]code.Param, 0, len(a.Vars))
	for _, name := range a.Vars {
		for _, l := range f.Locals {
			if l.Name == name {
				out = append(out, l)
				break
			}
		}
	}
	return out
}
