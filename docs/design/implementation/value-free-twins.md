# Value-Free Twins

<!-- former-mechanism E020: The specification makes a twin a rule of its own, with its own memo entries and its own call unit; compute units and memo identities depend on it -->

A rule can be called where its result is discarded and elsewhere where its tree is needed. A value-free twin of the
rule serves the first kind of call without building the discarded tree and without changing the valued calls.

## Design and Invariants

The compiler makes a twin for a rule without actions or text types. Calls in discarded, atomic or eligible body
contexts call the twin. The twin is a rule with its own identity and its own memo entries, so its empty result never
reaches a valued memo entry. Rules of left-recursion cycles get no twin, because a seed that grows partly with values
and partly without would change the result. A twin never skips the action of a rule it calls.

## Scope

All backends: the closure engine, both VMs, generated Go Node and typed Go, and TypeScript. The valued rule stays.

## Why It Is a Design

The parsing specification defines value-free twins: a rule and its twin count as two rules, with memo entries of
their own ([Evaluations of Rules](../../../spec/runtime/parsing.md#evaluations-of-rules)), and a call of a twin is a
unit ([Compute Units](../../../spec/runtime/parsing.md#compute-units)). The counted units and the memo identities of every backend
depend on it, so a parser without twins would count differently and is not a maintained form of the same contract.

## Evidence

Record 001 measures twins together with value-free bodies and the transient memo policy, so its JSON reductions of time
and allocation cannot be attributed to twins alone. No isolated effect of twins is reported today.

Evidence records: [001](../../optimizations/001-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903.md).

Packages: `internal/frontend/check` (the twins and their eligibility), the bytecode compiler (the calls of twins),
and the Go and TypeScript generators.

Related: [value-free rule bodies](../../optimizations/mechanisms/E010-value-free-rule-bodies.md),
[memo chains](memo-chains.md),
[left-recursion growth](../../optimizations/mechanisms/M060-left-recursion-growth.md).
