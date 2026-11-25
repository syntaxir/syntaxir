# Lowering From Core IR

Lowering from Core IR turns the rules of a rule table of [Core IR](../core-ir/README.md) into the functions of one module
at the Flow stage. A table is a Node table, a recognizer table or a typed table; its input is the Core module of the
table after the Core passes, the options of the table and the plan of the table, which lists the functions that the
module holds. This chapter specifies what the plan holds, how each node of Core IR is lowered and what a lowered
function guarantees, so that the proofs and the backends can rely on it.

The chapter separates two kinds of statement. The [requirements](#requirements), every sentence with MUST or MUST NOT,
and the sections [The Plan](#the-plan), with its table of uses, [Functions](#functions) and [Nodes](#nodes) are
normative, except the paragraphs marked **Current choice**: an implementation that breaks a normative statement is
wrong. The paragraphs marked **Current choice** describe how the implementation decides what the requirements leave
open, such as whether a rule runs flat or framed and which nodes run as functions of their own; a change to them is an
implementation change, which keeps the requirements and updates the paragraph, not a change to this specification.

## The Table

The functions of a table share its Core module, its [tables](functions.md#tables) of expectations and classes, its Action
module ([Action Expressions](../exec-ir/lowering.md#action-expressions)) and its options. The options fix what every
function of the table counts, records and builds:

| Option | The functions of the table |
|:--|:--|
| Logical | Count [compute units](../runtime/parsing.md#compute-units): each loop iteration has a `flow.step`, which checks the counter, and the operations count their events |
| Guards | Under Logical, keep a guard only where its skip can make what evaluating the alternative makes (exact, the default); or keep only the guards whose skip makes no call; or keep no guard, for a module that parses inserting missing tokens. A fourth setting, which keeps every guard and makes none of the effects of the calls it skips, is not exact and serves only measurements of what those effects cost |
| Recovers | Save and restore the number of recovered errors at each backtracking point: the module has a `Recover` or a `Missing` node |
| Inserts | Lower each `Missing` node with its insertion, and save the length of the path of insertions where the code after a backtracking point may insert. Without it a `Missing` node lowers as its literal |
| Suspend | Demand the input before each read of it (`flow.inputDemand`), for a stream whose input arrives in chunks. It changes nothing else: the results and the compute units are those of the lowering without it |
| Observe | Report the alternatives that guards skip (`flow.observeSkip`), and run no node as a function of its own |
| Node | Build the values of the Node runtime; without it, those of the typed runtime |
| Document | Read input only in ways that record how far a parse examined it ([Documents](invariants.md#documents)) |
| Resume | Lower the repetitions that a Document parse resumes with the operations of their runs (`flow.resumeOn` and the rest), in Node functions lowered with Document |
| Traced | Lower the traced copy of a rule that an observed parse runs: every read records how far the parse examined |
| Outlined | The nodes that run as functions of their own ([Outlined Nodes](#outlined-nodes)) |
| Guarded | The alternatives that keep their guard, and what the skip of each makes ([Guards](#guards)) |

## The Plan

The plan of a table lists its functions in the order in which they are lowered, each with its use in the table, its Core
rule, its name and its form, and for a framed function the node it lowers, the capture slots of its frame and whether
it builds a value. It also fixes the capture-slot layout of each rule's frame: the names of the slots of the Core rule's
scope.

### Requirements of the Plan

- A table has one plan, and every backend that runs the table from Flow IR, a generated parser or the bytecode machine,
  MUST run the functions of that plan, lowered from the same Core module with the same options. So the functions, and
  the compute units they count, are the same on every such backend.
- A rule body has at most one function in a table, a flat one or a framed one. The skip and each line of an Operators
  Rule have at most one function each, the body of the start rule of a stream parse at most one, and an outlined node
  at most one for each frame layout and building of its calls. So the number of functions of a table is bounded by the
  size of its Core module.
- A flat function MAY carry a framed function of the same rule as its fallback. The table runs the fallback in its
  place when the flat function is refused, and a rule whose flat function is refused and that has no fallback has no
  function in the table. A fallback MUST give the outcome of the flat function on every input and in every state of a
  parse, and count the same compute units.
- Whether a flat function is refused MUST be decided by an analysis of its Flow function as the lowering writes it,
  before any pass of the Flow stage runs, once for every backend: a refused function is replaced by its fallback in the
  module that the passes see. The flat functions of a typed table are never refused.
- The frame layout of each rule MUST be the one that every backend uses for the rule's frame: a backend that runs a
  framed function of the rule checks its frame against it.

A rule that the plan gives no function, other than an Operators Rule, whose runtime runs the functions of its skip and
lines, cannot run from Flow IR; a generated parser reports it as a rule that it cannot print.

### Uses

| Use | Function | What it computes |
|:--|:--|:--|
| `RuleBody` | Flat or framed | A flat function is the whole rule: it matches the body and makes the rule's value. A framed function matches the rule's body in the frame that the runtime gave the call and returns the body's value; the runtime owns the frame, the memo entry, the rule's value and the entry of the call |
| `OperatorsSkip` | Framed | The skip line of an Operators Rule, without a value and without slots of its own, in the frame it is run in |
| `OperatorsLine` | Framed | A line of an Operators Rule, its operands in order and then its operators in the order of their levels, in the frame of the line's scope, building the line's value unless the rule has none or the line's action does not read it |
| `StreamBody` | Framed | The body of the start rule that a stream parse runs, in the rule's frame, whose `#stream` repetition delivers each committed element to the callback instead of gathering it when the invocation streams |
| `Outlined` | Framed | A node of a function of the table, run under the caller's current frame ([Outlined Nodes](#outlined-nodes)) |

### Routes

**Current choice.** In a Node or recognizer table, a rule whose body runs flat is plain, lean and value-free, is no
left-recursion leader, and its body has no cut, recovery, predicate, inlined call or Operators Rule; its flat function
carries the rule's framed function as its fallback when the rule has one. A rule has a framed function when its body
has no inlined call and no Operators Rule and its action reads only the captures of its frame. A flat function is
refused when a reachable statement reads its entry position or its context, which a flat rule has no parameters for, or
a variable holds a value, which a value-free flat rule does not keep; reachability is that of the dead-store analysis of
Code IR. In a typed table, every rule whose captures can be variables, which excludes left-recursion leaders, Operators
Rules and rules with a recovery or an inlined call, runs flat and returns its typed value; a left-recursion leader or a
rule that recovers is a framed function, which builds its value when the rule's value is wanted, when its frame has
the slots of the frame that the typed runtime shares with the Node table. The lines of an Operators Rule are planned as framed functions when they have no
inlined call and no Operators Rule. The rules that no entry point reaches are left out, and those of the Node and
recognizer tables are lowered as a part of their own, so that the expectation table keeps a row for every rule.

## Functions

The lowering of a function is the lowering of its node at a failure label: the statements of a node run to their end
when it matches, leaving its value in a variable when the function builds it, and exit to the failure label when it
fails. Every function that the lowering makes MUST satisfy the [invariants](invariants.md); the Flow verifier checks
the module before any pass sees it, and a function that it rejects is an internal error of the lowering.

- A **local** function lowers a flat rule. Its entry variables are the start, the result and the context. Its body is
  the rule's body, building its value if the rule uses it, followed by the **finish**, which makes the rule's value from
  the body's value and the captures: nil for a value-free rule; the value of the rule's action, evaluated with
  `flow.eval` in the context of the rule (`flow.useCtx`, `flow.setWhere`), finished with `flow.ctxFinish`, or, for a
  struct that the action makes, dropping the structs made inside it; the body's value for a splice; or the scope value
  of the body with the captures attached. A rule whose value is converted to a node type converts it last
  (`flow.convertResult`). The captures of a local function are variables of it.
- A **framed** function lowers a node in a frame of the runtime: its captures are the slots of the current frame. It
  has no entry variables, and returns its value and true on success, and nil and false on failure. If its node cuts,
  its outermost scope reads the cut flag from the register `cut` on entry and publishes it to that register on both
  returns.

A node that begins at a point that no reachable statement leads to is not lowered.

### Backtracking Points

A choice, an optional, the element of a repetition, a negative lookahead and a recovery are backtracking points: before
their body runs they save the state that their body may change and that a failure of the body must undo, and they
restore it when the body fails.

- In a local function the saved state is the position; the number of recovered errors (`flow.recLen`, restored with
  `flow.restore`) when the table recovers; the length of the path of insertions (`flow.missingLen`, `flow.resetMissing`)
  when the table inserts and the body may recover; the variable environment (`flow.env`, `flow.setEnv`) when the body
  binds variables; and the values of the capture variables that the body may write, except for the element of a
  repetition with a scope of its own, whose captures are cleared for each iteration.
- In a framed function the saved state is a mark of the parser state (`flow.markState`), which names the parts the body
  may change: the environment, the recovered errors, the insertions and the capture trail. A failure returns to it
  with `flow.reset`.

When the body of a function cuts, each backtracking point inside it other than a negative lookahead has a cut flag of
its own, false before its body runs; a `Cut` sets the flag of the innermost one inside the innermost lookahead around
the cut, if any. When a body fails with its flag set, the backtracking point fails instead of trying what follows. So a
cut at the top level of a local function, or in the body of a lookahead outside every backtracking point inside that
body, sets no flag: the body of a lookahead starts with none, and a negative lookahead has none of its own.

### Nodes

| Node | Lowering |
|:--|:--|
| `Lit` | Matches the literal with `flow.matchLit`, which records its expectation when it fails; under Suspend it first demands the literal's bytes. Its value is a terminal node of the literal's text, of the input matched for a case-insensitive literal, and of the literal text for a literal that holds U+FFFD |
| `ClassNode` | Reads the character at the position (`flow.peek`) and fails, recording the class's expectation, when there is none or it is not in the class (`flow.inClass`); otherwise advances over it (`flow.advance`). Its value is a terminal node of the character |
| `Any` | Reads the character at the position, fails with the expectation of any character when there is none, and advances over it |
| `Anchor` | Fails, recording the anchor's expectation, when the position is not at the anchor (`flow.atAnchor`, or `flow.atEnd` for the end of the input or of a line, which demands the next character under Suspend) |
| `Succeed` | Nothing |
| `FailNode` | An exit to the failure label |
| `Seq` | Its items in order. Its value is a tuple node of the values of the items: an item whose shape is elements contributes its children, an item of one element its value, and others nothing |
| `Choice` | A backtracking point. The alternatives are tried in order, each in a block of its own: one that matches gives the choice its value, shaped as one element, and ends the choice; one that fails restores the saved state and, unless its cut flag is set, lets the next one run; after the last, the choice fails. The alternatives after one that cannot fail are not lowered. An alternative that keeps its guard is skipped first when the guard rules it out ([Guards](#guards)) |
| `Opt` | A backtracking point around its body. When the body fails it restores the saved state, fails if the body cut, and its value is nil |
| `Repeat` | A loop of at most the maximum of iterations ([Repetitions](#repetitions)) |
| `Look` | A positive lookahead saves the position, and the number of recovered errors and the length of the path of insertions where its body may change them, enters the silent depth with its site (`flow.silent`), matches its body and returns to the start on both outcomes, failing when its body fails; it keeps the captures and bindings of its body. A negative lookahead is a backtracking point that enters the silent depth with its site (`flow.silent`) too: it fails when its body matches, and restores the saved state on both outcomes. Neither builds a value |
| `Cut` | Sets the cut flag of the innermost backtracking point that has one ([Backtracking Points](#backtracking-points)) |
| `Call` | Calls the rule with `flow.callRule`, from the Operators Rule level of the call, and fails when the call fails. Its value is the rule's value |
| `Nest` | Not lowered: a rule whose body holds an inlined call has no function in the plan |
| `OperatorsRule` | Not lowered as a node: the runtime runs the Operators Rule with the functions of its skip and its lines |
| `Text` | Matches its body without a value; its value is a terminal node of the text that the body matched (`flow.text`) |
| `Drop` | Matches its body without a value; it has none |
| `Capture` | Matches its body, building its value, and stores the value shaped as a capture (`flow.shapeCapture`): in the variable of the slot in a local function, and in the slot of the current frame (`flow.setCapture`) in a framed one. A capture of a projected repetition stores the list of the values that the capture it projects takes in each element |
| `Pred` | Evaluates the predicate's action site (`flow.pred`) with the captures it reads, as arguments in a local function and from the current frame in a framed one, and fails when it is false |
| `BindNode` | Evaluates the binding's action site and binds the variable (`flow.bind`), failing when the binding fails |
| `Label` | Isolates the expectations recorded inside its body (`flow.isolate`). When the body matches it merges them into the enclosing scope (`flow.mergeExpected`); when it fails, `@diagnostic` merges them renamed to its expectation (`flow.mergeRenamed`) and `@error` records its expectation as the message at the furthest position they reached, and the label fails. Under Inserts a label that is the message of the insertions inside it opens a scope of insertions for its body (`flow.missingScope`) |
| `Recover` | Only in a framed function. A backtracking point that isolates the expectations of its body. When the body fails it keeps those expectations (`flow.keep`), restores the saved state and matches the skip expression without a value, which must advance the position; it then builds the syntax error (`flow.makeError`), appends it to the recovered errors, and its value is the error's value. When the skip also fails, the recovery restores the saved state, merges the expectations of the body and fails |
| `Missing` | Without Inserts, its literal. With Inserts, it isolates the expectations of the literal: when the literal matches they are merged; otherwise the position goes back to the start and the insertion is made (`flow.insertMissing`); when the parse makes none, the site fails, merging the expectations; its value is the value of the missing token |
| `Stream` | Its repetition. In the function of use `StreamBody`, which is a framed Node function and whose repetition is not projected, the repetition delivers each element: when the invocation streams (`flow.streamOn`) it begins the stream before the loop (`flow.streamStart`) and hands each element to the callback (`flow.emitStream`) instead of gathering it |
| `Scan` | A loop that reads one character of the class, or any character, at each iteration, ends recording the expectation when it reads none, and advances over it; the scan fails when it read fewer than its minimum. A scan site records its walks ([Scan Sites](../runtime/parsing.md#scan-sites)) |
| `ClassRun` | Not lowered: the Core module of a table holds no class run. Consecutive classes are matched as one by a form of the Flow stage ([Forms](passes.md#forms)) |
| `Terminal` | Not lowered: the Core module of a table holds no terminal node. The terminal regions of a local function are forms of the Flow stage, which a pass may match as one program ([Forms](passes.md#forms)) |

### Repetitions

A repetition is a loop of at most its maximum of iterations, unbounded when it has none. Each iteration first makes the
visit of the walk it belongs to, if any, then, under Logical, its `flow.step`, and then matches its element as a
backtracking point. The element of a repetition with a scope of its own has fresh captures: in a local function its
capture variables are cleared, in a framed function it runs under a new frame (`flow.newFrame`), and its value carries
its captures. An iteration that matches adds the value of its element, as a scope value, to the values gathered for the
repetition (`flow.pushKid`), and ends the loop when it did not advance the position. An iteration whose element fails
restores the saved state and ends the loop, and the repetition fails when the element cut. After the loop, the
repetition fails, dropping the values it gathered, when it matched fewer elements than its minimum. Its value is a list
node of the gathered values.

- A **skip-run site** begins its walk with `flow.skipBegin`, visits each iteration with `flow.skipVisit`, and once it has
  its minimum takes the rest of the walk from a record when one answers (`flow.skipHit`); it ends the walk with
  `flow.skipEnd` ([Skip Runs](../runtime/parsing.md#skip-runs)).
- A **run site** begins and ends its walk with the operations of its walks (`flow.runBegin`, `flow.skipVisit`,
  `flow.skipValue`, `flow.skipEnd`, `flow.runWalked`), and the list of a walk that took a record is the list of the
  record (`flow.skipKids`) ([Run Sites](../runtime/parsing.md#run-sites)).
- A **scan site**, a repetition of one character without a maximum or a scope of its own, takes the rest of a walk from
  a record or records itself (`flow.scanReach`, `flow.scanEnd`, `flow.scanKids`)
  ([Scan Sites](../runtime/parsing.md#scan-sites)).
- A repetition that a Document parse **resumes**, lowered with Resume, decides once whether the walk resumes its run
  (`flow.resumeOn`); a resuming walk reuses the elements of the run that its record holds (`flow.resumeNext`), evaluates
  each other element between `flow.resumeBegin` and `flow.resumeEnd`, and records the run when it matched
  (`flow.resumeFinish`). A walk that does not resume is the repetition's ordinary walk.
- A **projected** repetition gathers, for each element, the value that the capture it projects takes there; in a
  framed function it makes one frame for its elements and clears it for each (`flow.clearVals`).

### Guards

The Core pass `guards` gives an alternative a guard when the next character can rule it out. Whether a table keeps the
guard is the option Guarded, and an alternative that keeps it is skipped, before it is tried, when its guard fails at
the position: the guard of a literal compares the literal's first character (`flow.guardLitFails`), and the guard of a
class reads the character once for all the class guards of the choice (`flow.guardFails`). A skip records the
expectation of the guard, under Logical makes the effects of the calls that evaluating the alternative would make
before it fails (`flow.skippedCalls`), and under Observe reports the skip.

- A table MUST keep a guard only where skipping the alternative records the same expectations as evaluating it and
  leaves the state that evaluating it leaves where that state is observable: always the position, the captures, the
  bindings, the recovered errors and the insertions, and the memo entries and records that the calls of the
  alternative would store only when the table counts units (Logical) or the parse is a Document
  ([Evaluations of Rules](../runtime/parsing.md#evaluations-of-rules)). The setting of Guards that serves only
  measurements is the exception.
- A table MAY keep fewer guards than the current choice below, down to none: a kept guard only spares the evaluation
  of an alternative that would fail, so keeping one is an optimization.
- A guard whose skip passes over a call of a rule that a Document memoizes is taken only outside Documents
  (`flow.inDocument`): a Document parse evaluates the alternative, so that its memo records what the parse examined.
- In a table that inserts, a guard is taken only in a parse that does not insert missing tokens (`flow.inserting`).

**Current choice.** Without Logical and without Document every guard is kept. Otherwise a guard is kept when the calls
that evaluating the alternative makes before its first terminal fails are known, which the skip then makes; a table of
the bytecode machine keeps only the guards whose skip makes no call, and a table that parses inserting missing tokens
keeps none.

### Outlined Nodes

An outlined node runs as a framed function of its own, which its caller calls with `flow.callOutline` under its current
frame; the caller passes the slots of its scope and whether the node builds its value, and the call's results are the
node's value and whether it matched. The function belongs to the rule of its caller, which charged the entry of the
rule, so it counts only what its node counts.

- An outlined node MUST be a node that a framed function can lower, with no `Cut` and no `Stream` inside it; in a
  local function it MUST also set and read no capture, and hold no predicate, binding or recovery.
- The root of a function and the nodes of a function lowered with Observe are never outlined, and neither are the
  nodes of a stream body from its `#stream` repetition on: the repetition, its element and the nodes after it. A node
  before the repetition may be outlined; its function is lowered as for any framed function of the table, and under
  Suspend it demands its input as the stream body does.
- Each distinct node is lowered once for each frame layout and building of its calls, and its function follows the
  functions of the plan, in the order in which the functions of the table first call it.

**Current choice.** Only the TypeScript pipeline outlines, in its Node and recognizer tables: a large node, the large
parts of one, and, in a table that builds values, a structure that occurs more than once where it would not already be
shared.

## Requirements

The requirements below hold of every function that the lowering makes, before and after the passes of the Flow stage,
which preserve them ([Passes](passes.md)). The specification states them here; the proofs that the lowering meets the
requirements of Outcome, Size, Work and Compute Units are pending, and the proofs of the bounds on a parse use them as
stated.

### Outcome

The functions of a table, run under the contracts of the operations of the Flow dialect and of the runtime that owns
their frames, MUST give the outcome that the [Parser and Runtime Specification](../runtime/README.md) defines for the
rules they lower: whether a rule, line or node matches, the position after it, its value, the expectations it records,
the recovered errors and insertions it leaves, and, when the table counts units (Logical) or the parse is a Document,
the memo entries and records it stores. Otherwise the memo is not observable: a memo entry answers a call as its
evaluation would, so a skip of a guard that stores none of the failure entries of the calls it passes over gives the
same outcome. A framed function and the flat function it is the fallback of give the same outcome.

### Size

Each node lowers to a number of statements that is at most a constant times one plus the number of its direct children
and of the capture slots of its scope, plus the statements of its children. With the bound of the plan on the number of
functions, the size of the Flow module of a table is at most a constant times the number of its Core nodes times the
greatest number of capture slots of a scope, which is quadratic in the size of the Core module in the worst case. The
size depends only on the grammar, so it enters the constants of the bounds on a parse and not their dependence on the
input.

### Work

There is a constant $c$, which depends only on the grammar and the options of the table, such that a parse runs at most
$c \cdot (C + 1)$ statements and calls of operations in the functions of the table, where $C$ is the
[cost](../runtime/parsing.md#cost) of the parse. The work inside an operation is the operation's, bounded by its contract,
and the elements that a Document parse reuses from a resumed run are bounded by
[Work of a Document Parse](../runtime/documents.md#work-of-a-document-parse). An invocation of a framed function holds at
most one frame more than the depth to which repetitions with a scope of its own nest in its node, and each frame that it
makes is made by an iteration.

The bound rests on the structure of the lowering: an invocation of a function is a call or a body evaluation that the
cost counts, a try of the skip or of a line of an Operators Rule, of which each step of the rule makes at most one per
line and each step applies an operator or ends the evaluation of the rule, or the call of an outlined node inside the
invocation of its caller; outside loops an invocation runs each
statement of its function at most once; and every iteration of a loop is an iteration of a repetition or a read of a
character of a scan, which the cost counts.

### Compute Units

A function lowered with Logical MUST count, through its operations, exactly the logical events of
[Compute Units](../runtime/parsing.md#compute-units) that the evaluation it lowers makes: a guard that skips an
alternative makes the events of the calls that the alternative would have made, and no event is counted twice or left
out. So the functions of a table count the total that Compute Units requires, the same on every backend and at every
optimization setting.
The setting of Guards that keeps every guard without the effects of the calls it skips is outside this requirement.
