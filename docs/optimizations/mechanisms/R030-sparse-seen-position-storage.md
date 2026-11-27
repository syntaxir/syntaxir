<!-- generated:facts -->
# R030: Sparse Seen-Position Storage

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Seen positions of deferred memoization
- **Every parser of**: closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `sparse-memo` | setting (storage) | on | speed on, memory on, size on | adopted (speed, memory, size) | TypeScript | [136](../logs/136-measured-effect-of-the-generation-flags.md) |
| `sxir_reference_sparse_memo` | reference build tag | — | — | — | closure, recursive VM, iterative VM | [087](../logs/087-sparse-deferred-memo-seen-pages.md) |

No control reaches it yet on: Go Node, Go recognizer, typed Go.
<!-- /generated:facts -->

Deferred memoization needs first-call bits for rule/position pairs. A dense
array scales with the whole input even when few positions are visited. Sparse
pages reduce that indexed backing on sufficiently large inputs.

## Mechanism and Invariants

The runtime chooses sparse seen pages above both 64 deferred rules and 1,024
decoded units; smaller cases keep dense storage. Core and generated Go cache
the last page to avoid repeated directory lookup; reset clears page ownership
and lookup state, and retention limits trim reusable backing. TypeScript
resolves its directory and arena offset on each access and owns fresh arrays
for each Parser. Generated Go and TypeScript emit the selected representation
support and threshold logic before matching.

## Scope

Whole-input parse/recognition support in closure, both VMs, generated Go
Node, typed Go and TypeScript. Documents and streams do not use this
deferred-memo representation. Selection by input size is not a public
optimization option.

## Controls and Measurements

The build tag restores dense storage in the engine, and the setting turned off generates a TypeScript parser
with dense storage. These are same-source representation comparisons; they do not disable deferred
memoization. Generated Go has no dense control: its shared runtime always chooses by the input size and the
rule count.

## Evidence and Implementation

Record 087 separates historical, same-source and storage-only comparisons.
Twelve adoption storage probes measure sparse seen backing about 71.6% below
dense, excluding entries, counters and total heap. A 65-case parent comparison
has time medians 0.952–1.035 with noisy ranges, establishing no general speedup.
Generated TypeScript has separately scoped Node measurements; typed-array
capacity is not JavaScript heap. Storage-only probes support no timing claim,
and 32-bit effects remain unmeasured.

Evidence records: [087](../logs/087-sparse-deferred-memo-seen-pages.md).

Source owners: [`sparse memo
selection`](../../../internal/runtime/state/sparse_memo.go),
[`memoTable.markSparseSeen`](../../../internal/runtime/state/memo.go),
[`UseSparseMemo`](../../../internal/backend/codegen/common/generator.go), [`generated
sparse pages`](../../../pkg/runtime/libgo/lib.go), [`TypeScript sparse
seen pages`](../../../internal/backend/codegen/typescript/lib/lib.ts).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A160](A160-whole-input-scratch-pooling.md).

Related designs: [Deferred Memoization](../../design/implementation/deferred-memoization.md).

[Optimization Catalog](../README.md)
