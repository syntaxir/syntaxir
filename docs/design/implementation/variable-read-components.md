# Variable Read Components

<!-- former-mechanism G090: A compiler technique whose result, the sorted set of variables each rule reads, is the same as that of the per-rule search it replaced; that search no longer exists, so there is no other code path to compare -->

A search of reachable rules for every rule would collect the same transitive variable reads again and again.
Dependency components compute them once for each recursive group.

## Design and Invariants

Direct reads are collected lazily; a grammar with none returns before it builds a graph. Callee-first strongly connected
components join the direct reads of their peers with the finished keys of the callees outside them once, sort them, and
share one immutable key slice among the peers. Definitions alone are not reads; recovery and Operators Rule expressions
keep their conservative traversal of dependencies.

## Scope

The metadata of rules that every compilation and generation target shares. The memo keys of every backend use the
result, but no parser runs the analysis.

## Why It Is a Design

It is a technique of the compiler. The set of variables that a rule reads is the set of those reachable through its
calls, which a search from each rule computes as well as the components do, and the result is sorted, so the variables
of every rule, and everything built from them, are the same either way. The search from each rule no longer exists, so
there is no other code path to switch to, and a byte-identity run would have nothing to compare: the classification
rests on reading the analysis. The [classification criteria](../records/080-optimization-management.md) make such a
technique a design of the compiler.

## Evidence

Record 073 measures the full compilation of a 512-rule chain without reads going from 10.114 to 1.003 ms and from
13.414 to 1.711 MB. Cases with reads and cycles have results of their own; other analyses and distinct transitive sets
still cost. Memo keys are unchanged, so these gains of compilation are not estimates of parser throughput.

Evidence records: [073](../../optimizations/logs/073-dependency-component-propagation-of-variable-reads.md).

Packages: `internal/middleend/corepass` (the variables of rules and their components).

Related: [memo chains](memo-chains.md), [variable environments](variable-environments.md).
