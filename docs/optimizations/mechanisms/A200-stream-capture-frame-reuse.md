# A200: Stream Capture Frame Reuse

An emitted stream element no longer needs its completed frame structs. Rewinding
their slab prefix reuses storage for the next element.

## Mechanism and Invariants

Commit clears dead frames while preserving the live top-level frame. Capture
value arrays, nodes and child/field slices remain separately owned and are never
reused through this operation. Root-search protection handles roots that reside
in sealed or later slabs. A nil callback retains ordinary collected repetition
behavior.

## Scope and State

Default engine streaming in closure and both VMs. Generated Go streams reuse
their own capture frames in the runtime library, which this control does not
select; generated TypeScript streams keep no frame slabs and leave their
storage to the JavaScript garbage collector.

## Reference Control

Build tag `sxir_reference_stream_frames` selects the former allocation route
without a runtime selector. It is independent of text and prefix cleanup
controls.

## Evidence and Implementation

Record 085 measures same-source CSV stream allocated volume falling about 12–13
MB/op on each engine; closure allocations fall from 308,050 to 306,050. Median
time ratios are 0.988/0.994/1.000 and ranges overlap parity. Unicode stream
cases also lower allocated volume. These results support scratch reuse, not a
clear broad speedup or removal of input/capture-value copying.

Evidence records: [085](../085-reuse-stream-capture-frames.md).

Source owners: [`stream frame
allocation/commit`](../../../internal/runtime/state/runtime.go), [`frame reuse
selection`](../../../internal/runtime/state/stream_frame_scratch.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A220](A220-stream-prefix-isolation.md).

Related designs: [Bounded Stream Storage](../../design/implementation/stream-storage.md).

[Optimization Catalog](../README.md)
