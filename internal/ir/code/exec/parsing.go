package exec

import (
	"slices"

	"syntaxir.dev/sxir/internal/ir/code"
)

// State components with which rules parse the input and report on it.
const (
	// Exp is the expectation record.
	Exp = "expected"
	// Call is the call bookkeeping of the target outside the call protocol: the tracked path of Documents
	// and observation.
	Call = "call"
	// Obs is the observation of a parse.
	Obs = "observe"
	// Growth is the left-recursion growths in progress and the dependency memo of their contexts.
	Growth = "growth"
	// Win is the window of a stream parse: the input read and kept, the elements delivered and the
	// memory the delivered elements held.
	Win = "window"
)

// Names of the handle types of these components.
const (
	Body = "Body"
	Mark = "ExpMark"
	Set  = "ExpSet"
	Kept = "ExpKept"
	// ExpID identifies one expectation: a description the grammar lists, or a message of @error.
	ExpID = "ExpID"
	// Ctx is the context state of a memoized call (ctx.enter).
	Ctx = "Ctx"
	// Res is a result that a context keeps, a seed included, or the result of a growth.
	Res = "CtxResult"
	// Grow is the state of the growth of a leader (grow.begin).
	Grow = "Grow"
)

// Cost quantities of the operations of these components: measures of their arguments and of the state
// before the call.
const (
	// QSet is the number of expectations of the set that the call takes.
	QSet = "set"
	// QMerge is the work that the compositions over the expectation record (keep, mergeKept,
	// mergeExpected, and the calls of expect they make) do for the sets that the call keeps or
	// merges.
	QMerge = "merge"
	// QDeps is the number of dependencies that the call passes to the enclosing evaluation or stores:
	// those that the frame of the evaluation it ends recorded, and those stored with the results,
	// steps and jumps it reuses, with the observations they lead to.
	QDeps = "deps"
	// QTable is the work of the call's lookups and stores in the context tables at its position: one
	// for each lookup or store; one for each dependency set of the head it scans, each result under
	// the values the set selects and each key value those results compare, a string counting two
	// for each byte compared; and one for each jump it scans for a seed. A lookup that the outcome
	// of an observation leads to counts again on each path through the observations that outcomes
	// depend on.
	QTable = "table"
	// QPresent is the work of naming the present set of the call's context when a leader in it has no
	// bit in a word of bits: one for each growth of the context, and one more when no call named that
	// set since the first growth in progress began; 0 when every leader of the context has a bit.
	QPresent = "present"
	// QRun is, for each step of the run of the growth that the call records as jumps (the steps since
	// the last step that read another seed): one, one for each jump recorded before from its seed,
	// and the expectations of its set when its farthest position is that of the steps after it.
	QRun = "run"
	// QBody is the work of the evaluation of a rule's body that the call runs, and of the rule's
	// action, as the operations they call declare it; for an operation of the Operators Rule, that
	// of the trivia expression, or of the expression or the action of a line, that it runs.
	QBody = "body"
	// QSteps is the number of memoized steps and jumps that the call takes.
	QSteps = "steps"
	// QStreamScan is the number of input elements that the call copies when it compacts the buffer,
	// and of those it scans for line starts: every loaded element when the whole input is loaded from
	// its first element and no table of its lines exists yet, otherwise those before its argument
	// that no call scanned.
	QStreamScan = "stream_scan"
	// QStreamFrames is the number of capture frames that the scratch of the stream holds. A target may
	// visit each twice: a scan for the frame in use, which may lie outside the scratch, and a clear of
	// those after it.
	QStreamFrames = "stream_frames"
	// QStreamInactive is the capacity of the trail and the inactive capacity of a target's value
	// stack.
	QStreamInactive = "stream_inactive"
)

// Registers of the parser VM.
const (
	Pos    = "pos"
	Silent = "silent"
)

