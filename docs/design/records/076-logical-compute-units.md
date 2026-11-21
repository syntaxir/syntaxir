# 076. Compute Units as Logical Events

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-11-21

## Summary

A compute limit protects a service from hostile input to a trusted grammar. This record proposes to count compute
units at a small set of **logical events**: the events that the language defines and an attacker can multiply. Units
are no longer counted per operation of a backend. The events are calls of rules, input that terminals advance over
(counted again after backtracking), the size of the values that actions build or compare, the suffix copies of run
sites, and the syntax errors built. Every other piece of work is bounded by these events times a constant of
the grammar, so it charges nothing.

Two things must be identical on every backend and at every optimization setting: the **total** of a parse that ends
on its own, and the **decision** whether a limit rejects the parse. The units charged at a given instant need not be.
Charging is a plain addition, often of a static count for a whole block. Checking is separate: the counter is compared
with the limit only on cycles (recursive calls and loop back-edges), before charges whose size depends on the input,
at deliveries to the caller and at the end of the parse. Work after the limit is exceeded is therefore bounded by a
stated overshoot. A `LimitError` reports `Used` exactly when the parse is rejected at its end, and within that
overshoot otherwise.

This record is a proposal. A measured prototype on generated Go comes before any change to the specification or to a
backend.

## Motivation

### The Current Contract

