# 139. Geometric Typed Value Chunks

- **Status**: Adopted
- **Date**: 2026-11-26
- **Mechanisms**: [A280](mechanisms/A280-typed-chunk-growth.md); it changes the slabs of
  [A250](mechanisms/A250-typed-object-slabs.md) and [A260](mechanisms/A260-typed-list-slabs.md) and counts the
  bootstrap values of [A070](mechanisms/A070-typed-output-bootstrap.md)
- **Flags**: `typed-chunk-growth`
- **Revisit when**: the objects of [A250](mechanisms/A250-typed-object-slabs.md) or the lists of
  [A260](mechanisms/A260-typed-list-slabs.md) change their fixed limits or their bootstrap, a later mechanism
  bounds the allocations that are not chunk allocations, or the deterministic metrics show the typed bytes more
  than 2% above the fixed chunks' on any workload

## Problem and Mechanism

The chunks that hold typed values (the cells and lists of [A250](mechanisms/A250-typed-object-slabs.md) and
[A260](mechanisms/A260-typed-list-slabs.md), and the match cells of a typed parse) grew to a fixed limit, 256 cells or
1,024 list elements, and stayed there. Past that size every further chunk cost one allocation per fixed number of
values, so the allocations of a typed `ParseValue` grew linearly with the input, while the performance target asks
that they grow no faster than its logarithm.

