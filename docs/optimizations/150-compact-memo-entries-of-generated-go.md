# 150. Compact Memo Entries of Generated Go

- **Status**: Adopted
- **Date**: 2026-11-27
- **Mechanisms**: none (the representation of the generated Go library's memo entries)
- **Flags**: none

## Problem and Mechanism

A memo entry of the generated Go library took 144 bytes, of which 64 held what it keeps beside its value: the errors
recovered in the call as two pointers (one error, or a list of them), and the expectations at the farthest failure and
the values of the rule's variables (the memo key) as two slices of three words each. A Document keeps an entry for
every memoized result of its last parse; after records 147 to 149, the entries were most of what a Document of the
edit-latency texts retains beyond its tree.

The library now keeps those in 32 bytes, in the libgo package:

- the expectations and the key as a pointer to their first element and a 32-bit length each, which share the arrays
  the slices had, so that an entry keeps the same memory alive;
- the errors as one pointer, to an error or to a list, which the top bit of the expectation count tells apart: an
  expectation set holds distinct expectations, numbered by 32-bit integers, so its length never reaches that bit.

Node entries and typed entries take 112 bytes instead of 144. The memory limit counts entries at their size, so a
parse that stores entries counts 32 bytes less for each; the memo keys, the expectation arrays and the error lists are
counted as before. Units and results are unchanged.

## Applicability

| Execution profile | Status | Reason |
|:--|:--|:--|
| Generated Go parses, recognitions, typed parses and streams | Applied | Every memo entry |
| Generated Go Node and typed Documents | Applied | Every memo entry the Document keeps |
| Engine | Not applied | Its entries have their own layout |
| Generated TypeScript | Not applicable | Objects of the JavaScript engine |

## Reproduction

Timed with the `go` part of `cmd/timed` of the bench repository (`8ea663e`) in a measurement window on an Apple M3 Max
on AC power, 20 interleaved rounds with an A/A arm, the commit of this record against its parent:

```sh
go run ./cmd/timed -main <checkout> -parts go -rounds 20 -head <commit> -base <parent> -bench 'DocumentEdit/(CSV|Minilang)/.*/generated$'
```

Deterministic numbers come from `go run ./bench/metrics collect` and `compare` at both commits, and the retained memory
from `bench/document-retained` (`GOGC=off GOMAXPROCS=1`, 100,000 lines).

## Results

Head against its parent; A/A geomean 0.9960, accepted:

| Workload | Mean | Ratio | Median ratio | p99 ratio |
|:--|--:|--:|--:|--:|
| CSV, same length | 1.15 ms | 0.974 | 1.003 | noisy |
| CSV, length-changing | 3.68 ms | no difference | no difference | noisy |
| Minilang, same length | 0.92 ms | 0.938 | 0.957 | 0.931 |
| Minilang, length-changing | 6.33 ms | no difference | 1.026 | 0.951 |

No mean and no p99 is worse; two medians are slower, by 0.3% (CSV, same length) and 2.6% (Minilang,
length-changing).

The retained memory of a Document over its tree (`document-memory-permille`) falls from 2.49 to 2.28 on CSV and from
4.76 to 4.05 on Minilang: the Documents retain 25.6 MB and 130 MB less. A batch parse allocates 4 to 18% fewer bytes
on the benchmark grammars that memoize (Arith_LeftRec, Minilang, Recovery), and 0.1 to 0.3% fewer on XML.

The library grows by 2.1 KB (0.37%), and generated binaries by 64 bytes to 16.5 KB. Generating a parser allocates 1,671
more objects (60 KB) whatever the size of the grammar. A memory profile of one generation step (a synthetic grammar of
40 rules), compared between the two commits, puts four fifths of the added objects in parsing the embedded library
source before pruning it, almost all of them under function declarations, and the rest in printing the doc comments of
the declarations kept. The declarations that cost it are the new ones: the entry's side record, its error-list marker
constant and its six accessors (recursion records, expectations and environment key, each read and set), with the
memo store and hit paths of the batch and typed calls now going through those accessors. The cost is per generation,
not per parse.

The retained bytes of a tree alone, without a Document, and of Document clones and snapshots grow by a constant 24 to
48 KB, the same at 1,000 and at 100,000 lines. This offset is unexplained: heap profiles of the two commits show the
same objects in use, so it is not in what the parse keeps.

## Correctness and Tradeoffs

The pointer and length of a slice keep the slice's array alive as the slice did, and its capacity is its length: the
library never appends to the expectations or the key of an entry, and counts their bytes by their lengths.
`TestMemoSideKeepsWhatItIsGiven` checks that an entry gives back the errors, the expectations and the key it was given,
over the same arrays, with each of the others absent, empty or set and in either order of the setters, and that setting
the errors again replaces the kind of their pointer. The conformance tests
of the library's Exec IR operations, the Document memory tests, which pin the entry sizes and the bytes the limit
counts, and the tests that a pooled parser keeps no state of an earlier parse, hold the new form.

## Decision and Limits

Adopted, with no setting. The layout is the representation of the library's memo, not a separable optimization
(record 080, criterion 2): there is no second form to choose, and the 32 bytes it saves on each entry cost nothing
else that the measurements show.

Entries keep 64-bit positions. The normative maximum input of 2^31 − 1 position units lets them be 32-bit, which is the
next step on Document memory.
