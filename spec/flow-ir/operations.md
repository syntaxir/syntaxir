# Operations

A function at the Flow stage is Code IR: its structure (blocks, loops, branches, exits, breaks, continues and
assignments) and the arithmetic and logic of its conditions are the base's ([Code IR](../code-ir/README.md)), and
every other step is a call of one of the operations below, which act on the state of a parse. An operation names its
dynamic operands, which are arguments of the call, and its results; its static data (a literal, a class, a rule, a
site) is the immediate of the call, of the kind the Immediate column names. A variadic operation repeats its last
operand zero or more times. The calls that read or write the position and the cut flag use the registers `$pos` and
`$cut` instead; the environment and the current frame are reached through `flow.env`, `flow.setEnv`, `flow.frame` and
`flow.setFrame`.

The contract of each operation is that of its declaration in the Flow dialect, which the verifier, the conversion to
the Exec stage and the backends rely on.

A function lowered with logical units counts [compute units](../runtime/parsing.md#compute-units): each operation counts
the logical events that it makes, such as the input a terminal advances over, a call with its key, a syntax error built
or a suffix copy, and nothing else.

| Operation | Operands | Results | Immediate | Contract |
|:--|:--|:--|:--|:--|
| `flow.step` |  |  |  | The step of an iteration. It counts nothing, since the step of an iteration is no event of the [compute units](../runtime/parsing.md#compute-units), and checks their counter, which a target whose counter checks itself at each event may leave out. It is the step of the loop whose body holds it, once per iteration, and an iteration that goes on to the next step has advanced the position, so each step of a loop finds the position past the one of the loop's last step; a target that runs code it did not make itself, as the bytecode machines do, requires it and ends the parse with an error of invalid code otherwise ([Flow Bodies](../bytecode/README.md#flow-bodies)). |
| `flow.silent` |  |  | `silence` | Adds Delta to the silent depth, inside which expectations are not recorded. For a lookahead (Site is not 0) it begins the lookahead of the site after it enters the depth, or ends it, whose body matched if Ok, before it leaves. The end of a lookahead in a context of left recursion whose body read a seed stores its outcome with its dependencies in the context's table, and charges for them as ctx.keep does (spec/exec-ir/operations.md). |
| `flow.peek` |  | rune, int, bool |  | Returns the code point at the position, its size and whether there is one, without consuming it. In a Document parse the read records how far the parse examined the input. It counts no [compute unit](../runtime/parsing.md#compute-units): examining input without advancing is no event. |
| `flow.advance` | n int |  |  | Moves the position forward by n. In a function lowered with logical units it counts n [compute units](../runtime/parsing.md#compute-units), the input that a terminal advances over. |
| `flow.expect` | at int |  | `expectation` | Records the expectation at the position at, unless the silent depth is positive. |
| `flow.observeSkip` |  |  | `observedskip` | Reports to the observation of the parse that the guard of a choice skips the alternative, which begins with the rule call that the immediate names. |
| `flow.skippedCalls` |  |  | `skippedcalls` | Makes the effects of the calls that the alternative a guard skips would make under logical compute units, as the evaluation of the alternative makes them: one unit for each call and the units of the key of each memoized rule, the seen mark and policy counts of each memoized call, and the failure entry that a call stores when its memoization is not deferred, with the expectation of the terminal. A call that a stored entry answers ends the calls, as it ends the evaluation. It checks the counter. Only a lowering with logical units makes the call. |
| `flow.inDocument` |  | bool |  | Reports that the parse is a Document's. A guard whose skip passes over a call of a rule that a Document memoizes skips only outside Documents: a Document parse evaluates the alternative, where the memo records how far each entry examined the input and what later parses reuse ([Reuse](../runtime/documents.md#reuse)). It counts no event and charges nothing. |
| `flow.restore` | pos int, rec int |  |  | Moves the position back to pos and drops the recovered errors after the first rec. |
| `flow.mergeExpected` | far int, exps expids |  |  | Merges the expectations exps, recorded at far, into the enclosing expectation scope. |
| `flow.mergeRenamed` | far int, exps expids |  | `expid` | Merges the expectations exps, recorded at far, into the enclosing scope, renamed to the expectation of the immediate (@diagnostic). |
| `flow.pushKid` | x value |  |  | Pushes x on the stack of gathered values. |
| `flow.dropKids` | base int |  |  | Truncates the stack of gathered values to base. |
| `flow.streamStart` |  |  |  | Begins the stream of a stream parse: the repetition that delivers its elements is about to run. |
| `flow.skipBegin` |  |  | `site` | Begins a walk of the skip-run site at the position: the iterations that follow are its visits, until flow.skipEnd. |
| `flow.runBegin` |  |  | `runwalk` | Begins a walk of the hot run site at the position, as flow.skipBegin does for a skip-run site, until flow.skipEnd or flow.skipKids. With Build its iterations have values, which flow.skipValue adds and flow.skipKids gathers in a list; with KeyEnv its records are kept for the position and the values of the variables of the site's key, otherwise for the position alone. |
| `flow.skipVisit` |  |  |  | Ends the iteration of the walk before, if any, and begins one at the position; in a recording walk it keeps the expectations of the iteration before and reads the key of the walk. |
| `flow.skipValue` | x value |  |  | Adds the value x of the iteration that matched to the walk begun with Build; the records of the walk share it. |
| `flow.skipEnd` |  |  |  | Ends the iteration of the walk before, if any, ends the walk and records the rest of it from each position it visited while recording. A walk begun with Build drops the values it holds, which charges nothing, and copies the elements of the record it took where it cannot share them, a unit for each ([Run Sites](../runtime/parsing.md#run-sites)). |
| `flow.runWalked` | start int |  | `site` | Ends the walk of the run site that was not hot, which began at start: a walk that began below the furthest position that the walks of the site passed, and walked RunHotSpan or more of the input below it, turns the site hot. A walk shorter than RunHotSpan changes nothing. Noting the end of a walk at a site that is not hot is free ([Run Sites](../runtime/parsing.md#run-sites)). |
| `flow.scanEnd` | start int, count int |  | `site` | Ends a walk of the value-free scan site that began at start and matched count characters, at least ScanHotSpan: unless the walk took the rest from a record, it records itself at a hot site, which charges a unit for the record, or notes that it walked at a site that is not hot, which is free ([Scan Sites](../runtime/parsing.md#scan-sites)). |
| `flow.emitStream` | x value |  |  | Delivers the committed element x (nil for a value-free repetition) to the callback of a stream parse, which has consumed the input up to the position. |
| `flow.append` | vals vals, x value | vals | `spread` | Returns the list vals with x appended; with Spread, the children of the tuple node x. |
| `flow.setWhere` |  |  | `chars` | Names the rule whose action runs, for the messages of its errors. |
| `flow.dropCreated` | c ctx |  |  | Drops the structs made since the context c began. |
| `flow.unfresh` | x value |  |  | Marks the node x, if x is one, as no longer owned by its maker. |
| `flow.reset` | m mark |  |  | Returns to the parser state of the mark m: the position and the parts that flow.markState named. Undoing the captures written after m charges nothing, as the composition reset does ([Compositions](../exec-ir/compositions.md)). |
| `flow.setCapture` | x value |  | `slot` | Sets the capture slot of the current frame to x. |
| `flow.clearVals` | f frame |  |  | Sets the values of the frame f to nil. |
| `flow.dropTrail` | m mark |  |  | Drops the trail entries recorded since the mark m. |
| `flow.freeFrame` | f frame |  |  | Returns the frame f to the runtime. |
| `flow.appendError` | x error |  |  | Appends the syntax error x to the recovered errors. |
| `flow.declareNames` |  |  | `names` | Declares the slot names of a frame scope, which framed repetitions name before their element and declare after it. It does nothing to the state. |
| `flow.env` |  | env |  | Returns the variable environment. |
| `flow.setEnv` | e env |  |  | Sets the variable environment to e. |
| `flow.frame` |  | frame |  | Returns the current capture frame. |
| `flow.setFrame` | f frame |  |  | Makes f the current capture frame. |
| `flow.recLen` |  | int |  | Returns the number of recovered errors. |
| `flow.kidsLen` |  | int |  | Returns the height of the stack of gathered values. |
| `flow.streamOn` |  | bool |  | Reports that the invocation delivers the elements of its #stream repetition: a stream parse with a callback, at the top call of its start rule. |
| `flow.skipHit` |  | bool |  | Ends the iteration of the walk before, if any, and reports whether the walk has a record of the position for its key; it then takes the rest of the walk from the record, which moves the position to its end, records the recorded expectations again and binds the variables that the element of a keyed run site binds, and the walk stops. A walk begun with Build takes no record without values. |
| `flow.runHot` |  | bool | `site` | Reports whether the walks of the run site keep records, separately inside and outside lookaheads; a walk of a site that is not hot is an ordinary repetition, which flow.runWalked ends. Testing whether the site is hot is free ([Run Sites](../runtime/parsing.md#run-sites)). |
| `flow.skipKids` |  | vals |  | Ends the walk begun with Build as flow.skipEnd does, and returns the child list of the values of its iterations, then those of the record it took, as flow.kids does for a repetition without records. |
| `flow.scanReach` | start int, count int | int | `site` | For a walk of the scan site that began at start and has matched ScanHotSpan characters, count of them: at a hot site with a record of start it moves the position to the recorded end and returns the recorded count; otherwise it returns count. At a hot site it charges a unit for the lookup and one more for the record that it finds; at a site that is not hot it charges nothing ([Scan Sites](../runtime/parsing.md#scan-sites)). |
| `flow.scanKids` | start int, base int, count int | vals | `site` | Returns the elements of a walk of the scan site that began at start, gathered on the stack from base, which matched count characters: for a walk that reached ScanHotSpan characters it ends the walk as flow.scanEnd does, and the result is the elements of the record that the walk took, if it took one (the stack is dropped to base), or the gathered elements, which the record that it makes shares. It charges a unit for the record that it makes, as `flow.scanEnd` does, and nothing for the elements. |
| `flow.resumeOn` |  | bool |  | Reports that the repetitions resume their runs: the parse is a Document's, outside lookaheads and the evaluation of a skip expression (the silent depth is 0). |
| `flow.resumeStart` |  | run | `resume` | Starts the run of the repetition at the position and returns it, with the record of the run that it takes over, as the composition runStart does ([Repetition Runs](../exec-ir/compositions.md#repetition-runs)). |
| `flow.resumeMore` | r run | bool |  | Reports whether the run r may take another element: no reused element ended it, and it has fewer than its maximum (runMore). |
| `flow.resumeNext` | r run | value, bool |  | Reuses the next element of the run r that the record it took over holds, as the composition runNext does: it replays what evaluating the element did (the position, the examined input, the expectations and the statistics) and returns its value and true, for the caller to keep. It returns false when the repetition evaluates its next element instead. A reused element counts no unit (spec/runtime/parsing.md#compute-units): the call checks the limits, which may end the parse, in place of the step of the iteration that keeps the element. |
| `flow.resumeBegin` | r run |  |  | Starts evaluating an element of the run r, after the step of its iteration: its examined input and its expectations are recorded apart from those of the enclosing call (runBegin). |
| `flow.resumeEnd` | r run, ok bool, x value |  |  | Ends evaluating the element of the run r that flow.resumeBegin started, with its success ok and its value x, before a failed element resets the state: it adds the element's examined input and expectations to those of the enclosing call, and records the element if it matched (runEnd). |
| `flow.resumeFinish` | r run |  |  | Records the run r of a repetition that matched, before its value is built (runFinish). A repetition that fails drops its run without a call. |
| `flow.hasLit` |  | bool | `lit` | Reports that the input at the position begins with the literal, read in place without recording anything; false where the input cannot be read in place. |
| `flow.matchLit` |  | value, bool | `lit` | Matches the literal at the position and moves past it, recording its expectation on failure. Its results are its value (not used) and whether it matched; on failure the position is unchanged. It counts the position units it advances over, none on failure. |
| `flow.matchClass` |  | bool | `classid` | Matches one character of the class through its membership table and moves past it, recording the class's expectation on failure; reports whether it matched. It counts the position units of the character it advances over, none on failure. |
| `flow.inClass` | ch rune | bool | `classid` | Reports whether the code point ch is in the class. It counts no compute unit: the test examines a character without advancing. |
| `flow.guardFails` | ch rune, more bool | bool | `guard` | Reports that the guard of a class skips its alternative: the character that more (there is one) and ch describe is not in the class, and the nesting limit allows the guard's depth of calls. A guard counts no [compute unit](../runtime/parsing.md#compute-units): it examines the next character without advancing. |
| `flow.guardLitFails` |  | bool | `guard` | Reports that the guard of a literal skips its alternative: the input at the position does not begin with the literal's first character, and the nesting limit allows the guard's depth of calls. It compares the character as the literal does and examines what the literal would ([Slots](../runtime/documents.md#reuse-model)): in code points the character at the position; in bytes the UTF-8 encoding of the character byte by byte, up to the first byte that differs, or the end of the input, except U+FFFD, which also matches one invalid byte and so decodes the character; a folded character matches an ASCII letter in either case. It reads no further input than it examines. In a stream parse the read costs what loading the input costs; in a Document parse it records how far the parse examined the input. A guard counts no [compute unit](../runtime/parsing.md#compute-units): it examines the next character without advancing. |
| `flow.atAnchor` |  | bool | `anchor` | Reports whether the position is at the anchor. It counts no compute unit: it examines input without advancing. |
| `flow.skipTerminal` |  | bool | `terminalprogram` | Runs the terminal region as one program from the position and reports whether it matched; on a match the position moves past it. Its guards may decode the character at the position where `flow.guardLitFails` compares bytes: it runs only in functions that record no examined input, charge nothing and read no stream, and decoding admits the same characters. It counts the position units that each of its terminals advances over, again after the program backtracks. |
| `flow.skipClasses` |  | bool | `classrun` | Matches the classes of the run in turn and reports whether they matched; on a match the position moves past them. On failure the position is past the classes that matched, at the one that did not, whose expectation it records there; the code that fails returns to its mark. It counts the position units of each character it advances over, those before a class that fails included. |
| `flow.callRule` |  | value, bool | `rulecall` | Calls the rule from the Operators Rule level. Its results are the rule's value and whether it matched; on a match the position is the end of the rule's match, on failure it is unchanged. The call counts its unit and the key of a memoized rule, as the composition call does. |
| `flow.callOutline` |  | value, bool | `outline` | Runs the node of the outlined call, which a function of its own lowers, under the current frame. Its results are the value of the node, built if Build, and whether it matched. |
| `flow.isolate` |  | expmark |  | Begins an expectation scope at the position and returns its mark. |
| `flow.unisolate` | m expmark | int, expids |  | Ends the expectation scope m. Its results are the farthest failure inside it and its expectations. It charges nothing, as `exp.unisolate` (the [Exec operations](../exec-ir/operations.md)): each expectation was charged when it was recorded, and a merge of them charges each again. |
| `flow.text` | start int | value |  | Returns the input from start to the position. |
| `flow.literalText` | start int | value | `chars` | Returns the text of the literal matched from start, which differs from the literal where it holds the replacement character. Where it differs it takes the input from start to the position, charged as flow.text charges it. |
| `flow.str` |  | value | `chars` | Returns the string constant. |
| `flow.newMatch` | start int, text value | value |  | Returns a match value from start to the position with the text. |
| `flow.newNode` | start int, kids vals | value | `list` | Returns a tuple node, or with List a list node, from start to the position with the children kids. |
| `flow.newTerminalNode` | start int, text value | value |  | Returns a terminal of the Node runtime from start to the position, with the text. |
| `flow.newBranchNode` | start int, kids vals | value | `list` | Returns a branch of the Node runtime from start to the position: a sequence, or with List a list, of the children kids. |
| `flow.newVals` |  | vals | `room` | Returns an empty list of values with room for the immediate. |
| `flow.kids` | base int | vals |  | Returns the list of the values gathered above base, which it removes from the stack. |
| `flow.shapeSingle` | x value, start int | value, bool | `shape` | Returns the value of x, matched from start by an expression of the shape, where it takes one element, and whether it is a single element (not used). |
| `flow.shapeCapture` | x value, start int | value | `shape` | Returns the value of x, matched from start by an expression of the shape, where it is captured. |
| `flow.scopeValue` | x value, start int | value | `shape` | Returns the value of a scope whose body, of the shape, matched x from start, building a tuple for it if the shape needs one. |
| `flow.attachCaptures` | x value, start int, caps value (variadic) | value | `attach` | Returns x, matched from start, with the captures of the scope attached: the values caps of the scope's slots. |
| `flow.attachFrame` | x value, start int, f frame | value | `attach` | Returns x, matched from start, with the captures of the scope attached: the values of the frame f. |
| `flow.markState` |  | mark | `statemark` | Returns a mark of the parser state, which flow.reset returns to: the position, and the variable environment, the capture trail and the recovered errors as the immediate names them. |
| `flow.markPos` | m mark | int |  | Returns the position of the mark m. |
| `flow.newFrame` |  | frame | `names` | Returns a new frame for the scope of the names. |
| `flow.frameVal` | f frame | value | `slot` | Returns the value of the slot of the frame f. |
| `flow.keep` | exps expids | expids |  | Returns the expectations exps, kept beyond the scope that recorded them. |
| `flow.makeError` | far int, exps expids | error |  | Returns the syntax error at far with the expectations exps, which a recovery builds: one [compute unit](../runtime/parsing.md#compute-units), a syntax error built. |
| `flow.errorValue` | start int, x error | value |  | Returns the value of the recovered error x over the input from start to the position. |
| `flow.newErrorNode` | start int, x error | value |  | Returns the error node of the recovered error x in the Node runtime, over the input from start to the position. |
| `flow.freshNode` | x value | value |  | Returns x if it is a node its maker still owns, or nil. |
| `flow.convertResult` | x value, owned value (variadic) | value | `conversion` | Returns the value x of the rule converted to the rule's node type; with Owned, the only owned argument is the fresh node of the body, which converts in place. |
| `flow.useCtx` | start int | ctx |  | Returns a new context for the action of the rule entered at start. |
| `flow.pred` | caps value (variadic) | bool | `predicate` | Evaluates the predicate with the captures caps (or the current frame) and reports whether it holds. |
| `flow.bind` | caps value (variadic) | bool | `binding` | Binds the variable of the binding to the value of its expression, evaluated with the captures caps (or the current frame), and reports whether the evaluation succeeded. |
| `flow.eval` | c ctx, caps value (variadic) | value | `action` | Returns the value of the action of the function's rule, evaluated in the context c with the captures caps (or the current frame). |
| `flow.ctxFinish` | c ctx, x value | value |  | Returns the value x of an action finished in the context c: a struct that c made takes the range of the rule, and the values that c made are forgotten. It searches the values that c made for x, and charges one unit for each it compares with x. |

## Immediate Operands

The immediate of a call has a Go type that the declaration of the operation names and that verification checks (see
[Immediate Operands](../code-ir/module.md#immediate-operands)). The Immediate column names the type by its lower-cased
name without the package. The immediates are the following; a listing prints each in brackets before the arguments (see
[Text Form](text-form.md)).

- `silence`: the change of the silent depth, and for a lookahead its site and, at its end, whether its body matched.
- `site`: the number of a skip-run, run or scan site.
- `resume`: a repetition that a Document parse resumes, its least and greatest number of elements (-1 for no bound), and
  whether the values of its elements hold no position, so that reused elements may move with an edit.
- `runwalk`: a run site, whether its iterations have values, and whether its records are keyed by the values of the
  variables of the site's key rather than by the position alone.
- `slot`: a capture slot of a frame.
- `expectation`: an expectation of the grammar, and whether it is the message of a failed `@error`.
- `expid`: an expectation of the grammar, by its index in the expectations of the module's
  [tables](functions.md#tables). The other immediates name an expectation the same way.
- `spread`: whether the children of a tuple node are appended instead of the node.
- `rulecall`: the rule that a call names and the level of the Operators Rule it is called from.
- `outline`: an outlined call, by its number, the slots of the capture scope of the current frame, and whether the call
  builds the node's value. The lowering of a module numbers the distinct outlined calls (a node, run under a frame with
  these slots, building or not) from 0 in the order it makes them; the plan of the module's table finds the function
  that runs the node of each by its number.
- `observedskip`: the rule call that the alternative a guard skips begins with, as the grammar writes it: the name of
  the rule and the level of the skipped call, its minimum binding level as `rulecall` and a trace event count it (0
  for a call that names no level).
- `chars`: a string: the text of a literal, a string or the name of a rule.
- `list`: whether a node is a list, rather than a tuple.
- `room`: the room of a new list of values.
- `attach`: the slot names of a scope, and whether the captures are the values of a frame instead of variables.
- `statemark`: which parts of the state besides the position the code between a mark and its reset may change: the
  environment, the trail of capture writes and the recovered errors.
- `conversion`: the rule of a result conversion, and whether the fresh node of the body is passed in place.
- `names`: the slot names of a capture scope, which printers declare once.
- `predicate`, `binding` and `action`: the number of the action site of the predicate, the binding or the action of the
  rule in the Action module of the table ([Action Expressions](../exec-ir/lowering.md#action-expressions)), the
  variable that a binding binds, and the captures that the function of the site reads, which are the slot names of the
  scope or the current frame.
- `lit`: a literal: its text, whether it is case-insensitive, and its expectation, which the empty literal has none of.
- `classid`: a character class, by its index in the classes of the module's [tables](functions.md#tables).
- `guard`: the terminal that a guard tests first: the first code point of a literal and whether it is
  case-insensitive, or a class; its expectation; and the depth of calls that the guard adds.
- `anchor`: the kind of an anchor: the beginning or the end of the input or of a line.
- `classrun`: a run of single characters of classes, as the classes in turn.
- `shape`: the static shape of a value.
- `terminalprogram`: the program of the value-free terminal expression of a terminal region that the operation
  matches, made of case-sensitive literals and classes in sequences, choices, options and negative lookaheads. The pass
  that writes the operation assembles the expression into the operations of the runtime's terminal program, with a
  guard on each alternative of a choice that begins with a literal or a class. The program holds its classes as ranges
  and its literals as text, and its expectations as `expid` names them, in the order in which it first records them;
  the empty literal's is its quoted text, `""`. A printer only numbers those expectations in its parser and names the
  classes and literals.

## Quantities

The cost bound of a Flow operation (see [Costs](../code-ir/module.md#costs)) is in terms of the quantities below. An
operation of constant work charges nothing, because the call or the step around it has paid. The values that the parse
builds, gathers, takes off, drops and attaches captures to charge nothing ([Cost](../runtime/parsing.md#cost)); the value
of a recovered error charges a unit and its message. An operation whose Go form is a
[composition](../exec-ir/compositions.md) is bounded by what that composition charges.
An operation with a cost is declared No return ([Operations](../code-ir/module.md#operations)), since a check ends
the parse when a limit is exceeded; the verifier rejects one that is not.

| Quantity | Measures |
|:--|:--|
| `lit` | The code points of the operation's literal. |
| `classes` | The classes of the operation's class run. |
| `terminal` | The steps of the operation's terminal program. |
| `ranges` | The ranges of the operation's class that a membership test compares: of the class of the immediate, or of the class that a guard tests first (none for a literal). |
| `body` | The units that the body of a called rule, or the code of a predicate, binding or action, charges. |
| `text` | The code points of the input that a value copies: in a [stream](../runtime/streaming.md) parse, the length of a text. |
| `merge` | The units of the compositions over the expectation record (`expect`, `keep` and `mergeExpected`) for the expectations that the call records, keeps or merges, as the quantity `merge` of the [Exec dialect](../exec-ir/machine.md#quantities) measures them. |
| `call` | The units of the call protocol around a rule body (the composition `call`): the first-call record, the memo's lookups and stores and the growth of their tables. |
| `vals` | The values of the list that the call returns, which bound the elements of a record that it copies. |
| `stream` | The work of a stream parse at the call: loading, compacting and scanning its buffer, and the commit of a delivered element (the composition streamCommit); 0 outside a stream parse. |
| `error` | Building a syntax error: a unit for each expectation, the steps of sorting their descriptions and of finding the line, and at the first error of a parse of a whole input, a unit for each code point that the table of line starts reads. |
| `error_text` | The size of the message of a recovered error that a value holds: its bytes and a unit for each of its parts. |
| `key` | The run key of a walk of a run site that is not free of the environment: reading and hashing the values of its variables, comparing them with the key of a record and binding the variables of a record taken, each charged as for a memo key; 0 for other sites. |
| `records` | The records that the end of a walk of a skip-run or run site stores: one for each position that the walk visited while recording. |
| `walk_vals` | The values that a walk of a run site whose iterations have values holds at its end: those it gathered and those of the record it took. |
| `made` | The values that the context of an action made ([Values Made](../exec-ir/values.md#values-made)) which finishing the action's value searches for it: those up to it, all of them when it is a struct that the context did not make, and none when it is no struct. |
| `deps` | The dependencies that a lookahead which ends in a context of left recursion stores with its outcome, as `deps` of the Exec stage measures them; none outside such a context and for a lookahead that read no seed. |
| `table` | The work of storing the outcome of a lookahead in the context table at its position, as `table` of the Exec stage measures it: one for the store, one for each dependency set of the head that it scans and one for each result under the values the set selects that it compares; none where `deps` is none. |

A conformance test of the generated Go parsers checks every call of a Flow operation against its bound and its
contract, with the input read through the reference model of the input.
