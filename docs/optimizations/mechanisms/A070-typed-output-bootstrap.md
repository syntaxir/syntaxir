<!-- generated:facts -->
# A070: Typed Output Bootstrap

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Typed Go output
- **Every parser of**: typed Go

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `typed-output-bootstrap` | setting (storage) | on | speed on, memory on, size on | adopted (speed, memory, size) | typed Go | [136](../logs/136-measured-effect-of-the-generation-flags.md) |
<!-- /generated:facts -->

A tiny typed result can retain a large first slab containing discarded siblings.
Independent early allocations keep common small results from immediately paying
that slab footprint.

## Mechanism and Invariants

For each owner, the first three objects/terminals allocate independently before
object slabs grow from 8 cells (A250). The first nonempty small list also
allocates independently before list slabs grow from 64 elements (A260). These values count toward the
growth of later slabs (A280). Exact-size large lists remain unchanged. Fresh ownership
preserves append isolation, spans and shared result values; returned chunks are
not pooled. Full/bounded liveness sweeps keep legacy helpers and are measured
separately.

## Scope

Generated Go typed output, both direct construction and
Node conversion. Ordinary Node parse/recognition, closure, VMs and TypeScript
are unchanged.

## Controls and Measurements

With the setting off, the parser emits the legacy first-slab strategy. Object/list slab controls can further
isolate ownership.
The legacy same-source comparator retains bootstrap state fields, so it is not
layout-identical to historical main.

## Evidence and Implementation

Record 098 measures direct discard-heavy retained output from about 2.1 MB to at
most a few KiB after cloning returned Text. Conversion retained little in both
routes. Ordinary XML Bytes costs about 2.3% versus the same-generator legacy
route, with additional allocations; small JSON improves. These scoped tradeoffs
supported adoption and do not establish a general speedup.

Evidence records:
[074](../logs/074-smaller-first-chunks-for-generated-typed-values.md),
[098](../logs/098-bootstrap-generated-typed-output-allocations.md).

Source owners: [`typed allocation helper
emission`](../../../internal/backend/codegen/golang/print/types.go), [`typed allocator
state`](../../../pkg/runtime/libgo/typed.go),
[`disableTypedOutputBootstrap`](../../../internal/backend/codegen/golang/print/generator.go),
[`retained-output
fixture`](../../../internal/backend/codegen/golang/print/gengo_typed_output_retention_test.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A250](A250-typed-object-slabs.md), [A260](A260-typed-list-slabs.md).

Related rejected attempt: [record 094](../logs/094-generated-typed-output-liveness.md).

[Optimization Catalog](../README.md)
