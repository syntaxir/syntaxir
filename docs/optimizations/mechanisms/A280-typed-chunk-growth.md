# A280: Typed Chunk Growth

The slabs of typed objects ([A250](A250-typed-object-slabs.md)), of typed lists
([A260](A260-typed-list-slabs.md)) and the chunks of the match cells of a typed parse stop at a fixed size
without this mechanism, so their number, and the allocations of a typed `ParseValue`, grow linearly with the
output. With it they keep growing with the values their owner has handed out, and their number grows with the
logarithm of the output.

## Mechanism and Invariants

A slab may reach the larger of its fixed limit (256 cells, 1,024 list elements, 256 match cells) and 1/32 of
the values its owner had allocated before it; each slab is still at most twice the one before it. An owner
counts every value it hands out, including the bootstrap values of [A070](A070-typed-output-bootstrap.md) and
the lists above the small-list limit, which are allocated on their own: a large list therefore raises the size
that the later slabs of its owner may reach, by doubling at most from one slab to the next.

Once an owner has handed out 32 times the fixed limit, its slabs grow geometrically. A retained value pins at
most one slab, of at most the larger of the fixed limit and 1/32 of the owner's earlier allocations, and the
unused tail of the last slab is at most the larger of the fixed limit and 1/33 of the owner's total. The
`Memory` limit counts the real size of every slab.

## Scope and State

Generated Go typed output, with the library imported or embedded, in both direct construction and Node
conversion. Where object or list slabs are off ([A250](A250-typed-object-slabs.md),
[A260](A260-typed-list-slabs.md)) it acts only on the slabs that remain and on the match chunks. Ordinary
Node parses, the engine backends and TypeScript are unchanged.

## Reference Control

The optimization setting `-no-opt typed-chunk-growth` keeps every slab and match chunk at its fixed limit: the
parser prints the local chunk helpers with the fixed limit and sets a mask in the grammar that clears the growth of
the library's match chunks without a test.
Every profile leaves it on.

## Evidence and Implementation

Record [139](../139-geometric-typed-value-chunks.md) measured it: typed `ParseValue` allocations fall by 12–37%
on the workloads that build their values directly, the doubling ratio of the allocations falls from about 2.0
(linear) to 1.26–1.71, and the bytes allocated rise by at most 1.02% (deterministic metrics). A ratio of 8
halved the allocations again but cost about 7% more bytes, the unused tail of each type's last slab.

Related mechanisms: [A070](A070-typed-output-bootstrap.md), [A250](A250-typed-object-slabs.md),
[A260](A260-typed-list-slabs.md).

[Optimization Catalog](../README.md)
