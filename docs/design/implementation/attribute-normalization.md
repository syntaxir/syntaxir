# Attribute Normalization

<!-- former-mechanism G040: A compiler technique whose generated output is byte-identical with and without it, checked over the corpus with the eager form forced on -->

Preparing a grammar does not copy every expression node whose attributes and children already have the normalized
form. Copy-on-change reuses the unchanged paths.

## Design and Invariants

Normalization returns the original node when no child or attribute changes. A changed descendant copies each ancestor
on its path before the replacement, so the source AST is never changed. The order of attributes, binding, validation,
diagnostics and the shapes of nil and empty containers stay exact, and opaque extension values stay opaque. The
prepared grammar can share unchanged nodes with its source; it is not a detached deep copy.

## Scope

The preparation of grammars that compilation and generation share. No parser runs it.

## Why It Is a Design

It is a technique of the compiler, and the generated output of every corpus grammar is byte-identical with and
without it: the check forced the eager form (an internal compile option that copies every node) and compared the Go
Node, lean typed Go, TypeScript, VM bytecode and closure Core IR outputs of the ten corpus grammars. The
[classification criteria](../records/080-optimization-management.md) make such a technique a design of the compiler.

## Evidence

Record 095 reports reductions of allocation on its workloads, and a longer recheck that did not reproduce an initial
synthetic slowdown. Generated parsers are byte-identical. These measurements of compilation do not establish a
universal speedup of preparation or any effect on matching.

Evidence records: [095](../../optimizations/logs/095-attribute-copy-on-change.md).

Packages: the desugaring package (`internal/frontend/desugar`), with its checks of source immutability, and
`internal/frontend/check` (the option for eager copies).

Related: [variable read components](variable-read-components.md).
