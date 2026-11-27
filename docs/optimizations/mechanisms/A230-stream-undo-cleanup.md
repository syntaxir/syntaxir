<!-- generated:facts -->
# A230: Stream Undo Cleanup

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Streams of the engine
- **Every parser of**: closure, recursive VM, iterative VM

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `sxir_reference_stream_undo` | reference build tag | — | — | — | closure, recursive VM, iterative VM | [089](../logs/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md) |
<!-- /generated:facts -->

Resetting undo length can leave old capture references in its backing array.
Clearing inactive backing releases those obsolete owners after stream commits.

## Mechanism and Invariants

Once the undo journal is reset, prune-batched cleanup clears its full backing
capacity. It runs when at least 1,024 input positions have been consumed since
pruning. Until that boundary inactive references can remain, and scratch
capacity itself stays retained. Live frame/capture values and published results
remain untouched.

## Scope

Closure and both VM stream cleanup. Generated Go/TypeScript have no
stream API.

## Controls and Measurements

The build tag keeps the former backing references. The combined tag
`sxir_reference_stream_storage`, which is not a registered control, changes prefix and stack policy too; it
cannot attribute an undo-only effect.

## Evidence and Implementation

Record 089 includes paired retained-storage and throughput measurements for
independent/combined controls. Keep forced-GC retained bytes distinct from
allocated bytes and do not assign the combined reduction entirely to this
cleanup.

Evidence records:
[089](../logs/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md).

Source owners: [`undo backing
cleanup`](../../../internal/runtime/state/stream_undo.go), [`parser.commit and
pruning`](../../../internal/runtime/state/input.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A240](A240-stream-vm-stack-cleanup.md).

Related designs: [Bounded Stream Storage](../../design/implementation/stream-storage.md).

[Optimization Catalog](../README.md)
