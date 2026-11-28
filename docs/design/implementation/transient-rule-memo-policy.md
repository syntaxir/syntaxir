# Transient Rule Memo Policy

<!-- former-mechanism R120: The specification fixes which rules an ordinary parse does not memoize, and the compute units depend on which calls the memo answers -->

A rule whose ordinary memo results cannot be reused needs no entries. The analysis of transient rules removes that
bookkeeping before the parse.

## Design and Invariants

A rule that calls no rule, or that is called from at most one place in the grammar, is transient: an ordinary parse
does not memoize it. A repeated call at a position through its single caller is already covered by the caller's memo.
A start rule that its own body calls is memoized all the same, and so is every left-recursion leader. Documents
memoize transient rules, because later edits can reuse their results; the classification removes no incremental
state.

## Scope

The closure engine, both VMs, generated Go Node and typed Go, and TypeScript. Documents memoize every rule.

## Why It Is a Design

The [parsing specification](../../../spec/runtime/parsing.md#evaluations-of-rules) states which rules an ordinary
parse does not memoize, and the [compute units](../../../spec/runtime/parsing.md#compute-units) of a parse depend on
which calls the memo answers. A parser that memoized transient rules would count differently, so it is not a
maintained form of the same contract.

## Evidence

Record 001 measures transient rules together with value-free bodies and twins and does not isolate their effect on
time or memory.

Record 151 measures and rejects Documents that keep no entries for transient rules that only match characters: the
memory they save is bought by matching those rules again at every element of a list that a reparse visits.

Evidence records: [001](../../optimizations/logs/001-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903.md),
[151](../../optimizations/logs/151-documents-without-value-less-lexical-entries.md).

Packages: `internal/middleend/corepass` (transient rules), `internal/frontend/check` (the transient flag of rules),
`internal/runtime/state` (the decision between the ordinary and the Document memo) and the TypeScript generator.

Related: [deferred memoization](deferred-memoization.md),
[plain rule calls](../../optimizations/mechanisms/M050-plain-rule-calls.md).