[Cost](../../../spec/runtime/parsing.md#cost) sums rule bodies, examined input, iterations and operators, values that
actions build and suffix copies. Each backend charges at least one unit for each call, each character that a terminal
matches or rejects, each iteration, each operator and each value, and at most a constant of the grammar *and the
backend* for each. The units therefore differ between backends.
[Reservations and Interruptions](../../../spec/runtime/parsing.md#reservations-and-interruptions) adds that every
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
  workloads. This target is to be confirmed by the prototype; see [Open Questions](#limitations-and-open-questions).

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
| Key of a call of a rule that a parse from scratch memoizes | 1 for each variable that the rule reads, plus the UTF-8 bytes of each string value among them | Long string keys hashed and compared on every call |
| Input that a terminal advances over: a literal, a character class, `.`, or a fused scan | 1 for each position unit advanced, counted again each time backtracking returns before it and a terminal advances over it again | Backtracking that re-examines input (quadratic and worse re-scans) and long scans |
| A value that an action, a predicate or a binding builds | 1 for each element of a list and each byte of a string that it builds | Values that grow faster than the input, such as a concatenation in a repetition |
| A comparison of strings by an action, a predicate, a binding or a key | The length in bytes of the shorter operand | Repeated comparison of long equal strings |
| A list operation that visits elements (`map`, `filter`, folds) | 1 for each element visited | Repeated walks of long lists |
| A suffix copy of a [run site](../../../spec/runtime/parsing.md#run-sites) | 1 for each element copied | Copies that taking a record adds, which no other event bounds |
| A syntax error built (by recovery, by missing-token insertion, or as the final error of a failed parse) | 1 | Missing-token insertion, which can build errors without consuming input |
| An entry of recovered errors taken at a store of a memo entry, a growth result or an Operators Rule attempt | 1 | The errors of reused results that missing-token insertion records one by one |
| A text value in a stream (`text` of a node) | 1 for each position unit of its result | A stream discards input, so a text is a copy |

A terminal that fails, or a lookahead that ends, advances over nothing. A later advance over the same input after
backtracking counts again; examining input without advancing counts nothing.

### Work That Is Not an Event

Each item below is bounded by the events above, times a constant of the grammar. This bound is the obligation that the
proofs update carries.

- **Rule body evaluations.** Each evaluation of a body follows a call or a growth step, which are counted. Whether a
  call evaluates its body or reads the memo is a policy of the memo and changes time, not units. The total is
  therefore independent of what the memo holds and of the store policy.
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
  counted.
- **Runtime-built values, captures, the gathering and taking off of lists, typed conversion and storage growth.** These
  already charge nothing.
- **Expectations.** Recording, merging, keeping and copying the expected items is at most a constant of the grammar for
  each terminal tested or call made. The `Expectations` option changes the content of syntax errors, not the total.
- **Scalar operations of actions.** Arithmetic, Boolean and member operations, literals and `text` outside a stream are
  constants for each evaluation of the action.
- **Preparing the input.** Validating UTF-8, indexing code points and encoding for a TypeScript host is linear in the
  input once. `Input` bounds it, and the default `Compute` is at least 1024 times the input.
- **Lines of errors.** The line table is built once per parse and is linear in the input. Finding a line is a binary
  search for each error, and errors are counted.
- **Observing a parse.** Tracing, profiles and trace selection charge nothing. They are the caller's choice, and the
  caller pays for them in time.
- **Delivery.** Read calls of a stream, deliveries to callbacks and listing the errors of the outcome charge nothing.

### Accounting

The counter is one integer for each invocation. An implementation may charge in any grouping, provided that the
total of the parse is the sum of the events in the table:

- **Straight-line code.** A block with no branch charges the static sum of its events once. Each exit of a block that
  can fail charges the static sum of the events before that exit.
- **Terminals.** A literal that matches adds its length, which is static in `Bytes` and in `CodePoints`. A class adds
  one. A fused scan adds the distance it advanced, once, after its loop.
- **Position deltas.** In a region without jumps, the input advanced by terminals equals the end position minus the
  start, plus the distance rewound. An implementation may settle the input count at rewinds and rule exits from
  positions alone. A jump that does not examine input, such as a memo hit or the take of a record, is subtracted.
- **Inlining.** An inlined rule charges the call it replaced; an inlined block charges its static count.
- **Bulk charges.** A value of n elements or bytes charges n once, before it is built, with a check (below).

### Checks and Overshoot

A **check** compares the counter with the limit and, when the context interval has passed, observes the context. The
counter is checked at these points:

1. at the entry of each call of a recursive rule (a rule on a cycle of the call graph), and at each step of a growth;
2. at the back-edge of each loop: repetitions, the loops of an Operators Rule, and recovery skips. A fused loop is
   checked once for each chunk of at most B units;
3. after each charge whose size is not a constant of the grammar (a value, a comparison, a key, a suffix copy, a stream
   text), and before the work that the charge covers;
4. before each delivery to the caller: a stream element, a commit, or a callback with recovered errors;
5. at the end of the parse, before the outcome is returned.

Between two checks, a parse follows a path that has no cycle of the grammar's control flow, so it charges at most a
constant K of the grammar and the backend, plus at most B for a chunk. When a check finds the counter above the limit,
the parse stops.

- The work done after the counter first exceeded the limit is at most K + B units.
- A charge of a large value is compared before the value is built. The work that it covers is not done, although its
  units count in `Used`.

An implementation may check more often. The specification bounds K for each backend, and the cost view reports it for
a grammar.

### Decision Rule

The **total** of a parse is the sum of its events when the parse runs to its end with no limit. A parse is rejected
with a `LimitError` of kind `LimitCompute` if and only if its total exceeds `Compute`. A parse that would fail with a
syntax error, and whose total exceeds the limit, is rejected with the `LimitError`.

The rule holds on every backend because the counter only grows:

- a check that stops the parse has seen a partial sum above the limit, so the total is above it too;
- a parse whose total is above the limit and that reaches no stopping check is stopped by the check at its end.

The decision therefore depends only on the total, which is identical on every backend and optimization setting.

### LimitError Contract

For a `LimitError` of kind `LimitCompute`:

- `Limit` is the resolved limit;
- `Used` is the counter at the check that stopped the parse, so `Limit < Used <= Limit + K + B` (or up to the last large
  charge, which item 3 checks);
- when the end-of-parse check rejects the parse, `Used` is the exact total.

`Used` of a rejected parse may differ between backends within the overshoot. A parse that completes reports its
exact total, for example through the statistics of the outcome, identical on every backend. `Requested` is not
meaningful for a compute check that covers many charges. It is removed from compute errors, and the other kinds keep
it.

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
input:

- **Fusion** keeps the input advanced and charges it in bulk.
- **Inlining** charges the call it replaces.
- **Hoisting or sinking a check** may move it only between the points listed in [Checks and
  Overshoot](#checks-and-overshoot), and must keep K within its stated bound.
- **Dispatch on the next character, guards and left factoring** may skip an alternative only if the events that the
  plain evaluation of that alternative would charge before failing are known, and the pass charges them. A skipped
  alternative that fails at its first terminal charges the calls on its left corner. Where that number depends on
  the memo or on values, the pass does not skip. The plain evaluation of the grammar is the reference.
- **A pass that removes a call** (a value-free twin replaced by a recognizer, for example) charges the calls it
  removed, or is not a pass that may run under limit controls.

A differential test runs each optimization setting against the plain evaluation and compares totals. Mutants that
drop or double one charge in each pass show that the test catches it.

### Documents

The first parse of a `Document` has the total of a parse of its text from scratch, exactly. A later parse counts the
events it performs: a reused result is one call, and its key is counted if the rule is memoized. The work of the
`Document` itself (pending edits, moves, chain steps and copies of diagnostics) is not an event. It stays bounded by
[Work of a Document Parse](../../../spec/runtime/documents.md#work-of-a-document-parse). An explicit `Compute` limit and
the default count the same events, so a reparse under the default limit succeeds whenever a parse of the same text
from scratch does. The proof target is that a reparse never counts more than the parse from scratch.

### Streams

A stream counts the same events. A text value copies its range, so it counts its length. The checks before each
delivery keep the decision and the deliveries identical on every backend. The default limit stays a function of the
input demanded so far, not of the reads.

### Disposition of the Earlier Plan

The parts of the earlier plan that have not landed are re-judged under the event table:

- **Expectation unions** (merging, keeping and copying the expected items, and the scan of the record that each merge
  does). They are not an event, because they are bounded by the terminals tested and the calls made. Tracing,
  trace selection and expectations then leave the total unchanged. A prepared change makes them free on the engine
  and the libraries; it is kept for reuse, and its tests of option independence carry over.
- **Line table for profiles.** Observing a parse is not an event, so a profile builds its line table without
  charging.
- **Line table of the first error.** It is not an event, because it is linear in the input once per parse. The
  per-error line search is not an event either; see the open question on lines.
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

1. This record is accepted, together with the answers to its open questions.
2. A prototype on generated Go: a plain counter, position-delta accounting for terminals and fused scans, and checks
   at the points listed in [Checks and Overshoot](#checks-and-overshoot). It is measured as described in
   [Performance and Results](#performance-and-results). The gate is the overhead target and an overshoot within the
   stated K + B on a sweep of limits.
3. The specification: [Cost](../../../spec/runtime/parsing.md#cost),
   [Reservations and Interruptions](../../../spec/runtime/parsing.md#reservations-and-interruptions), the `LimitError`
   paragraphs and [Work of a Document Parse](../../../spec/runtime/documents.md#work-of-a-document-parse). The
   proofs where they name the units of a backend: the linear cost proof's setting and its Document lemma, and the
   cost view's units.
4. Each backend in turn (closure, bytecode machines, generated Go, generated TypeScript), each behind the
   conformance suites and the total-agreement test.
5. The performance document and the strategy's safety claims, with the measured overhead.

## Alternatives Considered

- **Exact units at every instant (the earlier plan).** Every backend reserves the same units in the same order, so
  `Used` and `Requested` of a `LimitError` agree everywhere. It costs a check per reservation, blocks fusion and
  hoisting, and needs exact emulation of every skipped alternative. Rejected: the agreement it adds is the agreement of
  error details, which serves no goal.
- **Backend-local units, as the specification allowed before.** This is cheapest, but the decision then depends on the
  backend and the optimization setting. Rejected by the determinism goal.
- **The Cost items as events, unaudited** (body evaluations, iterations and operators as well as calls and input). This
  is closer to the proofs as written. It charges events that the table shows are bounded by other events, and the
  iteration unit forces a fused scan to count its iterations as well as its advance. Recommended against; see the
  open questions.
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
- **Documents.** The first parse equals the parse from scratch on every backend. A reparse never exceeds the parse from
  scratch on the edit fuzz corpus.
- **Streams.** Deliveries before an interruption are identical on every backend.

## Performance and Results

No result yet. The prototype of step 2 is measured as follows.

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
  - The largest observed overshoot.
  - Generated code size and binary size.
- **Method.** Go 1.27.1 on one named machine, exclusive. At least ten alternating samples per configuration, reported
  as medians with ranges, and allocations per operation. The revisions are pinned, and the raw rows are kept with the
  benchmark measurements.

## Limitations and Open Questions

These need the owner's decision before step 2. Each states a recommendation.

1. **Audit of the Cost items.** Should body evaluations, iterations and operators stop being events, because calls and
   input bound them? Recommendation: yes. It makes the total independent of the memo and lets a fused scan charge its
   advance alone. The alternative keeps them, at the price of a second count in every loop. The proofs must carry the
   bounds of [Work That Is Not an Event](#work-that-is-not-an-event) either way.
2. **Preparation of the input.** Should it stop being charged (reversing the earlier "input as a whole is n units"
   ruling), so that creating a `Document` never fails a compute limit? Recommendation: yes; `Input` bounds it.
3. **Lines of errors.** Should the line and column of a syntax error be resolved when the caller reads the error, not
   in the parse? Recommendation: yes. The parse then does no logarithmic work for each error, and the line table moves
   out of the parse, as materializing a text does. The alternative charges the logarithm of the lines for each error,
   which no event bounds.
4. **Comparison of strings.** Should a comparison charge the length of the shorter operand instead of the bytes up to
   the first difference? Recommendation: yes, for constant-time accounting.
5. **`Requested`.** Remove it from compute errors, or keep it as the size of the charge that the stopping check
   covered? Recommendation: remove; it has no meaning for a check that covers many charges.
6. **Overshoot bound.** Should K be specified per grammar (reported by the cost view) or capped by a fixed number, with
   extra checks inserted where a grammar's acyclic paths are longer? Recommendation: a fixed cap, 4096 units, with
   extra checks where needed. A caller can then rely on one number.
7. **Overhead target.** Is 1.10× of the time without controls on JSON and XML the acceptance bar for the prototype?
   Recommendation: yes as the target. If the prototype misses it, report the measured figure and decide after.
8. **Dispatch under limits.** Should a dispatch that cannot charge the skipped left-corner calls exactly be disabled
   under limit controls, or should calls that fail without advancing be made free, so that dispatch charges nothing?
   Recommendation: disable such a dispatch and measure how many are lost. Making those calls free also exempts key
   hashing of the calls, which is an attack surface.

The bound arguments in [Work That Is Not an Event](#work-that-is-not-an-event) are sketches, not proofs. The
proofs update of step 3 must state them, and must restate the linear cost and cost view proofs in units that no longer
depend on the backend.
