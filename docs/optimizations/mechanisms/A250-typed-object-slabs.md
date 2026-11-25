# A250: Typed Object Slabs

After small-result bootstrap allocations, typed struct and terminal cells use
chunks to amortize per-object allocation.

## Mechanism and Invariants

Each typed owner starts slabs at 8 cells and grows them geometrically: a slab
is at most the larger of 256 cells and 1/32 of the cells that the owner had
allocated before it (`chunkRatio`), so the number of slabs of a parse is
logarithmic in its output. Fresh owner state avoids reusing returned cells. One
escaped cell can retain unused siblings in its chunk, so a retained value pins at
most one slab of at most 256 cells or 1/32 of the allocations of its owner, and
the unused tail of the last slab is at most the larger of the fixed limit and 1/33 of the owner's total. Bootstrap and optional liveness cleanup are separate policies. Direct
construction and Node conversion use the same object allocator.

## Scope and State

Default generated Go typed output only. Node slabs, engine values and TypeScript
objects use different allocation policies.

## Reference Control

Private `disableTypedObjectSlabs` emits individual object allocation while
holding list slab policy independently selectable. It is not the
bootstrap-disable control.

## Evidence and Implementation

Records 048/074 describe typed construction and smaller first chunks; 098 adds
independent early allocations. Retained-output controls preserve spans, sharing
and field pointers. Slab allocation can reduce object counts while increasing
retained siblings; current broad speedup is not established by the individual
allocator controls.

Typed `ParseValue` allocations on JSON and XML grew linearly with the input
(707 and 661 at 512 KiB) because every chunk had a fixed limit; with the ratio
32 they are 466 and 417 (JSON 72, 99, 139, 213, 318, 466 and XML 62, 85, 123,
186, 282, 417 over 16 to 512 KiB), with the bytes allocated between -0.4% and
+1.1% (JSON) and -0.5% and +2.0% (XML) of the fixed chunks'. A ratio of 8 gave
271 and 232 allocations at 512 KiB but about 7% more bytes at 128 to 512 KiB, the
unused tail of each type's last slab; the ratio is the one that keeps the bytes
within about 2%. The `Memory` limit counts the real size of each slab.

Evidence records: [048](../048-a-typed-runtime-for-parsevalue.md),
[074](../074-smaller-first-chunks-for-generated-typed-values.md),
[098](../098-bootstrap-generated-typed-output-allocations.md).

Source owners: [`astNew generation`](../../../internal/backend/codegen/golang/print/types.go),
[`disableTypedObjectSlabs`](../../../internal/backend/codegen/golang/print/generator.go),
[`objects/individual
controls`](../../../internal/backend/codegen/golang/print/gengo_typed_output_retention_test.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A070](A070-typed-output-bootstrap.md), [A260](A260-typed-list-slabs.md), [A090](A090-typed-output-liveness-cleanup.md).

[Optimization Catalog](../README.md)
