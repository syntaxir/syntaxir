# Left-Recursion Member Memoization

<!-- former-mechanism R170: The specification defines how the memo answers and stores the calls of the rules of a left-recursion cycle in the context of growths, on every backend and in Documents -->

The rules of a left-recursion cycle other than its leader are memoized, so a member reached from several call sites at
one position is evaluated once instead of once for each call.

## Design and Invariants

The result of a call of a rule of a cycle is a function of the growths of its cycle in progress at its position, its
context: the leaders that grow there and the seeds they have reached. A call records what its result depended on: the
seeds it read, each by an identity that equal step results of a leader share; the outcome of each lookahead whose
evaluation read a seed, in place of those seeds; and the leaders that grew at the position (the present set, one bit
for each leader). A result that read no seed and observed no lookahead goes to the memo with its present set and
answers any call whose context it includes. Any other result is kept in the table of its context, which lasts while the
first growth there lasts, and answers a call in a context with the same present set where the seeds it read have the
same identities and the lookaheads it observed have the same outcomes. A growth in a context memoizes its steps the same
way and records runs of steps that read only its own seed as jumps. Leaders grow from failure; a call of a leader that
grows at its position reads the seed. No dependent result reaches the memo of a Document, so later parses reuse only
results that depend on the rule, the position and the variables. The transient policy applies to members.

## Scope

All backends, and Documents. A transient member, one called from a single place, is not memoized in ordinary parses.

## Why It Is a Design

The [parsing specification](../../../spec/runtime/parsing.md#evaluations-of-rules) defines how the memo answers and
stores the calls in the context of growths, and the [compute units](../../../spec/runtime/parsing.md#compute-units)
depend on which calls the memo answers. The public option that disables memoization turns off every memo, not this
one, and the tests compare the engine with an internal parse option that evaluates every call in a context again;
neither is a maintained form of the same contract.

## Evidence

Evidence record: [130](../../optimizations/logs/130-memoizing-members-of-left-recursion-cycles.md).

Packages: `internal/frontend/check` (the memo flags and the numbers of cycles), `internal/runtime/state` (calls,
growths, the context state and the dependency memo of the engine), `pkg/runtime/libgo` (those of generated Go parsers),
`internal/ir/code/exec/compose` (the call protocol of Exec IR, with the context and growth operations that
`internal/ir/code/exec` declares) and the TypeScript library.

Related: [deferred memoization](deferred-memoization.md), [transient rule memo policy](transient-rule-memo-policy.md).
