# 076. Compute Units as Logical Events

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-11-21

## Summary

A compute limit protects a service from hostile input to a trusted grammar. This record proposes to count compute
units at a small set of **logical events**: the events that the language defines and an attacker can multiply. Units
are no longer counted per operation of a backend. The events are calls of rules, input that terminals advance over
(counted again after backtracking), the size of the values that actions build, compare or measure, the suffix copies of run
sites, and the syntax errors built. Every other piece of work is bounded by these events times a constant of
the grammar, so it charges nothing. Body evaluations, iterations and operators were proposed as events at the start
of this design; the table drops them because calls and input bound them
([Limitations and Open Questions](#limitations-and-open-questions), decision 1).

Two things must be identical on every backend and at every optimization setting: the **total** of a parse that ends
on its own, and the **decision** whether a limit rejects the parse. The units charged at a given instant need not be.
Charging is a plain addition, often of a static count for a whole block. Checking is separate: the counter is compared
with the limit only on cycles (recursive calls and loop back-edges), before charges whose size depends on the input,
at deliveries to the caller, at each demand of a stream for more input and at the end of the parse. Every check first
settles the pending charges, so it compares the exact sum of the events performed so far. Work after the limit is
exceeded is therefore bounded by a stated overshoot. A `LimitError` reports `Used` exactly when the parse is rejected
at its end, and within that overshoot otherwise.

This record is a proposal. A measured prototype on generated Go comes before any change to the specification or to a
backend.

## Motivation

### The Current Contract

[Cost](../../../spec/runtime/parsing.md#cost) sums rule bodies, examined input, iterations and operators, values that
actions build and suffix copies. Each backend charges at least one unit for each call, each character that a terminal
matches or rejects, each iteration, each operator and each value, and at most a constant of the grammar *and the
backend* for each. The units therefore differ between backends.
[Checks and Interruptions](../../../spec/runtime/parsing.md#checks-and-interruptions) adds that every
reservation is checked before its work and keeps its place among trace events, deliveries and callbacks. It also
says that every backend must make the same charges; the current backends do not yet meet this rule.

An earlier plan made the units the Cost made exact: one table of items, each reserved one unit at a time on every
backend. Its first part has landed or is ready to land:

- storage growth charges nothing;
- values that the parse builds (terminals, sequences, lists, captures) charge nothing;
- a cut charges nothing;
- an iteration charges one unit before its element;
- a call charges one unit, its body one more, and a call of a rule that a parse from scratch memoizes charges the
  size of its key;
- tracing no longer charges a call twice.

### The Cost of Per-Operation Charging

Generated Go with finite compute limits takes 1.30–1.47× its ordinary `Parse` time, and 1.47–1.61× its ordinary
`ParseValue` time ([Performance](../../performance.md)). The interpreters take 1.54–1.79×. A charge per character
and a poll per call put a compare and a branch into every inner loop. They also pin each charge to its operation,
which blocks the optimizations that generated Go needs most:

- a character-class scan fused into one loop must still charge each character;
- a check cannot be hoisted out of a loop;
- an inlined rule keeps the poll of the call it replaced;
- a choice that dispatches on the next character must reproduce the units of the alternatives it skips, or not skip
  them.

### What the Units Are For

Three goals pull in different directions:

- **Effective.** A hostile input must stop within a stated bound of work. This is why most users turn limits on.
- **Deterministic.** The same grammar, options and input must give the same accept or reject decision on every backend
  and at every optimization setting. Otherwise an optimization flag changes which inputs a service accepts, and a
  TypeScript frontend cannot predict what a Go backend rejects.
- **Cheap and transparent to optimization.** Charging must cost little, and passes must stay free to fuse, hoist and
  inline.

Agreement at every instant serves none of these goals. Effectiveness needs a bound on the work done after the limit,
and determinism needs equal totals and decisions.

## Goals

- Units count only events whose number a hostile input can raise beyond a constant of the grammar times the other
  events. Each event in the table names the attack it defends against.
- On every backend (closure, both bytecode machines, generated Go, typed generated Go, generated TypeScript) and at
  every optimization setting, a parse that ends on its own has the same total. A finite `Compute` limit gives the same
  decision.
- The options that observe a parse (tracing, profiles, expectations, trace selection) and the conversion to typed
  values do not change the total.
- After the total exceeds the limit, a parse does at most a stated amount of further work. The specification gives
  the bound.
- Charging is a counter addition, with static counts summed per block. Checks happen only where the specification
  places them.
- Measured: generated Go with finite limits takes at most 1.10× its time without limit controls on the JSON and XML
  workloads. This is the acceptance target of the prototype
  ([Limitations and Open Questions](#limitations-and-open-questions), decision 6).

## Non-Goals

- The `Memory`, `Input` and `Depth` measures keep their contracts
  ([Default Limits](../../../spec/runtime/parsing.md#default-limits)). Memory stays a separate count of bytes.
- Elapsed time is not bounded. Units bound work up to a constant of the grammar, and grammars are trusted.
- A `LimitError` of a rejected parse need not report the same `Used` on every backend.
- The work that only a `Document` does (pending edits, moves, chains) remains bounded by the Document proof and is not
  counted as events.

## Design

### Threat Model

The grammar, its options and the backend are trusted. The input is hostile, and so is the order and content of the
edits to a `Document`. An attack is an input on which the work of the parse grows faster than the caller expects. Two
kinds matter. The first is super-linear work: backtracking that re-examines input, and calls that memoization does
not share. The second is work that is linear but large: long values built by actions, and long keys hashed on every
call. A unit that bounds neither kind defends against nothing and only costs time.

### Events

| Event | Units | Attack it defends against |
| --- | --- | --- |
| Call of a rule, including a call answered from the memo, a call of a value-free twin, the entry call and a step of a growing left-recursive rule | 1 | Calls that memoization does not share: exponential backtracking in rules without a memo, call storms at one position, and growth of left recursion |
| Key of a call of a rule that a parse from scratch memoizes, and the key of a walk of a [run site](../../../spec/runtime/parsing.md#run-sites) keyed by variables (see [Keys of Walks](#keys-of-walks)) | 1 for each variable of the key, plus the UTF-8 bytes of each string value among them | Long string keys hashed and compared on every call or lookup |
| Input that a terminal advances over: a literal, a character class, `.`, or a fused scan, including inside a lookahead and inside an alternative that later fails | 1 for each position unit advanced, counted again each time backtracking returns before it and a terminal advances over it again | Backtracking that re-examines input (quadratic and worse re-scans) and long scans |
| A value that an action, a predicate or a binding builds | 1 for each element of a list and each byte of a string that it builds | Values that grow faster than the input, such as a concatenation in a repetition |
| A comparison of strings by an action, a predicate, a binding or a key | The length in bytes of the shorter operand | Repeated comparison of long equal strings |
| `len` of a string, in either position unit | The length in bytes of the string | Repeated measures of long strings, whose length in code points, or in bytes from another encoding, takes a walk |
| A list operation that visits elements (`map`, `filter`, folds) | 1 for each element visited | Repeated walks of long lists |
| A suffix copy of a [run site](../../../spec/runtime/parsing.md#run-sites) | 1 for each element copied | Copies that taking a record adds, which no other event bounds |
| A syntax error built (by recovery, by missing-token insertion, or as the final error of a failed parse) | 1 | Missing-token insertion, which can build errors without consuming input |
| An entry of recovered errors taken at a store of a memo entry, a growth result or an Operators Rule attempt | 1 | The errors of reused results that missing-token insertion records one by one |
| A text value in a stream (`text` of a node) | 1 for each position unit of its result | A stream discards input, so a text is a copy |

The advances of terminals inside a lookahead, and inside an alternative that fails after them, count when they
happen; the rewind at the end of the lookahead or at the failure adds and removes nothing. A terminal that fails
advances over nothing and counts nothing. A later advance over the same input after backtracking counts again;
examining input without advancing counts nothing.

Body evaluations, iterations and operators are not in the table, although the direction that opened this design
listed them as events. [Work That Is Not an Event](#work-that-is-not-an-event) gives the bounds that justify the
omission, which is decision 1.

#### Keys of Walks

A walk of a run site keyed by variables looks up a record at each position it visits, with the values that the
variables of the key hold there. The predicates of the element may bind variables of the key, so those values can
change between the start of the walk and a later position. The charge follows the site, which is static:

- a site whose element binds no variable of the key keeps the values of the key for the
  whole walk. Its key is charged once, when the walk starts; the walk hashes it at most once and reuses that hash at
  every lookup;
- a site whose element binds a variable of the key charges its key at each lookup, because each lookup hashes and
  compares values that may differ from the last.

A hit ends the walk, so a walk makes at most one full comparison of its key with a record, apart from
hash collisions; the lookups that miss compare hashes. The charge stays a property of the grammar, so every backend
agrees on it.

This changes the Go library, whose skip runs read, hash and charge the key at every hit and every visit today,
whatever the site. The prototype of step 2 implements the change.

### Work That Is Not an Event

Each item below is bounded by the events above, times a constant of the grammar. This bound is the obligation that the
proofs update carries.

- **Rule body evaluations.** Each evaluation of a body follows a call or a growth step, which are counted. Whether a
  call evaluates its body or reads the memo still changes the total: an evaluation counts the events of the body,
  and a hit counts only the call and its key. The total is therefore defined by the plain evaluation together with
  the store policy of the memo, which becomes normative (see [Memo Policy](#memo-policy)).
- **Iterations of repetitions.** Every iteration but the last of each repetition advances the position. It does so by
  a terminal or a call, which are counted, or by the take of a record, which ends the walk. The last
  iteration happens once for each time the repetition is entered. Entries are bounded by the evaluations and
  iterations of the enclosing expression, whose nesting is a constant of the grammar.
- **Operators of an Operators Rule.** They are tried and applied at most a constant of the grammar times for each
  operand. An operator applied without consuming input ends its expression.
- **Retries of alternatives, entries of lookaheads, cuts and `#atomic` frames.** Each is at most a constant of the
  grammar for each body evaluation or iteration. The input that a retried alternative advances over again is counted.
- **Lookups and records of skip runs, run sites and scan sites.** There is at most one lookup for each iteration and
  one take for each repetition entry. The iterations and characters that a take skips are not done, so they are not
  counted. The key of a keyed walk is the one exception, an event of its own ([Keys of Walks](#keys-of-walks)),
  because its size depends on string values.
- **Runtime-built values, captures, the gathering and taking off of lists, typed conversion and storage growth.** These
  already charge nothing.
- **Expectations.** Recording, merging, keeping and copying the expected items is at most a constant of the grammar for
  each terminal tested or call made. The `Expectations` option changes the content of syntax errors, not the total.
- **Scalar operations of actions.** Arithmetic, Boolean and member operations, literals and `text` outside a stream are
  constants for each evaluation of the action.
- **Preparing the input.** Validating UTF-8, indexing code points and encoding for a TypeScript host is linear in the
  input once. `Input` bounds it, and the default `Compute` is at least 1024 times the input.
- **Lines of errors.** The parse does not resolve lines and columns. They are resolved when the caller reads an
  error, outside the parse (decision 3).
- **Observing a parse.** Tracing, profiles and trace selection charge nothing. They are the caller's choice, and the
  caller pays for them in time.
- **Delivery.** Read calls of a stream, deliveries to callbacks and listing the errors of the outcome charge nothing.

### Memo Policy

A memo hit and an evaluation count different events, so the total depends on which calls the memo answers. The
plain evaluation that defines the total includes the store policy of the reference implementation, which this record
makes normative on every backend:

- **Which rules defer.** A first call may defer its memoization only for a rule that is memoized, not transient
  and not the leader of a cycle of left recursion; the program package decides this for every backend. A leader
  never defers.
- **Which parses defer.** A parse defers unless it is a stream or a `Document` parse, a static choice of the entry
  point. A parse that is observed by tracing, profiles or trace selection defers like one that is not.
- **The first call.** The first call of a deferring rule at a position is evaluated and its result is not stored; the
  call only marks the position as seen for the rule, whatever the values of its variables. The mark is made before
  the call enters a context of growths, so a rule on a cycle that is not its leader defers inside the contexts of
  growths too.
- **Later calls.** A later call at a seen position is evaluated and stored, and later calls that the stored entry
  answers are hits. Whether an entry answers a call depends on more than the position: the binding level `min`, the
  values of the variables that the rule reads, whether the entry was stored in a silent context, and, inside a context
  of growths, whether the context allows the entry. Step 3 states these conditions exactly in the specification,
  as they stand in [Memoization](../../../spec/runtime/parsing.md#memoization) and [Evaluations of
  Rules](../../../spec/runtime/parsing.md#evaluations-of-rules).
- **The switch.** Each rule keeps a count of its calls and of its repeated calls (calls at a seen position). Once the
  repeated calls exceed a sixteenth of the calls, the rule stores every first call from then on, for the rest of the
  parse. Both counts are halved together before the count of calls would reach 2^31.

This is the first-call policy of the engine's state package, the Go library and the TypeScript library. The
store-policy conformance tests hold each backend only between a memo that stores every first call
and one that defers each;
step 3 states the policy exactly in the specification and the total-agreement test holds every backend to it. Totals
agree because the policy is a deterministic function of the sequence of calls, which the plain evaluation fixes.

Any pass that changes what the memo sees must preserve the policy:

- inlining a memoized rule is forbidden under limit controls, unless the inlined code reproduces the hit or miss, the
  seen mark, the store and the counts of the call it replaces;
- a pass that skips, moves or duplicates a call of a memoized rule must leave the same seen marks, stores and counts
  as the plain evaluation, or not run under limit controls.

A `Document` adds the reuse of results of earlier parses, which is normative in the same way (see
[Documents](#documents)).

### Accounting

The counter is one integer for each invocation. An implementation may charge in any grouping, under two rules:

- **Exact at checks.** Every check first settles what is pending: position deltas not yet added, and the static count
  of the events of the current block performed before the check, which is known for each check site. The counter it
  compares is then exactly the sum of the events performed so far, on every backend.
- **Never ahead of the event.** No charge counts an event before it happens. A block charges its static sum at its
  exits, not at its entry; a fused scan charges a chunk after scanning it. The one exception is a sized operation
  (below), whose event begins when its size is known.

Within these rules:

- **Straight-line code.** A block with no branch charges the static sum of its events once, at its end. Each exit of a
  block that can fail charges the static sum of the events before that exit.
- **Terminals.** A literal that matches adds its length, which is static in `Bytes` and in `CodePoints`. A class adds
  one. A fused scan adds the distance it advanced, after its loop or after each chunk.
- **Position deltas.** In a region without jumps, the input advanced by terminals equals the end position minus the
  start, plus the distance rewound. An implementation may settle the input count at rewinds and rule exits from
  positions alone. A jump that does not examine input, such as a memo hit or the take of a record, is subtracted.
- **Inlining.** An inlined rule charges the call it replaced; an inlined block charges its static count.
- **Sized operations.** A value of n elements or bytes, a string comparison, a key or a suffix copy charges n once,
  when n is known and before the work, with a check (below). The operation has begun when it charges, and it
  runs unless that check or another limit stops the parse.

### Checks and Overshoot

A **check** settles the pending charges ([Accounting](#accounting)), compares the counter with the limit and, when
the context interval has passed, observes the context. The counter is checked at these points:

1. at the entry of each call of a recursive rule (a rule on a cycle of the call graph), and at each step of a growth;
2. at the back-edge of each loop: repetitions, the loops of an Operators Rule, and recovery skips. A fused loop is
   checked once for each chunk of at most B units, after the chunk is scanned and charged;
3. after each sized charge (a value, a comparison, a key, a suffix copy, a stream text), and before the work that the
   charge covers;
4. before each delivery to the caller: a stream element, a commit, or a callback with recovered errors;
5. in a stream under the default limit, before each demand (an increase of `d`, the furthest unit that the plain
   evaluation has examined), or merged as [Streams](#streams) proves;
6. at the end of the parse, before the outcome is returned.

Between two checks, a parse follows a path that has no cycle of the grammar's control flow. The compiler inserts
further checks on long acyclic paths, splitting blocks, so that the static charges between two checks stay within a
cap. An atomic static charge, the length of a single literal, is not split. The bound is therefore

> K = max(4096, A), where A is the largest atomic static charge of the grammar,

plus at most B for a chunk of a fused loop. Splitting a literal into chunks with checks between them would remove A
from the bound, but it costs code in the hottest terminal for literals that grammars do not have: A is the length of
the longest literal of a trusted grammar, a constant that the cost view reports. When a check finds the counter above
the limit, the parse stops.

- The units charged after the counter first exceeded the limit are at most K + B.
- A sized charge is compared before its work. The work that it covers is not done, although its units count in
  `Used`.
- The work that charges nothing is at most a constant of the grammar for each counted event ([Work That Is Not an
  Event](#work-that-is-not-an-event)). The uncharged work done after the limit was exceeded is therefore at most
  (K + B) times a constant of the grammar, besides the work of the `Document` itself, which
  [Documents](#documents) bounds separately.

An implementation may check more often; [Decision Rule](#decision-rule) shows that extra checks cannot change the
decision.

### Decision Rule

The **total** of a parse is the sum of its events when the parse runs to its end with no limit. A parse is rejected
with a `LimitError` of kind `LimitCompute` if and only if its total exceeds `Compute`. A parse that would fail with a
syntax error, and whose total exceeds the limit, is rejected with the `LimitError`.

The rule holds on every backend because the counter only grows:

- a check that stops the parse has seen a partial sum above the limit, so the total is above it too;
- a parse whose total is above the limit and that reaches no stopping check is stopped by the check at its end.

The decision therefore depends only on the total, which is identical on every backend and optimization setting.

**A limit that moves.** The default limit of a stream is a function of the input demanded so far, so it rises at
each demand ([Streams](#streams) defines demands).
Let the demands be d1, d2, ..., dm, let L0 be the limit before d1 and Li the limit between di and
d(i+1), and let Si be the sum of the events that the plain evaluation performs before demand d(i+1) (Sm is the total).
The rule is then: a stream is rejected if and only if Si > Li for some i. Check 5 compares exactly Si with Li, because
every check settles first and no charge runs ahead of its event; check 6 compares Sm with Lm. Each Si is the same on
every backend, provided that the events before each demand are the same, which the obligations of passes ensure.

Extra checks cannot change this decision. A check at an instant between di and d(i+1) compares a partial sum with Li.
The partial sum is at most Si, because the counter only grows and every event before the instant comes before
d(i+1). So a check there rejects only if the check at d(i+1) would reject, and a parse stopped early has the same
decision, reached sooner. The same argument with one interval gives the fixed-limit rule above: every check, wherever
it runs, compares a partial sum of the total with the same limit.

### LimitError Contract

For a `LimitError` of kind `LimitCompute`:

- `Limit` is the resolved limit;
- `Used` is the counter at the check that stopped the parse, so `Limit < Used <= Limit + K + B`, or up to the last
  sized charge, which check 3 compares before its work;
- when the end-of-parse check rejects the parse, `Used` is the exact total;
- `Requested` is absent, as decided on 2026-11-20 for the exact unit table: compute errors no longer carry it, and the
  other kinds keep it. A check that covers many charges gives it no meaning either.

The same decision of 2026-11-20 made units atomic, so that `Used` equalled `Limit` when a parse stopped. This record
changes that part: `Used` may exceed `Limit` by the overshoot, and `Used` of a rejected parse may differ between
backends within it. A parse that completes reports its exact total, for example through the statistics of the
outcome, identical on every backend.

A cancellation is observed at checks once at least P units have been charged since the last observation (P is 256
today). The latency of a cancellation is therefore at most P + K + B units. When a check finds both a context error and
an exceeded limit, the context error wins, as it does today.

The rule that every reservation keeps its place among trace events, deliveries and callbacks changes as follows:

- deliveries to the caller stay ordered by item 4. An interruption exposes exactly the deliveries before the check
  that stopped the parse, and every backend has that check before the same delivery;
- trace events may run up to K + B units past the point where the counter exceeded the limit, and the trace of a
  rejected parse may differ between backends in that span. A trace of a parse that completes is unchanged.

### Obligations of Optimization Passes

A pass of the [pass pipeline](../pass-pipeline.md) that changes the code of a rule must keep the events of every
input, in an order that keeps the sum before each check point and each demand of a stream:

- **Fusion** keeps the input advanced and charges it in bulk, after the scan.
- **Inlining** charges the call it replaces. A memoized rule is not inlined under limit controls unless the inlined
  code reproduces the memo behaviour of the call ([Memo Policy](#memo-policy)).
- **Hoisting or sinking a check** may move it only between the points listed in [Checks and
  Overshoot](#checks-and-overshoot), and must keep K within its stated bound. It may not move a demand of a stream
  before events that the plain evaluation performs before that demand.
- **Dispatch on the next character, guards and left factoring** keep a guard under limit controls exactly when
  skipping the alternative leaves the same observable state as evaluating it: the same unit total, the same limit
  decision and the same memo state (the seen marks, policy counts and stored entries that later calls read, as in
  [Memo Policy](#memo-policy)). Any other guard is disabled, and the alternative is tried. The plain evaluation of the
  grammar is the reference. A pass establishes the equivalence statically, for alternatives that fail at a terminal
  without advancing after a fixed chain of calls on every character the guard rejects:
  - the left corner of the alternative, followed through first items, captures and the bodies of the rules it calls,
    is a chain of calls that ends in a non-empty literal or class, with no choice, predicate, action, binding,
    repetition, recovery or inlined call before it, so every character the guard rejects makes the same calls in
    the same order and fails at the same terminal;
  - no rule of the chain is the leader of a cycle of left recursion or a rule of operators, and no call names a
    binding level.

  The skip makes the events of the chain on the path that skips the alternative and only there. It walks the calls
  outermost first, as the evaluation enters them. Each call counts one unit and checks the counter where the
  evaluation checks it. A call of a rule that the parse memoizes counts its key and makes the first call's seen mark
  and policy counts. If its memoization is deferred, the walk goes on to the next call. Otherwise it looks the call
  up: an entry that answers the call ends the walk, with the entry's expectations; any other call goes on, and on
  the way back the walk stores the failure entry of the call, with the expectations of the terminal and the
  silence of the context, innermost first, as the evaluation stores them. A chain without a memoized rule is a static
  count of units. Whether a rule is memoized depends on the parse. A transient rule (one that only a `Document`
  memoizes) goes straight to its body outside a `Document`, so its call there has no memo effect and its guards are
  kept. A `Document` parse does not count logical units until its reuse policy is specified
  ([Documents](#documents)).
- **A pass that removes a call** (a value-free twin replaced by a recognizer, for example) charges the calls it
  removed and keeps their memo effects, or is not a pass that may run under limit controls.

A differential test runs each optimization setting against the plain evaluation and compares totals. Mutants that
drop or double one charge in each pass show that the test catches it.

### Documents

The first parse of a `Document` has the total of a parse of its text from scratch, exactly. A later parse counts the
events it performs: a reused result is one call, and its key is counted if the rule is memoized. The work of the
`Document` itself (pending edits, moves, chain steps and copies of diagnostics) is not an event. It stays bounded by
[Work of a Document Parse](../../../spec/runtime/documents.md#work-of-a-document-parse), and no `Compute` limit counts it,
on any backend, except that the generated TypeScript runtime still counts it under an explicit limit until it leaves that work out. An explicit `Compute` limit and
the default count the same events, so a reparse under the default limit succeeds whenever a parse of the same text
from scratch does. The proof target is that a reparse never counts more than the parse from scratch.

What a reparse reuses decides what it counts: a reused result counts one call and its key, and a result that is not
reused counts its evaluation. The reuse policy of a `Document` (which results of earlier parses it keeps, which it
retires and which a lookup finds) is therefore normative on every backend, like the store policy of the memo. Step 3
states it in [Documents](../../../spec/runtime/documents.md), and the total-agreement test covers reparses.

A `Document` memoizes transient rules under that reuse policy, so a skip in a `Document` parse must also make the
entries and reuse effects of the transient calls of its chain, as the evaluation makes them. Until that policy is
specified, a parser with logical units is not built for `Document` parses, and its guards need no `Document` form.

### Streams

A stream counts the same events. A text value copies its range, so it counts its length. The checks before each
delivery keep the decision and the deliveries identical on every backend.

The default limit of a stream is `L(d) = max(2^20, 1024 * d)`, where `d` is the number of units up to the furthest
position that the parser has demanded so far
([Default Limits](../../../spec/runtime/parsing.md#default-limits)). It never shrinks.

**Demands.** A demand is an increase of `d`: the plain evaluation examines a unit beyond the furthest it has examined.
It is not a call of the reader. A backend that reads ahead of the plain evaluation, by a bulk compare of a literal, a
SIMD scan or a read of the reader into a buffer, does not raise `d` by doing so; `d` rises only as the plain
evaluation would examine the units. `d` therefore depends on the grammar and the input alone, as the specification
already requires.

**The rule.** Before `d` rises, the parse settles the pending charges and checks them against the current limit: with
Si the events before the demand and Li the limit before it, the stream is rejected if and only if Si > Li for some
demand, or the total exceeds the final limit ([Decision Rule](#decision-rule)). Every read site that can raise `d` is
therefore a check site (check 5). Without these checks, a parse could exceed the limit of the input it had and then
pass once more input arrived, so the decision would depend on where a backend placed its checks. An explicit, fixed
`Compute` limit needs no demand check, though one does no harm.

**Merged checks.** An implementation may merge the checks of the demands inside one chunk, a literal compare or a
fused scan, into checks after the chunk, under this proof that the decision cannot change. Inside such a chunk the
only events are the units that it advances over, so between two consecutive demands of the chunk the events grow by
at most 1. Number the demands of the chunk j = 1, ..., m; the j-th examines unit d0 + j, where d0 is `d` before the
chunk. Let Sj be the events before demand j, and Lj = L(d0 + j - 1) the limit before it. Then:

- in the region where the limit is flat (d0 + j - 1 <= 1024, so Lj = 2^20), Sj - Lj never decreases from one demand to
  the next, because Sj grows by at most 1 and Lj stays;
- past it (d0 + j - 1 >= 1025), Lj grows by at least 1024 from one demand to the next (from L(1024) = 2^20 to
  L(1025) = 2^20 + 1024 at the boundary, and by 1024 after), while Sj grows by at most 1, so Sj - Lj decreases.

So the largest Sj - Lj of the chunk is at j = 1 or at j* = min(m, 1025 - d0), the last demand in the flat region
(there is none when d0 >= 1025). A merged check that compares S1 with L1 and S(j*) with 2^20 rejects exactly when
one of the per-demand checks would. Sj is the events before the chunk plus the units advanced before unit d0 + j
was examined, which the chunk knows when it ends. The floor is the case that a single check after the chunk would
get wrong: a crossing in the middle of a chunk at d0 + j - 1 <= 1024, with `d` passing 1024 later in the same chunk,
raises the limit by 1024 per unit and would hide the rejection from a check that compares the end of the chunk with
the end limit. The comparison at j* keeps it. A merged check runs after its chunk, so the parse may scan up to the
chunk past the demand that rejects it; that is within the overshoot B.

The chunk-size sweep of [Testing](#testing) covers the floor: chunks that start below 1024 units and end above it,
with totals near 2^20.

### Disposition of the Earlier Plan

The parts of the earlier plan that have not landed are re-judged under the event table:

- **Expectation unions** (merging, keeping and copying the expected items, and the scan of the record that each merge
  does). They are not an event, because they are bounded by the terminals tested and the calls made. Tracing,
  trace selection and expectations then leave the total unchanged. A prepared change makes them free on the engine
  and the libraries; it is kept for reuse, and its tests of option independence carry over.
- **Line table for profiles.** Observing a parse is not an event, so a profile builds its line table without
  charging.
- **Line table of the first error.** It leaves the parse: lines and columns are resolved when the caller reads the
  error (decision 3), so neither the table nor the search for a line is part of a parse.
- **Preparation of the input in `CodePoints`.** This is the earlier "input as a whole" item, and it is no longer an
  event. Construction of a `Document` (`Parser.NewDocument`) does no counted work. It then cannot fail with a compute
  `LimitError` before the first parse, which it can today under a small explicit limit. The `Input` limit is the
  control for the size of the input.
- **The first parse of a `Document` on the closure backend and on the bytecode machines.** These disagree today; on
  one measured case the bytecode machines charge more, by the length of the input. The event table defines that parse
  as the parse from scratch, so the disagreement is a defect for the conformance suites to remove. Its cause is not
  established.
- **Comparison of strings in a binding.** The earlier ruling charged one unit plus the bytes compared up to the first
  difference. Under the event table, a comparison charges the length of the shorter operand. That number is known
  from the lengths in constant time, needs no comparison loop that reports where the strings differ, and bounds the
  work. It overcharges strings that differ early, which no attack exploits.
- **Units that have landed but are not events.** These are the unit of a body evaluation, the unit of an iteration
  and the units of operators. Each is a constant of the grammar for each counted event. They are removed when the
  backends switch, behind the conformance suites. Removing them changes no decision rule.

### Implementation Sequence

1. This record is accepted, with the decisions in [Limitations and Open Questions](#limitations-and-open-questions).
2. A prototype on generated Go: a plain counter, position-delta accounting for terminals and fused scans, and checks
   at the points listed in [Checks and Overshoot](#checks-and-overshoot). It is measured as described in
   [Performance and Results](#performance-and-results). The gate is the overhead target and an overshoot within the
   stated K + B on a sweep of limits.
3. The specification: [Cost](../../../spec/runtime/parsing.md#cost),
   [Checks and Interruptions](../../../spec/runtime/parsing.md#checks-and-interruptions), the `LimitError`
   paragraphs, the store policy of the memo ([Memo Policy](#memo-policy)), the reuse policy of a `Document` and
   [Work of a Document Parse](../../../spec/runtime/documents.md#work-of-a-document-parse). The
   proofs where they name the units of a backend: the linear cost proof's setting and its Document lemma, and the
   cost view's units.
4. Each backend in turn (closure, bytecode machines, generated Go, generated TypeScript), each behind the
   conformance suites and the total-agreement test. Generated Go counts logical units first for parses from scratch,
   with guards kept by the equivalence of
   [Obligations of Optimization Passes](#obligations-of-optimization-passes). `Document` parses follow once their
   reuse policy is specified, with the `Document` tests of [Testing](#testing).
5. The performance document and the strategy's safety claims, with the measured overhead.

## Alternatives Considered

- **Exact units at every instant (the earlier plan).** Every backend reserves the same units in the same order, so
  `Used` of a `LimitError` agrees everywhere. It costs a check per reservation, blocks fusion and
  hoisting, and needs exact emulation of every skipped alternative. Rejected: the agreement it adds is the agreement of
  error details, which serves no goal.
- **Backend-local units, as the specification allowed before.** This is cheapest, but the decision then depends on the
  backend and the optimization setting. Rejected by the determinism goal.
- **The Cost items as events, unaudited** (body evaluations, iterations and operators as well as calls and input). This
  is closer to the proofs as written. It charges events that the table shows are bounded by other events, and the
  iteration unit forces a fused scan to count its iterations as well as its advance. Rejected by decision 1.
- **Checks at fixed unit intervals (every 256 units) instead of at structural points.** This is simple, but every
  charge site must then compare. Structural checks let straight-line code and fused loops run without a compare.
- **Time-based limits.** Not deterministic, so rejected.

## Testing

- **Total agreement.** For the corpora of the conformance suites and the benchmark grammars, the total of each parse
  is the same on every backend and optimization setting, in both position units, with and without tracing, profiles
  and expectations.
- **Decision agreement.** A sweep of limits around each total: every backend accepts at the total and rejects one
  below it.
- **Overshoot.** On the same sweep, `Used - Limit` stays within the stated bound for each backend. Hostile inputs
  (exponential backtracking without a memo, deep left recursion, long concatenations, long keys, missing-token
  insertion) are rejected within the bound.
- **Passes.** Mutants that drop or double one charge in fusion, inlining, dispatch and check hoisting fail the
  differential test.
- **Kept guards.** Each parse's total and memo state (seen marks, policy counts and the entries in chain order) with
  the guards kept equal those of an evaluation that tries every guarded alternative. The grammars include one whose
  skipped calls of a memoized rule decide when that rule switches to storing every first call, one whose chains pass
  memoized rules that call before their terminal, with an entry that answers mid-chain and stores innermost first,
  and one whose memoized rule reads a variable. Mutants that leave out a call's unit, key, seen mark, policy counts
  or stored entry, store outermost first, use the wrong key or let an answering entry continue the walk fail it.
  The same holds for transient rules outside a `Document`. Once `Document` parses count logical units, a `Document`
  reparse of such a grammar keeps the total of the parse from scratch, and a mutant that skips without the entries
  of transient calls fails it.
- **Documents.** The first parse equals the parse from scratch on every backend. A reparse never exceeds the parse from
  scratch on the edit fuzz corpus.
- **Streams.** Deliveries before an interruption are identical on every backend. Under the default limit, a sweep of
  input chunk sizes gives the same decision, and a mutant that drops the demand check fails it. The sweep includes
  literals and fused scans that cross 1024 units with totals near 2^20, and a mutant that merges the checks of such a
  chunk into one check at its end fails it.
- **Memo and reuse policy.** The evaluations and hits of each rule are the same on every backend, for parses and for
  `Document` reparses, and a mutant of the store policy (deferring never, or always) fails the total-agreement test.

## Performance and Results

The prototype of step 2 was measured as follows; the results follow the method.

- **Workloads.** JSON and XML of the benchmark suite. The wedge workloads: a calculator, a filter language and JSON,
  each in its idiomatic grammar, on short (under 1 KiB) and long (over 100 KiB) inputs. Hostile inputs for the
  overshoot.
- **Configurations.** Generated Go `Parse` and `ParseValue`, in `Bytes` and `CodePoints`:
  - generated without limit controls;
  - with limit controls and limits `Unlimited`;
  - with a finite `Compute` (today's charging, and the prototype);
  - with a cancelable context only.
- **Comparisons.**
  - Finite limits against no limit controls, today against the prototype.
  - The prototype's unlimited mode against no controls, to show the cost of carrying the counter.
  - Units per input byte on each workload, to set the factor of the default limit.
  - The largest observed overshoot, against K + B.
  - Dispatches of the benchmark grammars, by class: kept under limits with a static charge (a fixed chain of
    non-memoized, non-keyed rules failing at its first terminal) and disabled, with the time that disabling costs.
  - Generated code size and binary size.
- **Method.** Go 1.27.1 on one named machine, exclusive. At least ten alternating samples per configuration, reported
  as medians with ranges, and allocations per operation. The revisions are pinned, and the raw rows are kept with the
  benchmark measurements.

Results on an Apple M3 Max with Go 1.27.1, from ten alternating rounds, each started once the one-minute load
average was 8 or less. The parser without limit controls was run twice per round; the median ratio of the two runs
was 0.993 to 1.009 in every round. Each figure is the median, over the rounds, of the time relative to the parser
generated without limit controls in the same round:

| Workload | Configuration | `Parse`, `Bytes` | `Parse`, `CodePoints` | `ParseValue`, `Bytes` | `ParseValue`, `CodePoints` |
|:--|:--|--:|--:|--:|--:|
| JSON | today's charging, finite limit | 1.279 | 1.208 | 1.296 | 1.270 |
| JSON | logical units, guards through transient rules disabled | 1.250 | 1.202 | 1.151 | 1.113 |
| JSON | logical units, guards through transient rules kept | 1.105 | 1.096 | 1.045 | 1.058 |
| JSON | every guard kept and uncounted (a bound, not exact) | 1.078 | 1.078 | 1.021 | 1.031 |
| XML | today's charging, finite limit | 1.327 | 1.315 | 1.390 | 1.390 |
| XML | logical units, guards through transient rules disabled | 1.070 | 1.064 | 1.056 | 1.048 |
| XML | logical units, guards through transient rules kept | 1.060 | 1.061 | 1.046 | 1.045 |
| XML | every guard kept and uncounted (a bound, not exact) | 1.062 | 1.058 | 1.033 | 1.030 |

- **Overhead.** XML meets the target with guards through transient rules disabled, and keeping them changes it
  little. JSON misses it by up to 0.15 with them disabled: its value dispatch tries objects, arrays and null literals,
  all transient rules, before the alternative that matches. With them kept, JSON is within the target except
  `Parse` in `Bytes`, at 1.105.
- **Units.** About 2.4 units per byte of the JSON input and 2.1 of the XML input, the same for `Parse` and
  `ParseValue`. With guards kept by the equivalence, with or without those through transient rules, the totals equal
  those of an evaluation that tries every guarded alternative, on valid, truncated and damaged inputs.
- **Overshoot.** A sweep of limits accepts every input at its total and rejects it one unit below; the largest
  overshoot was 21 units, against K + B = 4096.
- **Dispatch.** Of 22 guard sites in the JSON parser, 14 are kept and 8 disabled when guards through transient rules
  are disabled; of 136 in the XML parser, 102 and 34. With guards through transient rules kept outside a `Document`,
  every site is kept in both.
- **Size.** The code of the generated JSON parser is 51 KB without limit controls and 51 KB with logical units, 53
  KB with guards through transient rules kept; the XML parser's is 289, 298 and 304 KB. Today's limit-controlled form
  is about a fifth larger than the form without limit controls on both.
- **Not measured.** The wedge workloads, hostile inputs, streams, and the failure entries of skipped memoized calls:
  the prototype's skip made the seen marks and policy counts of those calls but stored no entry. Limits `Unlimited`
  and a cancelable context ran within about 0.03 of a finite limit in an earlier round set.

## Limitations and Open Questions

The questions that this record raised are decided as follows, each with its reason.

1. **Body evaluations, iterations and operators are not events** (decided 2026-11-21). Calls and the input advanced
   bound them by a constant of the grammar, so counting them defends against no further attack. Dropping them lets a
   fused scan charge its advance alone and a loop carry no second count. The total still depends on the memo, through
   the normative store policy. The proofs must carry the bounds of [Work That Is Not an
   Event](#work-that-is-not-an-event).
2. **Preparing the input charges nothing** (decided 2026-11-21). This reverses the earlier "input as a whole is n units"
   ruling. The work is linear in the input once, and `Input` bounds it, so creating a `Document` never fails a compute
   limit.
3. **Lines and columns of syntax errors are resolved lazily** (decided 2026-11-21), when the caller reads the error,
   not in the parse. The parse then does no logarithmic work for each error, which no event would bound, and the line
   table moves out of the parse, as materializing a text does.
4. **A string comparison charges the length of the shorter operand**, not the bytes up to the first difference. The
   length is known in constant time and bounds the work; overcharging strings that differ early helps no attack.
5. **The overshoot bound is K = max(4096, A)**, where A is the longest literal of the grammar, with checks inserted on
   longer acyclic paths and literals left whole. A caller relies on one number, 4096, except for grammars with longer
   literals, which the cost view reports; splitting literals would cost code in the hottest terminal for a case that
   grammars do not have.
6. **The overhead target of the prototype is 1.10×** the time without limit controls on JSON and XML. If the prototype
   misses it, the measured figure is reported and the design is reconsidered with it. The prototype met it on XML. On
   JSON it measured 1.113 to 1.250 with guards through transient rules disabled, and 1.045 to 1.105 with them kept
   outside a `Document` ([Performance and Results](#performance-and-results)).
7. **A dispatch guard is kept exactly when skipping is equivalent to evaluating** (decided 2026-11-21): the same unit
   total, limit decision and memo state. The equivalence decides, not a kind of rule: a transient rule disables a
   guard only where its call has memo effects, in a `Document` parse.

One question was settled before this record. `Requested` is absent from compute errors, as decided on 2026-11-20;
this record changes only the `Used = Limit` part of that decision ([LimitError Contract](#limiterror-contract)).

One question stays open: whether a `Document` parse can keep the guards through the rules it memoizes, by writing on
the skip the same failure entry that the call would store under the reuse policy. A study of it must show that a
reparse then reuses the same results and keeps the total of the parse from scratch.

The bound arguments in [Work That Is Not an Event](#work-that-is-not-an-event) are sketches, not proofs. The
proofs update of step 3 must state them, and must restate the linear cost and cost view proofs in units that no longer
depend on the backend.
