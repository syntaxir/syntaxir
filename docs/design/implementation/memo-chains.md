# Memo Chains

<!-- former-mechanism R010: The memo structure that the complexity bounds rely on; every backend's memo, its keys and the Document reuse are built on it -->

The memo is indexed by the input position directly, so a lookup hashes no composite key and an entry needs no heap
allocation of its own. Rules that read variables still need keys that tell the values they read apart.

## Design and Invariants

Each position owns a chain of entries. A key holds the rule, the recursion level and the values of the variables the
rule reads, in an order fixed at compile time. Entries and the expectations they keep live in chunk storage. When no
variable is defined, a shared empty key replaces a fresh one; it is never written. A missing variable stays distinct
from a defined value under the key comparison. A Document can splice the index of positions without changing a key.

## Scope

The closure engine, both VMs, generated Go Node and typed Go, and TypeScript. TypeScript indexes chains by position and
keys them by variables too, but allocates entries as JavaScript objects, not in Go slabs. A rule may defer or omit
ordinary memoization; left recursion and Documents have policies of their own.

## Why It Is a Design

The bounds on the work of a parse ([Conditions for Linear Cost](../../../spec/runtime/parsing.md#conditions-for-linear-cost))
rely on memo lookups and stores that cost a constant of the grammar for each event, and the deferred memoization,
the retirement of entries, the member memo of left recursion and the reuse of Documents are built on this structure. A
parser without it would need another memo, not a slower form of this one.

## Evidence

Record 005 reports JSON going from about 130 to 85 ms with the per-position table. Record 012 removes the exponential
repeated evaluation of rules that read variables. Record 066 reports the allocations of a typed CUE configuration
falling from 128,458 to 3,006 with the shared key of undefined variables. These results concern distinct workloads and
revisions, not one combined ratio today.

Evidence records: [005](../../optimizations/005-memo-table-with-per-position-chains-cf77e4f.md),
[012](../../optimizations/012-memoizing-rules-that-read-variables.md),
[066](../../optimizations/066-no-memo-key-allocated-for-variables-where-none-is-defined.md).

Packages: `internal/runtime/state` (the memo and the keys of calls), `internal/middleend/corepass` (the variables a rule
reads), `pkg/runtime/libgo` (the memo of generated Go parsers) and the TypeScript library.

Related: [deferred memoization](deferred-memoization.md), [memo entry retirement](memo-entry-retirement.md),
[variable environments](variable-environments.md), [variable read components](variable-read-components.md).