// addParsing adds to m, a module made by memoizedCall, the declarations with which rules parse the input
// and report on it: the registers, the expectation record, the invocation of rule bodies, the growth of
// left recursion, captures, the Operators Rule, Documents, observation, suspended calls and the
// components they write, with their operations. Each operation has a reference model and conformance
// cases. A rule body runs grammar code, which may change any state; the declarations name only the
// operation's own component and the registers. It also adds the host operations of the stream window (streamWindow).
func addParsing(m *code.Module) {
	m.AddRegister(Pos, code.TInt, "The position of the parse in the input.")
	m.AddRegister(Silent, code.TInt, "The depth of lookaheads, inside which expectations are not recorded.")
	m.AddComponent(Exp, `The expectation record: the expectations at the farthest position.`)
	m.AddComponent(Call, `The call bookkeeping of the target outside the call protocol: the tracked path of
Documents and observation.`)
	m.AddComponent(Obs, `The observation of a parse.`)
	m.AddComponent(Growth, `The left-recursion growths in progress with their seeds, the frames that record
what the calls in their contexts depend on, and the tables of the results kept in each context (the
dependency memo).`)

	rule := code.THandle(Rule)
	bodyH := m.AddHandle(Body, Rules, "The invocation of a rule's body that a generated call passes.")
	mark := m.AddHandle(Mark, Exp, "The record that an isolation set aside.")
	set := m.AddHandle(Set, Exp, "The expectations an isolation recorded, which a target may keep where the record grows next: valid as exp.setAt requires.")
	expID := m.AddHandle(ExpID, Exp, "An expectation: the identifier of a description or message of the grammar.")
	kept := m.AddHandle(Kept, Exp, "Expectations kept beyond the record, as a memo entry keeps them.")
	ctx := m.AddHandle(Ctx, Growth, `The context state of a memoized call: the growths of its rule's cycle at its
position, their present set, and whether the call records its dependencies.`)
	res := m.AddHandle(Res, Growth, `A result that a context keeps (a seed, or the result of a call or a growth step
that read a seed), or the last result of a growth.`)
	grow := m.AddHandle(Grow, Growth, "The state of the growth of a left-recursion leader.")
	value, errs, entry, values := code.THandle(Value), code.THandle(Errs), code.THandle(Entry), code.THandle(Values)
	result := []code.Type{value, code.TBool}
	regs := []string{Pos, Silent}
	ruleArg := []code.Param{code.P("r", rule)}
	call := []code.Param{code.P("r", rule), code.P("min", code.TInt)}

	body := code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QBody}}}
	// Making the rule's value from its captures costs nothing (spec/runtime/parsing.md#compute-units).
	invoke := code.Cost{Const: 1, Terms: []code.CostTerm{{Coef: 1, Quantity: QBody}}}
	m.AddOp(&code.Op{Name: "call.tracked", Component: Call, Results: []code.Type{code.TBool}, Reads: []string{Call}, Writes: []string{Call},
		Features: []string{Document},
		Contract: `Reports whether a rule call of the engine takes its traced path: in a traced parse, for a call
that the traced path has not yet entered. The call that call.track makes back is not traced a second
time: for it the operation reports false and clears the mark that says so.`})
	m.AddOp(&code.Op{Name: "call.track", Component: Call, Params: call, Results: result, Writes: append([]string{Call}, regs...),
		Features: []string{Document}, NoReturn: true, Cost: invoke,
		Contract: `Calls the rule at the binding level min on the engine's traced path and returns its result.`})

	for _, a := range []struct{ name, doc string }{
		{"rule.memo", "Reports whether the rule is memoized. A parse in the missing-token mode memoizes no rule that can insert a missing token."},
		{"rule.leader", "Reports whether the rule leads a left-recursive cycle."},
		{"rule.plain", "Reports whether the rule has no captures, so that its plain body serves its invocation."},
		{"rule.docMemo", "Reports whether a Document parse memoizes the rule."},
		{"rule.docOnlyMemo", "Reports whether a Document parse memoizes the rule and a parse from scratch does not; never for a rule that can insert a missing token in a parse in the missing-token mode."},
		{"rule.docPlain", "Reports whether a parse from scratch calls the plain body of the rule directly, at a call site, without the call protocol."},
		{"rule.positional", "Reports whether the results of the rule may depend on positions, not only on the input they examine."},
		{"rule.cyclic", "Reports whether the rule is on a cycle of the call graph, so that a parse that counts compute units checks them at the entry of each call of the rule."},
		{"rule.novalue", "Reports whether the rule makes no value: its calls return the null value."},
	} {
		m.AddOp(&code.Op{Name: a.name, Component: Rules, Params: ruleArg, Results: []code.Type{code.TBool}, Contract: a.doc})
	}
	m.AddOp(&code.Op{Name: "rule.id", Component: Rules, Params: ruleArg, Results: []code.Type{code.TInt},
		Contract: `Returns the rule's index, the rule of its memo keys.`})
	m.AddOp(&code.Op{Name: "rule.seen", Component: Rules, Params: ruleArg, Results: []code.Type{code.TInt},
		Contract: `Returns the rule's index among the rules whose memoization a parse may defer to their second
call at a position (seen.first), or -1 for a rule without one: a rule that a parse from scratch
does not memoize, and a leader of a left-recursive cycle. The rules with an index are numbered from 0
in the order of their indexes (rule.id).`})
	m.AddOp(&code.Op{Name: "rule.invoke", Component: Rules, Params: call, Results: result, Writes: append([]string{Rules}, regs...),
		NoReturn: true, Cost: invoke,
		Contract: `Runs the rule's body at the binding level min in a frame of its own and makes the rule's
value; on failure the position is unspecified. The evaluation counts in the statistics of a Document
parse, and that of a rule on a cycle of calls takes a nesting level while it runs. The unit of the call
is charged or counted before the nesting level is taken, so that a call that both the compute limit
and the nesting limit stop stops on the compute limit.`})
	m.AddOp(&code.Op{Name: "rule.invokePlain", Component: Rules, Params: call, Results: result, Writes: append([]string{Rules}, regs...),
		NoReturn: true, Cost: code.Cost{Const: 1, Terms: append(slices.Clone(body.Terms), code.CostTerm{Coef: 1, Quantity: QTokens})},
		Contract: `Runs the plain body of a rule without captures and returns its result, at the start
position and without the errors it recovered on failure; in the missing-token mode a failure also
removes the missing tokens that the body inserted, as missing.reset does. The evaluation counts, and
the unit of the call comes before the nesting level, as in rule.invoke.`,
		Requires: `rule.plain is true of r.`})
	m.AddOp(&code.Op{Name: "rule.body", Component: Rules, Params: call, Results: result, Writes: append([]string{Rules}, regs...),
		NoReturn: true, Cost: body,
		Contract: `Runs the rule's body at the binding level min in the current frame and returns the value of the
body, from which rule.finish makes the rule's value; on failure the position is unspecified. It
counts no unit of the call and takes no nesting level: the invocation around it does, as rule.invoke
does.`})
	m.AddOp(&code.Op{Name: "rule.finish", Component: Rules, Params: []code.Param{code.P("r", rule), code.P("f", code.THandle(Frame)),
		code.P("v", value), code.P("start", code.TInt)}, Results: []code.Type{value}, Reads: []string{Frames, Pos}, Writes: []string{Rules},
		NoReturn: true, Cost: body,
		Contract: `Returns the rule's value made from the value v of its body, which matched from start to the
position with the captures of f: the value of its action, or v shaped as the rule's value, converted to
the rule's node type if it has one. Making the value charges what its action charges.`,
		Requires: `v is the value of a successful evaluation of the body of r, whose captures f holds.`})
	m.AddOp(&code.Op{Name: "rule.run", Component: Rules, Params: []code.Param{code.P("inv", bodyH)}, Results: result,
		Writes: append([]string{Rules}, regs...), NoReturn: true, Cost: body,
		Contract: `Runs the invocation inv of a rule at binding level 0 and returns its result; on failure the
position is unspecified. The evaluation does not count in the statistics of a Document parse, which
the caller counts with doc.evaluated.`})

	m.AddOp(&code.Op{Name: "exp.isolate", Component: Exp, Params: []code.Param{code.P("pos", code.TInt)}, Results: []code.Type{mark},
		Reads: []string{Exp}, Writes: []string{Exp},
		Contract: `Sets the record aside and starts an empty one at pos.`})
	m.AddOp(&code.Op{Name: "exp.unisolate", Component: Exp, Params: []code.Param{code.P("m", mark)}, Results: []code.Type{code.TInt, set},
		Reads: []string{Exp}, Writes: []string{Exp}, Requires: `m is the innermost isolation that has not ended.`,
		Contract: `Ends the record that the isolation m started and restores the one set aside; returns the
farthest position of the ended record and its expectations.`})

	// The operations of recording one expectation.
	expArg := []code.Param{code.P("id", expID)}
	m.AddOp(&code.Op{Name: "exp.farthest", Component: Exp, Results: []code.Type{code.TInt}, Reads: []string{Exp},
		Contract: `Returns the farthest position of the current record.`})
	m.AddOp(&code.Op{Name: "exp.recording", Component: Exp, Results: []code.Type{code.TBool}, Reads: []string{Exp},
		Contract: `Reports whether the parse records expectations. A parse that does not keeps one placeholder
entry in the record of a failure, which stands for the failure at the farthest position and is not an
expectation.`})
	m.AddOp(&code.Op{Name: "exp.reset", Component: Exp, Params: []code.Param{code.P("pos", code.TInt)}, Writes: []string{Exp},
		Contract: `Starts the current record anew at pos: the farthest position becomes pos and the entries are
dropped.`})
	m.AddOp(&code.Op{Name: "exp.fresh", Component: Exp, Results: []code.Type{code.TBool}, Reads: []string{Exp},
		Contract: `Reports whether the current record has no entry.`})
	m.AddOp(&code.Op{Name: "exp.placeholder", Component: Exp, Reads: []string{Exp}, Writes: []string{Exp},
		Contract: `Adds the placeholder entry to the current record.`})
	m.AddOp(&code.Op{Name: "exp.maybe", Component: Exp, Params: expArg, Results: []code.Type{code.TBool}, Reads: []string{Exp},
		Requires: `id is an expectation, not the placeholder.`,
		Contract: `Reports whether id may be in the current record: false means it is not; true means it may be.`})
	m.AddOp(&code.Op{Name: "exp.count", Component: Exp, Results: []code.Type{code.TInt}, Reads: []string{Exp},
		Contract: `Returns the number of entries of the current record.`})
	m.AddOp(&code.Op{Name: "exp.at", Component: Exp, Params: []code.Param{code.P("i", code.TInt)}, Results: []code.Type{expID},
		Reads: []string{Exp}, Requires: `0 <= i < exp.count.`,
		Contract: `Returns entry i of the current record, in the order they were added.`})
	m.AddOp(&code.Op{Name: "exp.add", Component: Exp, Params: expArg, Reads: []string{Exp}, Writes: []string{Exp},
		Contract: `Adds id to the current record, after its other entries.`})
	m.AddOp(&code.Op{Name: "exp.setCount", Component: Exp, Params: []code.Param{code.P("s", set)}, Results: []code.Type{code.TInt},
		Contract: `Returns the number of expectations of s.`})
	m.AddOp(&code.Op{Name: "exp.setAt", Component: Exp, Params: []code.Param{code.P("s", set), code.P("i", code.TInt)},
		Results: []code.Type{expID}, Reads: []string{Exp}, Requires: `0 <= i < exp.setCount(s); s is the set that exp.unisolate returned last, no
isolation began or ended since, and every entry written to the record since then was written at an
index below n + i, where n is the number of entries of the record when exp.unisolate returned s.
Merging s into the record entry by entry, reading each entry before the write that follows it, meets
this.`,
		Contract: `Returns expectation i of s, in the order they were recorded.`})
	m.AddOp(&code.Op{Name: "exp.keptCount", Component: Exp, Params: []code.Param{code.P("k", kept)}, Results: []code.Type{code.TInt},
		Contract: `Returns the number of expectations of k.`})
	m.AddOp(&code.Op{Name: "exp.keptAt", Component: Exp, Params: []code.Param{code.P("k", kept), code.P("i", code.TInt)},
		Results: []code.Type{expID}, Requires: `0 <= i < exp.keptCount(k).`,
		Contract: `Returns expectation i of k, in the order they were recorded.`})

	// The operations of keeping a set beyond the record.
	m.AddOp(&code.Op{Name: "exp.empty", Component: Exp, Results: []code.Type{kept},
		Contract: `Returns the kept set with no expectations.`})
	m.AddOp(&code.Op{Name: "exp.last", Component: Exp, Results: []code.Type{kept}, Reads: []string{Exp},
		Contract: `Returns the set that exp.copy made last in the parse, and the empty set before the first.`})
	m.AddOp(&code.Op{Name: "exp.copy", Component: Exp, Params: []code.Param{code.P("s", set)}, Results: []code.Type{kept},
		Reads: []string{Exp}, Writes: []string{Exp}, NoReturn: true, Cost: code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QSet}}},
		Requires: `s has at least one expectation, and exp.setAt may read each of them.`,
		Contract: `Copies the expectations of s, in order, into a set that outlives the record, makes it the set
that exp.copy made last and returns it. The copy costs what the target's copies of values cost, and
the memory it takes is accounted as the target accounts it.`})

	entryArg := []code.Param{code.P("e", entry)}
	m.AddOp(&code.Op{Name: "memo.far", Component: Memo, Params: entryArg, Results: []code.Type{code.TInt}, Reads: []string{Memo},
		Requires: `memo.setExpected recorded expectations with e.`,
		Contract: `Returns the farthest position of the expectations of entry e.`})
	m.AddOp(&code.Op{Name: "memo.expected", Component: Memo, Params: entryArg, Results: []code.Type{kept}, Reads: []string{Memo},
		Requires: `memo.setExpected recorded expectations with e.`,
		Contract: `Returns the expectations of entry e.`})
	m.AddOp(&code.Op{Name: "memo.setExpected", Component: Memo, Params: append(entryArg, code.P("far", code.TInt), code.P("k", kept)),
		Writes: []string{Memo},
		Contract: `Records the expectations k at far of the call that computed entry e. When memo.splice moves e,
far moves with it, by the same amount as its end.`})
	ctxArg := code.P("c", ctx)
	resArg := []code.Param{code.P("s", res)}
	m.AddOp(&code.Op{Name: "ctx.enter", Component: Growth, Params: call, Results: []code.Type{ctx, res},
		Reads: []string{Growth, Rules}, Writes: []string{Growth}, NoReturn: true, Cost: code.Cost{Const: 1, Terms: []code.CostTerm{{Coef: 1, Quantity: QPresent}}},
		Contract: `Begins a memoized call of the rule at the position: returns its context state and, if the
rule is a leader that grows at the position, its current seed, which the call reads (with the
dependency on its identity); otherwise the null result. The context of a call of a rule of a
left-recursive cycle is the growths of its cycle in progress at the position; another call has
none.`})
	m.AddOp(&code.Op{Name: "ctx.found", Component: Growth, Params: resArg, Results: []code.Type{code.TBool},
		Contract: `Reports whether s is a result, not the null result.`})
	m.AddOp(&code.Op{Name: "ctx.ok", Component: Growth, Params: resArg, Results: []code.Type{code.TBool}, Reads: []string{Growth},
		Contract: `Returns whether the result s succeeded.`})
	m.AddOp(&code.Op{Name: "ctx.value", Component: Growth, Params: resArg, Results: []code.Type{value}, Reads: []string{Growth},
		Contract: `Returns the value of the result s.`})
	m.AddOp(&code.Op{Name: "ctx.end", Component: Growth, Params: resArg, Results: []code.Type{code.TInt}, Reads: []string{Growth},
		Contract: `Returns the end of the match of the result s; for a failure, the position of its call.`})
	m.AddOp(&code.Op{Name: "ctx.errs", Component: Growth, Params: resArg, Results: []code.Type{errs}, Reads: []string{Growth},
		Contract: `Returns the recovered errors of the result s.`})
	m.AddOp(&code.Op{Name: "ctx.far", Component: Growth, Params: resArg, Results: []code.Type{code.TInt}, Reads: []string{Growth},
		Contract: `Returns the farthest position of the expectations kept with the result s.`})
	m.AddOp(&code.Op{Name: "ctx.expected", Component: Growth, Params: resArg, Results: []code.Type{kept}, Reads: []string{Growth},
		Contract: `Returns the expectations kept with the result s.`})
	m.AddOp(&code.Op{Name: "ctx.examined", Component: Growth, Params: resArg, Results: []code.Type{code.TInt},
		Reads: []string{Growth}, Features: []string{Document},
		Contract: `Returns the examined end that the parse recorded with the result s, or 0 if it recorded none.`})
	m.AddOp(&code.Op{Name: "ctx.before", Component: Growth, Params: append(resArg, code.P("pos", code.TInt)),
		Results: []code.Type{code.TBool}, Reads: []string{Growth}, Features: []string{Document},
		Contract: `Reports whether the input that the result s, kept for a call at pos, examined starts before pos.`})
	m.AddOp(&code.Op{Name: "ctx.allows", Component: Growth, Params: append([]code.Param{ctxArg}, entryArg...),
		Results: []code.Type{code.TBool}, Reads: []string{Growth, Memo},
		Contract: `Reports whether the call with the context state c may reuse the memo entry e: always outside a
context, and in one only when the leaders growing there are among those that grew where e was stored
(its present set). An entry holds a present set of one word of bits, so it answers no call in a context
with a leader that has no bit there.`})
	m.AddOp(&code.Op{Name: "ctx.active", Component: Growth, Params: []code.Param{ctxArg}, Results: []code.Type{code.TBool},
		Contract: `Reports whether the call with the context state c is in a context.`})
	m.AddOp(&code.Op{Name: "ctx.lookup", Component: Growth, Params: []code.Param{ctxArg, code.P("r", rule), code.P("min", code.TInt),
		code.P("key", values)}, Results: []code.Type{res}, Reads: []string{Growth}, Writes: []string{Growth},
		NoReturn: true, Cost: code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QTable}, {Coef: 1, Quantity: QDeps}}},
		Contract: `Returns the result kept in the context of the call for the rule at the position, the level
and the key that holds there: the seeds it read have the same identities, and the lookaheads it
observed the same outcomes, in the current context; it records those dependencies for the enclosing
call. Otherwise the null result.`,
		Requires: `c is in a context.`})
	m.AddOp(&code.Op{Name: "ctx.push", Component: Growth, Params: []code.Param{ctxArg}, Writes: []string{Growth},
		Contract: `Starts recording the dependencies of the evaluation of the call, if it is in a context.`})
	m.AddOp(&code.Op{Name: "ctx.pop", NoReturn: true, Component: Growth, Params: []code.Param{ctxArg}, Results: []code.Type{code.TInt},
		Reads: []string{Growth}, Writes: []string{Growth}, Cost: code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QDeps}}},
		Contract: `Ends recording the dependencies of the evaluation and returns where its result goes:
ctx.storeMemo for the memo (it read no seed, made no observation and its context's leaders all have
a bit in a word of bits), ctx.storeCtx for the table of the context, or another value for nowhere
(its context records no dependencies).`})
	for _, a := range []struct{ name, doc string }{
		{"ctx.storeMemo", "Returns the value of ctx.pop for a result stored in the memo."},
		{"ctx.storeCtx", "Returns the value of ctx.pop for a result kept in the table of its context."},
	} {
		m.AddOp(&code.Op{Name: a.name, Component: Growth, Results: []code.Type{code.TInt}, Contract: a.doc})
	}
	m.AddOp(&code.Op{Name: "memo.setPresent", Component: Memo, Params: append(entryArg, ctxArg), Reads: []string{Growth},
		Writes: []string{Memo},
		Contract: `Adds to the present set of the entry e that of the context state c, for ctx.allows: the
entry answers the calls in contexts whose leaders are all in the union, since a result stored in the
memo read no growing result and is the same in each of them.`,
		Requires: `The present set of c fits in a word of bits, which a call whose result ctx.pop sends to the memo meets.`})
	m.AddOp(&code.Op{Name: "ctx.keep", Component: Growth, Params: []code.Param{ctxArg, code.P("r", rule), code.P("pos", code.TInt),
		code.P("min", code.TInt), code.P("key", values), code.P("ok", code.TBool), code.P("end", code.TInt), code.P("silent", code.TBool),
		code.P("value", value), code.P("errs", errs), code.P("far", code.TInt), code.P("k", kept)},
		Reads: []string{Growth, Call}, Writes: []string{Growth}, NoReturn: true, Cost: code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QTable}, {Coef: 1, Quantity: QDeps}}},
		Contract: `Keeps the result of the call in the table of its context with the dependencies that ctx.pop
ended, and the examined end of a Document parse.`,
		Requires: `The last ctx.pop of c returned ctx.storeCtx. If r leads a cycle, its growth at pos has ended (grow.end) and the result passed is the one grow.end returned: the result of a leader is final only then.`})
	m.AddOp(&code.Op{Name: "ctx.done", Component: Growth, Params: []code.Param{ctxArg}, Writes: []string{Growth},
		NoReturn: true, Cost: code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QDeps}}},
		Contract: `Passes the dependencies that ctx.pop ended to the evaluation that encloses the call.`})

	m.AddOp(&code.Op{Name: "grow.begin", Component: Growth, Params: call, Results: []code.Type{grow},
		Reads: []string{Growth, Rules, Pos}, Writes: []string{Growth}, NoReturn: true, Cost: code.Cost{Const: 1, Terms: []code.CostTerm{{Coef: 1, Quantity: QPresent}}},
		Contract: `Starts the growth of the leader at the binding level min at the position from a failure,
at a cost of one step, and present for naming the present set of its context when it is in one: the growth
becomes the innermost, with a failed seed that ends at the position. Every failed seed has the same
identity, and every seed that grow.adopt or grow.adoptStep adopts has a new one. The growth is in a context
when other leaders of its cycle grow at the position.`,
		Requires: `r is a leader that does not grow at the position among the growths of its cycle there, and
no growth of another cycle at the position is the innermost.`})
	m.AddOp(&code.Op{Name: "grow.ctx", Component: Growth, Params: []code.Param{code.P("g", grow)}, Results: []code.Type{code.TBool},
		Reads: []string{Growth},
		Contract: `Reports whether the growth g memoizes its steps in its context (grow.memoStep and grow.keepStep): when
other leaders of its cycle grow at its position, unless the parse keeps no results in contexts. A
growth that does not takes its steps with grow.longer and grow.adopt.`,
		Requires: `g is the innermost growth.`})
	m.AddOp(&code.Op{Name: "grow.longer", Component: Growth, Params: []code.Param{code.P("g", grow), code.P("ok", code.TBool)},
		Results: []code.Type{code.TBool}, Reads: []string{Growth, Pos},
		Contract: `Reports whether a step of the growth g with success ok, ending at the position, matched more
than the seed: it succeeded, and the seed failed or ends before the position.`,
		Requires: `g is the innermost growth, and grow.ctx(g) is false.`})
	m.AddOp(&code.Op{Name: "grow.adopt", Component: Growth, Params: []code.Param{code.P("g", grow), code.P("value", value),
		code.P("errs", errs)}, Reads: []string{Growth, Rec}, Writes: []string{Growth, Pos}, NoReturn: true, Cost: code.Cost{Const: 1},
		Contract: `Adopts the successful step of the growth g that ends at the position, with its value and the
errors errs that it recovered, as the seed of g with a new identity, at a cost of one step. In the missing-token mode the seed also holds
the tokens that the step inserted. The seed it replaces is no longer a valid result. The position then
returns to the start of the growth.`,
		Requires: `g is the innermost growth, grow.ctx(g) is false and grow.longer(g, true) holds.`})
	growArg := code.P("g", grow)
	m.AddOp(&code.Op{Name: "grow.memoStep", Component: Growth, Params: []code.Param{growArg}, Results: []code.Type{res},
		Reads: []string{Growth, Silent}, Writes: []string{Growth, Pos}, NoReturn: true,
		Cost: code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QSteps}, {Coef: 1, Quantity: QTable}, {Coef: 1, Quantity: QDeps}, {Coef: 1, Quantity: QMerge}, {Coef: 1, Quantity: QRun}}},
		Contract: `Takes the steps of the growth g that the table of its context gives, following the jumps it
records: while the table keeps a step from the current seed that holds in the current context, as
ctx.lookup finds a result, and kept outside a lookahead unless the parse is in one, it passes the
dependencies of the step to the enclosing evaluation, merges its expectations into the record and
adopts every step that matched more than the seed, as grow.adoptStep does. It returns the first step
that did not match more, which ends the growth, or the null result when the table keeps no step from
the seed, whose evaluation grow.isolate then begins. A jump takes several of the steps at once with
the same effects as taking them one at a time, so only the seed the steps reach and the step that
ends them are observable. A step taken from the table counts no compute unit: only a step that evaluates
the body does.`,
		Requires: `grow.ctx(g) holds, g is the innermost growth, no step of g is being evaluated, and no step that
grow.keepStep returned is pending: each was adopted by grow.adoptStep or did not grow.`})
	m.AddOp(&code.Op{Name: "grow.isolate", Component: Growth, Params: []code.Param{growArg},
		Reads: []string{Growth}, Writes: []string{Growth},
		Contract: `Starts evaluating a step of the growth g: it records the dependencies of the step, as ctx.push
does, and keeps its examined range and its expectations apart from those of the parse until
grow.keepStep.`,
		Requires: `As for grow.memoStep, and the last call of grow.memoStep(g) returned the null result.`})
	m.AddOp(&code.Op{Name: "grow.keepStep", Component: Growth, Params: []code.Param{growArg, code.P("value", value),
		code.P("ok", code.TBool), code.P("errs", errs)}, Results: []code.Type{res}, Reads: []string{Growth, Pos, Silent},
		Writes: []string{Growth}, NoReturn: true,
		Cost: code.Cost{Const: 65, Terms: []code.CostTerm{{Coef: 1, Quantity: QMerge}, {Coef: 1, Quantity: QSegs}, {Coef: 2, Quantity: QDeps}, {Coef: 1, Quantity: QTable}}},
		Contract: `Ends the evaluation of the step of the growth g that grow.isolate started, with its value, its
success ok and the errors errs that it recovered, and returns its result, which ends at the position or,
for a failure, at the start of the growth: it keeps the result in the table of the context with the
dependencies of the step, passes them to the enclosing evaluation, and merges the expectations of the
step into the record. A result equal to an earlier step's of the growth's leader at the position has that
step's identity as a seed, and otherwise a new one. It does not change the recovered errors: the caller
lists the errors of the step with rec.since and drops the entries after the mark of the growth with
rec.truncate.`,
		Requires: `grow.isolate(g) started the evaluation of a step that has not ended, and the calls that the body
made have ended. errs is the list that rec.since returns for the mark of the growth, after the entries of
a step that failed were dropped.`})
	m.AddOp(&code.Op{Name: "grow.grew", Component: Growth, Params: []code.Param{growArg, code.P("s", res)},
		Results: []code.Type{code.TBool}, Reads: []string{Growth},
		Contract: `Reports whether the step s of the growth g matched more than its seed: s succeeded, and the seed
failed or ends before s.`,
		Requires: `s is the step that grow.keepStep returned last for g.`})
	m.AddOp(&code.Op{Name: "grow.adoptStep", Component: Growth, Params: []code.Param{growArg, code.P("s", res)},
		Reads: []string{Growth}, Writes: []string{Growth, Pos}, NoReturn: true,
		Cost: code.Cost{Const: 1, Terms: []code.CostTerm{{Coef: 1, Quantity: QRun}}},
		Contract: `Adopts the step s of the growth g as its seed, with the identity of s, at a cost of one step, and
returns the position to the start of the growth.`,
		Requires: `grow.grew(g, s) holds, and s is the step that grow.keepStep returned last for g.`})
	m.AddOp(&code.Op{Name: "grow.end", Component: Growth, Params: []code.Param{code.P("g", grow)}, Results: []code.Type{res},
		Reads: []string{Growth}, Writes: append([]string{Growth}, regs...), NoReturn: true, Cost: code.Cost{Terms: []code.CostTerm{{Coef: 1, Quantity: QRun}, {Coef: 1, Quantity: QErrs}}},
		Contract: `Ends the growth g and returns its last seed: the position moves to its end. It does not change
the recovered errors: the caller drops the entries recorded since the growth began with rec.truncate and
records the errors of the seed, as rec.add does or, in the missing-token mode, as missing.replayResult
does. The result is the last seed of the growth, in a context or not: the one that grow.adopt or
grow.adoptStep adopted last, or the failed seed if none was; it may be that same result or a copy of it, which is not
specified, as no operation changes a result.`,
		Requires: `g is the innermost growth, and no step of g is being evaluated.`})

	m.AddOp(&code.Op{Name: "obs.memoHit", Component: Obs, Writes: []string{Obs}, Features: []string{Observe},
		Contract: `Counts a reuse of a memo entry in the observation, if the parse is observed.`})
	m.AddOp(&code.Op{Name: "obs.active", Component: Obs, Results: []code.Type{code.TBool}, Reads: []string{Obs}, Features: []string{Observe},
		Contract: `Reports whether the parse is observed.`})
	m.AddOp(&code.Op{Name: "obs.enter", Component: Obs, Params: []code.Param{code.P("r", rule), code.P("pos", code.TInt), code.P("min", code.TInt)},
		Writes: []string{Obs}, Features: []string{Observe}, NoReturn: true,
		Contract: `Reports to the observation of the parse that a call of the rule at pos and binding level
min begins.`,
		Requires: `The parse is observed.`})
	m.AddOp(&code.Op{Name: "obs.exit", Component: Obs, Params: []code.Param{code.P("ok", code.TBool), code.P("pos", code.TInt),
		code.P("examined", code.TInt)}, Writes: []string{Obs}, Features: []string{Observe}, NoReturn: true,
		Contract: `Reports to the observation of the parse that the call that the last unmatched obs.enter began
ends, with its success ok, at pos, and with the end of the input it examined.`,
		Requires: `The parse is observed, and a call that obs.enter began has not ended.`})
	m.AddOp(&code.Op{Name: "doc.examinedBegin", Component: Call, Results: []code.Type{code.TInt}, Reads: []string{Call, Pos},
		Writes: []string{Call}, Features: []string{Document},
		Contract: `Starts measuring how far an observed call examines the input: the examined end of the call
starts at the position. Returns the enclosing call's examined end, for doc.examinedEnd. The call
itself, and any memo entry it reads or records, extends the end as in an unobserved parse, so that
observation does not change reuse.`,
		Requires: `The parse tracks the examined input (doc.tracking).`})
	m.AddOp(&code.Op{Name: "doc.examinedEnd", Component: Call, Params: []code.Param{code.P("enclosing", code.TInt)},
		Results: []code.Type{code.TInt}, Reads: []string{Call}, Writes: []string{Call}, Features: []string{Document},
		Contract: `Ends measuring the examined end of an observed call and merges it into the enclosing one; returns
the call's examined end: one past the last unit that the call's result depends on.`,
		Requires: `enclosing is the result of the matching doc.examinedBegin.`})
	m.AddOp(&code.Op{Name: "doc.enter", Component: Call, Params: []code.Param{code.P("pos", code.TInt)},
		Results: []code.Type{code.TInt, code.TInt}, Reads: []string{Call}, Writes: []string{Call}, Features: []string{Document},
		Contract: `Begins a rule call at pos in a Document parse: returns the examined input of the enclosing call,
its end and its start, and starts the call's examined input as the empty range at pos: a call has
examined nothing when it begins, and each read extends the range by exactly what it examined.`,
		Requires: `The parse reads a Document.`})
	m.AddOp(&code.Op{Name: "doc.leave", Component: Call, Params: []code.Param{code.P("examined", code.TInt), code.P("low", code.TInt)},
		Reads: []string{Call}, Writes: []string{Call}, Features: []string{Document},
		Contract: `Ends a rule call in a Document parse: the examined input of the enclosing call becomes the smallest
range that holds [low, examined) and the call's.`,
		Requires: `The parse reads a Document; examined and low are the results of the matching doc.enter.`})
	doc := []string{Document}
	inDoc := `The parse reads a Document.`
	m.AddOp(&code.Op{Name: "doc.active", Component: Call, Results: []code.Type{code.TBool}, Reads: []string{Call}, Features: doc,
		Contract: `Reports whether the parse reads a Document.`})
	m.AddOp(&code.Op{Name: "doc.tracking", Component: Call, Results: []code.Type{code.TBool}, Reads: []string{Call}, Features: doc,
		Contract: `Reports whether the parse tracks how far each call examines the input: a Document parse, which
reuses results by it, and an observed parse, which reports it. Every input unit that can influence a
call's result then lies below the call's examined end.`})
	m.AddOp(&code.Op{Name: "doc.touch", Component: Call, Params: []code.Param{code.P("end", code.TInt)}, Reads: []string{Call},
		Writes: []string{Call}, Features: doc,
		Contract: `Extends the examined end of the current call to end, if end is past it: end is exactly one past the
last unit that what the call read depends on. A parse that does not track the examined input
(doc.tracking) keeps none.`})
	m.AddOp(&code.Op{Name: "doc.touchFrom", Component: Call, Params: []code.Param{code.P("from", code.TInt)}, Reads: []string{Call},
		Writes: []string{Call}, Features: doc,
		Contract: `Extends the examined input of the current call back to from, if from is before its start. A parse
that reads no Document keeps none.`})
	m.AddOp(&code.Op{Name: "doc.low", Component: Call, Results: []code.Type{code.TInt}, Reads: []string{Call},
		Features: doc, Contract: `Returns the start of the input that the current call examined so far.`, Requires: inDoc})
	m.AddOp(&code.Op{Name: "doc.examined", Component: Call, Results: []code.Type{code.TInt}, Reads: []string{Call},
		Features: doc, Contract: `Returns the examined end of the current call: the exact end of the input it examined
so far, exclusive.`, Requires: `The parse tracks the examined input (doc.tracking).`})
	m.AddOp(&code.Op{Name: "doc.reused", Component: Call, Writes: []string{Call}, Features: doc,
		Contract: `Counts a reused memo entry in the statistics of the Document.`, Requires: inDoc})
	m.AddOp(&code.Op{Name: "doc.evaluated", Component: Call, Writes: []string{Call}, Features: doc,
		Contract: `Counts an evaluated rule body in the statistics of the Document.`, Requires: inDoc})
	// The host operations of the stream window.
	streamWindow(m, value)
	documentMove(m)
	documentEdits(m)
	documentRuns(m, value)
	splitCall(m, rule, values, ctx, mark, entry)
	framesAndOperators(m, rule, value, errs)
}
