<!-- generated:facts -->
# A220: Stream Prefix Isolation

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Streams of the engine
- **Every parser of**: closure, recursive VM, iterative VM

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `sxir_reference_stream_prefix` | reference build tag | — | — | — | closure, recursive VM, iterative VM | [089](../logs/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md) |
<!-- /generated:facts -->

A long-lived stream root can retain a slab containing otherwise dead early
elements. Private prefix allocation isolates that root from later result chunks.

## Mechanism and Invariants

Before top-level streaming starts, a negative chunk sentinel budgets 32
exact-size prefix allocations. At stream start, tails seal and the sentinel
resets; later cells use normal slabs. Root protection remains necessary if
growth allocates a root later. Published Nodes, lists, fields and functions stay
immutable under reuse.

## Scope

Engine stream prefix ownership in closure and both VMs. Generated Go
streams give the prefix and the elements separate chunks in the runtime
library, which this control does not select; generated TypeScript streams keep
no slabs and leave their storage to the JavaScript garbage collector.

## Controls and Measurements

The build tag selects ordinary prefix slabs and isolates prefix ownership. The
combined tag `sxir_reference_stream_storage`, which is not a registered control, also disables the cleanup
routes, so it cannot attribute a prefix-only effect.

## Evidence and Implementation

Record 089 measures this with undo/VM cleanup in paired retained-storage cases
showing 81–92% reduction. Combined results are not prefix-only effects. It
records altered inline eligibility in allocation helpers and does not claim
universal throughput improvement.

Evidence records:
[089](../logs/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md).

Source owners: [`prefix allocation
policy`](../../../internal/runtime/state/stream_prefix.go), [`nodeChunks and stream
root protection`](../../../internal/runtime/state/runtime.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A200](A200-stream-capture-frame-reuse.md), [A230](A230-stream-undo-cleanup.md), [A240](A240-stream-vm-stack-cleanup.md).

[Optimization Catalog](../README.md)
