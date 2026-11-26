# A260: Typed List Slabs

Small typed list backing can share chunks instead of allocating a separate
backing array for every list.

## Mechanism and Invariants

After the first nonempty small-list bootstrap, list slabs start at 64 elements
and double up to 1,024, satisfying requested lengths;
[A280](A280-typed-chunk-growth.md) lets them grow further with the elements the
owner has allocated, lists above the small-list policy included, so a large
list raises the size later slabs may reach (by doubling at most from one slab
to the next). Lists above the small-list policy use exact-size allocations.
Published slices constrain capacity for append isolation; an escaped slice
retains its backing slab. Empty-list nil/non-nil shapes remain exact.

## Scope and State

Default generated Go typed direct and conversion output. Node child slabs and
TypeScript arrays are separate policies.

## Reference Control

Private `disableTypedListSlabs` emits independent list arrays; object slab
policy remains separately selectable. Bootstrap disable changes early
allocation, not all list slabs.

## Evidence and Implementation

Records 074/098 document first-chunk/bootstrap changes and retained-output
fixtures. The list control helps attribute sibling retention but does not
establish lower retained memory for every result or universal throughput gains.

Evidence records: [048](../048-a-typed-runtime-for-parsevalue.md),
[074](../074-smaller-first-chunks-for-generated-typed-values.md),
[098](../098-bootstrap-generated-typed-output-allocations.md); the growth past 1,024 elements is
[A280](A280-typed-chunk-growth.md)'s.

Source owners: [`astSlice generation`](../../../internal/backend/codegen/golang/print/types.go),
[`disableTypedListSlabs`](../../../internal/backend/codegen/golang/print/generator.go), [`slices/individual
controls`](../../../internal/backend/codegen/golang/print/gengo_typed_output_retention_test.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A070](A070-typed-output-bootstrap.md), [A250](A250-typed-object-slabs.md), [A280](A280-typed-chunk-growth.md).

[Optimization Catalog](../README.md)
