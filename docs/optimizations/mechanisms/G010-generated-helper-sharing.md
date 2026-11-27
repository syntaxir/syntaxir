<!-- generated:facts -->
# G010: Generated Helper Sharing

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Generated parsers with exactly equal bodies
- **An eligible subset, named under Scope, of**: Go Node, Go recognizer, typed Go, TypeScript

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `method-sharing` | setting (printer) | on | speed on, memory on, size on | adopted (speed, memory, size) | Go Node, Go recognizer, typed Go, TypeScript | [136](../logs/136-measured-effect-of-the-generation-flags.md) |
<!-- /generated:facts -->

Generated parsers can repeat identical expression methods and literal tables.
Sharing exact emitted helpers reduces generated source duplication without
merging rule identities.

## Mechanism and Invariants

Helper interning requires an identical method signature and complete body. Rule
wrappers remain separate, preserving memo keys, depth, captures and
expectations. Failed speculative direct lowering rolls back inserted helper
entries and reserved names, so later code cannot call a discarded helper.
Literal-table sharing is a separate registry and selector.

## Scope

Helper sharing in generated Go Node, typed Go and TypeScript. Closure
and VMs are outside this generator mechanism.

## Controls and Measurements

With the setting off, the same generator prints a duplicate-helper reference. Literal sharing can be held fixed
independently; no matching-time selector is emitted.

## Evidence and Implementation

Record 090 measures emitted-source changes and paired generated workloads in
Go/TypeScript. Its mixed-use TypeScript literal results did not justify default
adoption. Source reduction does not by itself prove lower matching time; use the
record's separate configurations and workload results rather than assigning one
universal speedup.

Evidence records: [090](../logs/090-share-identical-generated-helpers.md).

Source owners: [`Generator.Method`](../../../internal/backend/codegen/golang/print/generator.go),
[literal interning in the generator state](../../../internal/backend/codegen/common/generator.go),
[`TypeScript sharing policy`](../../../internal/backend/codegen/typescript/print/generator.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G130](G130-generated-literal-sharing.md).

Related designs: [Go Node Bodies](../../design/implementation/go-node-bodies.md).

[Optimization Catalog](../README.md)
