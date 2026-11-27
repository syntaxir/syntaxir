# Expectation Sets

<!-- former-mechanism A110: The memo entries, recovery state and diagnostics of every backend store expectations as these IDs and stack windows; a parser without them would need another representation of expectations -->

Repeated diagnostic strings and copied lists of expectations would make the bookkeeping of the memo and of errors
expensive. Expectations are integer IDs fixed at compile time, kept in windows of one stack.

## Design and Invariants

Description tables resolve the IDs into messages. Each isolated region owns a window of the expectation stack; a set
that must outlive it, in a memo entry or in recovery state, is copied into an arena, and equal consecutive kept sets
are shared where the backend supports it. The order, the farthest position and the message flags stay exact. The IDs
are an internal representation, not part of a diagnostic a user sees.

## Scope

All backends: the closure engine, both VMs, generated Go Node and typed Go, and TypeScript. Go allocates the kept sets
in arenas, and TypeScript in arrays it owns.

## Why It Is a Design

The memo entries, the recovery state and the error construction of every backend are written against this
representation. A parser without it would need another representation of expectations throughout, not a slower form of
this one. The filter that avoids duplicate searches over these sets is an optimization of its own.

## Evidence

Record 004 reports the Minilang allocation volume falling from 59 to 42 MB and the time from about 128 to 103 ms.
Record 011 ports the representation to the generated parsers. The effect on each backend today is not measured.

Evidence records: [004](../../optimizations/logs/004-expected-sets-as-interned-ids-on-a-shared-stack-11a6ce3.md).

Packages: `internal/runtime/state` (record, isolate and keep), `internal/frontend/check` (the expectation table),
`pkg/runtime/libgo` (the IDs of generated Go parsers) and the TypeScript library.

Related: [absent expectation filter](../../optimizations/mechanisms/M100-absent-expectation-filter.md).
