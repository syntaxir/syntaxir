# Deferred Memoization

<!-- former-mechanism R020: The specification fixes exactly which first evaluations leave no result, the same on every backend, because the compute units depend on which calls the memo answers -->

Most results of an ordinary whole-input parse are never reused. Recording only that a rule was called at a position
avoids a full memo entry until the rule is called there again.

## Design and Invariants

A first call marks the pair of rule and position; a later call at the same position stores its result, which bounds
repeated evaluation. Each rule counts its calls and its repeated calls: once the repeated calls exceed a sixteenth of the
calls, every evaluation of the rule stores its result, for the rest of the parse. Before the counts can overflow, both
are halved. Left-recursion leaders store every result, because their seed drives growth. Documents and streams store
every result, for reuse after edits and for pruning. Recognition counts by the rules of its own program.

## Scope

Ordinary whole-input parsing and recognition in the closure engine, both VMs, generated Go Node and typed Go, and
TypeScript. It is not the policy of Documents or streams.

## Why It Is a Design

The [parsing specification](../../../spec/runtime/parsing.md#evaluations-of-rules) defines the rule exactly, with the
threshold of one sixteenth and the halving of the counts, and requires it to be the same on every backend, because the
[compute units](../../../spec/runtime/parsing.md#compute-units) of a parse depend on which calls the memo answers. A
parser that stored every result would count differently, so it is not a maintained form of the same contract. How the
seen positions are stored is an optimization of its own.

## Evidence

Record 018 reports closure JSON parsing going from 22.5 to 18.1 ms and from 41.2 to 25.8 MB, and JSON recognition from
15.9 to 12.0 ms; left-recursive parsing with the adaptive threshold can still cost about 0–5% more (20–33% without it).
Record 021 ports the policy to generated Go. These results do not establish a universal benefit.

Evidence records: [018](../../optimizations/018-memoizing-from-the-second-call-at-a-position.md),
[021](../../optimizations/021-changes-18-and-19-in-the-generated-parsers.md).

Packages: the call protocol of Exec IR (`internal/ir/code/exec/compose`), which every runtime prints over its record of
a call: `internal/runtime/state` in the engine, `pkg/runtime/libgo` in generated Go, and the TypeScript library.

Related: [sparse seen-position storage](../../optimizations/mechanisms/R030-sparse-seen-position-storage.md),
[transient rule memo policy](transient-rule-memo-policy.md).
