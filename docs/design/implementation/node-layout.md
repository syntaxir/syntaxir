# Node Layout

<!-- former-mechanism A010: Every backend that builds Nodes reads this layout, and it is the public representation of a Node; a parser without it would need a different Node -->

Every Node carries its names, positions, children and fields. The layout keeps them compact and shares the kind
metadata between Nodes, without changing the names a tree reports or its JSON form.

## Design and Invariants

The fields of a Node are an ordered slice of name and value pairs, not a map per Node. A Node kind interns the pair of
type and rule, and each rule computes its kind once, so matching does no interning lookup. Positions are 32-bit; the
names are read through the type and rule accessors. Field lookup and the sorted JSON output keep their contracts. Slab
storage of Nodes and the records of Document snapshots are separate mechanisms built on this layout.

## Scope

The Nodes of the closure engine, both VMs and generated Go Node parsers. Typed Go results are structs of their own,
and TypeScript builds JavaScript objects; neither shares this layout.

## Why It Is a Design

The layout is what the public `Node` type of the runtime is, and every backend that builds Nodes, the slabs and the
Document snapshots depend on it. A parser without it would need another Node representation, not a slower form of the
same one, so there is nothing a control could switch off.

## Evidence

Record 003 reports the JSON allocation volume falling from 72 to 64 MB when maps were replaced. Record 063 measures
11–20% lower Go allocation volume with timing within about 3%; record 064 records the reductions in generated parsers,
with mixed small timing effects. The 88-byte Node of those records is specific to the host and layout and does not
imply an equal reduction of the total heap.

Evidence records: [003](../../optimizations/003-node-fields-as-a-slice-54a5774.md),
[063](../../optimizations/063-a-smaller-node.md),
[064](../../optimizations/064-a-smaller-node-in-generated-parsers.md).

Packages: `pkg/runtime/value` (Nodes), `pkg/runtime/value/impl` (their representation and kinds) and
`pkg/runtime/libgo` (the Nodes of generated Go parsers).

Related: [Node and list slabs](../../optimizations/mechanisms/A020-node-and-list-slabs.md),
[expectation sets](expectation-sets.md).
