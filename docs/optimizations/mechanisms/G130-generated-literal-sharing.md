<!-- generated:facts -->
# G130: Generated Literal Sharing

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Generated parsers with equal literals; TypeScript as an opt-in
- **An eligible subset, named under Scope, of**: Go Node, Go recognizer, typed Go, TypeScript

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `literal-sharing` | setting (printer) | on | speed on, memory on, size on | adopted (speed, memory, size) | Go Node, Go recognizer, typed Go | [136](../logs/136-measured-effect-of-the-generation-flags.md) |
| `ts-literal-sharing` | setting (printer) | off | speed off, memory off, size off | opt-in | TypeScript | [136](../logs/136-measured-effect-of-the-generation-flags.md) |
<!-- /generated:facts -->

Repeated immutable literal tables can be emitted once and referenced by multiple
matchers, reducing generated data duplication.

## Mechanism and Invariants

Interning keys use decoded literal contents. Each matcher retains its own
description/expectation identity; sharing data cannot merge diagnostics or rule
wrappers. Failed speculative lowering rolls back inserted literal entries and
reserved declarations. Helper-body sharing is independently selected.

## Scope

Literal sharing in generated Go Node and typed Go, and in TypeScript through its own setting. Helper sharing
([G010](G010-generated-helper-sharing.md)) is separate. Core closure/VM literals are separate compilation
representations.

## Controls and Measurements

`-no-opt literal-sharing` selects duplicate tables, and takes precedence over `ts-literal-sharing`, which
opts TypeScript into literal sharing. These select generated source before parsing.

## Evidence and Implementation

Record 090 compares helper-only, literal-only, combined and no-sharing outputs.
It primarily supports source/build-cost reduction; mixed TypeScript literal
results did not justify default adoption. Combined ratios do not isolate literal
sharing alone.

Evidence records: [090](../logs/090-share-identical-generated-helpers.md).

Source owners: [the literal registry of the generator state](../../../internal/backend/codegen/common/generator.go),
[`TypeScript literal sharing selection`](../../../internal/backend/codegen/typescript/print/generator.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G010](G010-generated-helper-sharing.md), [G070](G070-literal-text-predicates.md).

[Optimization Catalog](../README.md)
