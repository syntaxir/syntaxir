<!-- generated:facts -->
# A240: Stream VM Stack Cleanup

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Streams of the VMs
- **Every parser of**: recursive VM, iterative VM

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `sxir_reference_stream_stack` | reference build tag | — | — | — | recursive VM, iterative VM | [089](../logs/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md) |
<!-- /generated:facts -->

Popping a VM value stack can leave references beyond its active length. Stream
cleanup clears that inactive suffix when completed elements no longer need it.

## Mechanism and Invariants

Cleanup waits for VM completion of an element and the same prune-batched
1,024-position boundary. It clears only the inactive stack region, preserving
live operands and published results. Capacity remains reusable. This is distinct
from the cleared stack retained by a Document across reparses.

## Scope

Recursive and iterative VM streaming only. Closure and the generated Go
and TypeScript streams have no VM operand stack.

## Controls and Measurements

The build tag restores the former inactive-reference behavior. The combined tag
`sxir_reference_stream_storage`, which is not a registered control, also changes undo and prefix policy.

## Evidence and Implementation

Record 089 measures the storage controls and their combined retained-heap
reduction. It does not establish an independent universal stack-cleanup speedup
or immediate release at every commit.

Evidence records:
[089](../logs/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md).

Source owners: [`inactive VM value
cleanup`](../../../internal/runtime/state/stream_stack.go),
[`vm.Machine.ClearInactive`](../../../internal/runtime/vm/vm.go), [`prune-batched
commit cleanup`](../../../internal/runtime/state/input.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A100](A100-document-vm-value-stack.md).

Related designs: [Bounded Stream Storage](../../design/implementation/stream-storage.md).

[Optimization Catalog](../README.md)
