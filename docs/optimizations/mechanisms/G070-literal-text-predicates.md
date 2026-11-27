<!-- generated:facts -->
# G070: Literal Text Predicates

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Predicates that compare a capture's text with literals, without limit controls
- **An eligible subset, named under Scope, of**: Go Node, Go recognizer, typed Go, TypeScript

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `text-dispatch` | setting (action pass) | on | speed on, memory on, size on | adopted (speed, memory, size) | Go Node, Go recognizer, typed Go, TypeScript | [136](../logs/136-measured-effect-of-the-generation-flags.md) |
<!-- /generated:facts -->

A pure OR of comparisons against one capture's text can compute that text once
and dispatch by literal, rather than repeat general expression evaluation for
every branch.

## Mechanism and Invariants

The complete predicate must consist of comparisons of the same direct capture
with string literals, joined by OR; a single comparison qualifies, and either
operand order is accepted. The action lowering reads the text once and returns
whether it equals one of the literals, testing them in order. Mixed
expressions, aliases, bindings and different captures remain general,
preserving short-circuit order and side effects. Helper/literal-table sharing
is independent of this shape specialization. Generated Go and TypeScript take
this path on ordinary nil-control calls; active budget-controlled calls keep
the general charged evaluation.

## Scope

Eligible predicates in generated Go Node, typed Go and TypeScript.
Closure and both VMs retain their general action evaluator for this disjunction
shape.

## Controls and Measurements

With the setting off, the same generator emits the general predicate. It adds no runtime selector to the
generated parser.

## Evidence and Implementation

Record 091 compares the same generator with dispatch disabled; it measured
dispatch as a switch over the deduplicated literals, which the comparison chain
has not been measured against. Large Go
workloads have paired geometric-mean time ratios about 0.952/0.955 in
CodePoints/Bytes, while small-workload means are 1.001/0.990. TypeScript has
separate scoped gains and a slower first-match case. No-op source is
byte-identical. These results concern qualifying pure predicates, not arbitrary
OR expressions or all parsers.

Evidence records: [091](../logs/091-specialize-pure-literal-text-disjunctions.md).

Only predicates with two or more distinct literals become a switch: repeated literals collapse, and a single
distinct literal stays the plain equality `ts == "x"`, which is shorter and says what it is.

Source owners: the action lowering of the Exec IR, which recognizes the literal
text dispatch, and the Go and TypeScript action targets, which print it as one
switch; [`route and unsafe-shape
checks`](../../../internal/backend/codegen/common/gen_literal_text_dispatch_test.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M120](M120-unboxed-text-comparisons.md), [G130](G130-generated-literal-sharing.md).

[Optimization Catalog](../README.md)