A chunk may now reach the larger of its fixed limit and 1/32 of the values its allocator had handed out before it
(the chunk ratio, a constant of the library and of the generator's local copy of it), and at most twice the chunk
before it, so the chunks of one owner grow geometrically once it has handed out 32 times the fixed limit. A retained
value pins at most one chunk, of at most the larger of the fixed limit and 1/32 of the owner's earlier allocations,
and the unused tail of the last chunk is at most the larger of the fixed limit and 1/33 of the owner's total. The
`Memory` limit counts the real size of each chunk.

## Applicability

| Execution profile | Status | Reason |
|:--|:--|:--|
| Generated Go `ParseValue` (library imported or embedded) | Applied | Typed values come from the chunks |
| Generated Go `Parse`, `Recognize` | Not applicable | No typed values |
| Engine backends, generated TypeScript | Not applicable | Other value storage |

## Separability

The growth is an optimization in the sense of [record 080](../design/records/080-optimization-management.md), not a
parameter of the slab design: a ratio without bound is the fixed chunk of [A250](mechanisms/A250-typed-object-slabs.md)
and [A260](mechanisms/A260-typed-list-slabs.md), the plain form of the same allocator, which keeps the same contract
and relies on no other optimization. It is therefore the setting `typed-chunk-growth` of mechanism
[A280](mechanisms/A280-typed-chunk-growth.md), on in every profile. Off, the generator prints the local chunk
helpers with the fixed limit, as it does for the other storage controls, and sets a mask in the grammar that clears
the growth of the match chunks of a typed parse, which the library makes. The library applies the mask arithmetically,
so the setting adds no test to the default parser, which takes the library's chunks and prints nothing for the
setting. The mask is a field of the library protocol, which moved from version 36 to 37 because its exported
declarations changed; a parser of version 36 never sets the field, and its zero value is the growth, so the library
still implements 36.

## Reproduction

Base: main `6fcaf8348`; candidate: `54be31a17`, the two commits of the change on that base. The setting was added
afterwards: the default parser it generates differs from the candidate's only in the protocol constant, and the
library only in the mask that the size of a new match chunk applies. Against the change without the setting, on
the same base, the deterministic metrics of the default path are equal in allocations and bytes on every workload;
the Go library source is 541 bytes larger, the binaries 16 bytes, and the generated parsers are unchanged. Go 1.27.1, Node 24.13, Apple M3 Max on mains power, `GOMAXPROCS=16`,
`GOGC=100`.

- Allocations, bytes, sizes and compilation: `go run ./bench/metrics collect` on each revision (workloads and sizes
  parts), compared with `go run ./bench/metrics compare`. The counts are deterministic; the growth metric
  `parse-value-doubling-allocs` is (c₂ − c₁)/(c₁ − c₀) in thousandths for the allocations c₀, c₁, c₂ at a quarter, a
  half and the whole of each benchmark input, scaled to an exact doubling: 2000 is linear growth, 1000 logarithmic.
- Time: `go run ./cmd/timed` of syntaxir/bench `8ea663e` with `-base`, 10 rounds over `BenchmarkParse` (generated in
  both units and `generated_ast`) of JSON, CSV, XML and both arithmetic grammars, in an exclusive measurement window
  (load average 4.6 before, 3.9 after). The A/A control was accepted: geomean −0.10%, no row outside its spread.
- Retained memory: the live heap that a typed `ParseValue` result of JSON, XML, CSV and the calculator keeps after
  a collection, at 16, 64, 256 and 512 KiB, with `GOGC=off` and `GOMAXPROCS=1`, on the base and on the candidate with
  the setting; the result is held whole, so the unused tails of the last chunks count.

## Results

Typed `ParseValue` allocations (a quarter, a half and the whole of the input) and their growth:

| Workload | Base allocations | Candidate allocations | Base growth (‰) | Candidate growth (‰) |
|:--|--:|--:|--:|--:|
| JSON | 163, 255, 436 | 163, 247, 381 | 1965 | 1593 |
| XML | 190, 298, 511 | 189, 281, 438 | 1978 | 1712 |
| CSV | 121, 197, 349 | 121, 190, 306 | 2008 | 1687 |
| Arithmetic, Operators Rule | 135, 224, 400 | 128, 182, 250 | 1981 | 1262 |
| Arithmetic, left recursion | 159, 264, 467 | 158, 243, 370 | 1937 | 1497 |
| Minilang | 405, 639, 1082 | 405, 638, 1066 | 1885 | 1829 |
| Outline | 488, 928, 1856 | 488, 928, 1846 | 1969 | 1948 |
| Recovery | 583, 967, 1708 | 583, 967, 1695 | 1921 | 1887 |

Axes measured:

| Axis | Workloads | Change | Uncertainty |
|:--|:--|:--|:--|
| Time | `ParseValue` of JSON, CSV, XML, both arithmetic grammars | No difference on CSV, XML and Operators Rule; JSON +0.5% (95% CI +0.07% to +0.81%), left recursion +0.9% (+0.30% to +1.35%) | 10 rounds, A/A accepted |
| Time | `Parse` of the same, both units | No difference | 10 rounds |
| Allocations | Typed `ParseValue` of the eight workloads | −12% to −37% on the five that build their values directly, −0.5% to −1.5% on the other three (table above) | Exact |
| Bytes allocated | Typed `ParseValue` | Up to +1.02% (metrics); up to +2.5% in the medians of `B/op` of the timed run (JSON +0.31%, XML −0.54%, CSV +2.51%, Operators Rule +1.70%, left recursion +0.76%) | Exact; medians of 10 rounds |
| Retained memory | Typed `ParseValue` result of JSON, XML, CSV, calculator | −0.4% to +0.5% (largest: XML +0.46% and Calc +0.47% at 512 KiB, +126 KiB and +40 KiB; CSV −0.38% at 512 KiB) | Live heap after a collection, three runs per size; the last two agree within 250 bytes |
| Generated code size | Generated Go and TypeScript parsers | Unchanged; the Go library source (`go-lib`) +1,442 bytes (+0.25%) | Exact |
| Binary size | Go binaries of the parsers | +16 bytes, or one more 16 KiB page (+0.3% to +0.5%); DuckDB +82,576 bytes (+1.3%) | Exact |
| Compile time | `compile-allocs` of the generation | +805 allocations (+0.01% to +0.06%); bytes within tolerance | Exact |

## Correctness and Tradeoffs

A larger chunk lets one escaped value retain more unused siblings, bounded as above. A ratio of 8 gave 271 and 232
allocations at 512 KiB of JSON and XML but about 7% more bytes at 128 to 512 KiB, the unused tail of the last chunk of
each type; a lower floor, a square-root limit and a cap by the remaining input did not remove that without losing the
savings. The ratio 32 keeps the bytes within about 2.5% of fixed chunks.

## Decision and Limits

Adopted, on in every profile. The objective is the allocation target of typed `ParseValue` (allocations that grow
with the logarithm of the output): it removes 12–37% of the typed allocations of the workloads that build their
values directly, for at most 0.9% of `ParseValue` time, 1.02% of the bytes allocated (2.5% in the timed medians) and
at most 0.5% of the memory a result retains. `speed` keeps it on for the allocations. `size` keeps it on: off, the parser prints local chunk
helpers, which is more code. `memory` turns the object and list slabs off, so there the setting acts only on the
match chunks; it keeps the default, because a profile value changes only with a measurement of that profile
([record 136](136-measured-effect-of-the-generation-flags.md)), and this record did not measure one.

It does not meet the growth target. At the benchmark inputs most owners have not handed out the 8,192 cells (32 × 256)
after which their chunks grow, so the fixed limit still sets most chunk sizes. Minilang, Outline and Recovery change
by only 0.5–1.5%, so most of their allocations are not chunk allocations (Minilang and Recovery convert their values
from the tree). The open next mechanism is to bound those allocations and to start the geometric growth earlier
without the tail bytes that a smaller ratio cost.
