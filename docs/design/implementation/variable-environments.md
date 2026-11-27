# Variable Environments

<!-- former-mechanism R050: The compute units bound every other piece of work by a constant of the grammar for each event; an environment that kept every binding would make a read walk the bindings of a repetition -->

Repeated bindings neither keep every shadowed binding nor make a read walk the history of bindings. A persistent
environment keeps one current binding for each name.

## Design and Invariants

Variables are numbered at compile time and held in a persistent vector: a flat copy-on-write array for up to 16
variables, and a 16-way trie above. Replacing a value copies one path and shares the rest, so saved environments keep
the state of rule scopes and backtracking. Binding a scalar equal to the current value reuses the environment. Live
snapshots and memo keys still keep the values their contracts need. This bounds the history of bindings, not the size
of user values.

## Scope

The closure engine, both VMs, generated Go Node and typed Go, and TypeScript.

## Why It Is a Design

The [compute units](../../../spec/runtime/parsing.md#compute-units) bound every piece of work that is not an event by a
constant of the grammar for each event. A read in an environment that appended every binding would walk the bindings
that a repetition made, which grows with the input, so a parser with such an environment would break that bound.

## Evidence

Record 069 measures a stream with three names keeping three bindings instead of growing to 180,001, with nearly flat
retained growth and faster reads of outer variables. Record 128 removes the walk over names and the copy of the prefix.
Do not infer a universal reduction of allocation or time.

Evidence records: [069](../../optimizations/logs/069-bound-persistent-variable-binding-histories.md),
[128](../../optimizations/logs/128-indexed-variable-environments.md).

Packages: `internal/runtime/state` (lookup and replacement), `pkg/runtime/libgo` (the environments of generated Go
parsers) and the TypeScript library.

Related: [memo chains](memo-chains.md), [variable read components](variable-read-components.md).
