# 149. Kept Child Stack of Generated Go Documents

- **Status**: Adopted
- **Date**: 2026-11-27
- **Mechanisms**: none (the generated Go form of the engine Document's kept stacks)
- **Flags**: none

## Problem and Mechanism

After records 147 and 148, a one-digit edit of the 100,000-line CSV and the reparse that follows still allocated
5.4 MB on a generated Go Document, against 1.19 MB on the closure backend. An allocation profile of 300 edits put
1.26 GB of the 2.08 GB allocated at the one `append` to `Parser.KidStack` in the repetition of the records: every
Document parse began with an empty stack of repetition values and grew it again, by doubling, to the 100,000 records
of the file. The engine's Document keeps the stack of its last parse for the next one.

A generated Go Document now does the same, in the libgo package: a Node parse takes the stack that the
Document keeps and gives it back, empty, when it ends; a parse that stops on a panic does not give it back. Every pop
of the stack clears what it takes off, so the parse clears only what it left on the stack, not the whole capacity, as
the engine does.

The memory limit counts the kept stack: the bytes that the Document retains, which a parse compares with the limit to
decide whether the history alone passes it, include its capacity, and the Node parse that holds it counts it as its
own storage, as it counted the stack it grew. Under a memory limit a parse takes no stack from a pooled parser, so
that its count does not depend on what the parser parsed before; the kept stack is the Document's own storage and is
taken under a limit too, as in the engine. A typed parse starts its count at the bytes that the Document retains, the
stack included. `CacheStats.EstimatedBytes` includes it, and `ResetHistory` drops it. Units are unchanged: the stack
is not an item of the cost.

## Applicability

| Execution profile | Status | Reason |
|:--|:--|:--|
| Generated Go Node Document | Applied | Every parse |
| Generated Go typed Document (`ParseValue`) | Not applied | The typed parse gathers values on stacks of its own |
| Engine Documents | Not applicable | Already keeps its child and backend stacks |
| Generated TypeScript Documents | Not measured | |

## Reproduction

Timed with the `go` part of `cmd/timed` of the bench repository (`8ea663e`) in a measurement window on an Apple M3 Max
on AC power, 20 interleaved rounds with an A/A arm:

```sh
go run ./cmd/timed -main <checkout> -parts go -rounds 20 -head <commit> -base <parent> -bench 'DocumentEdit/(CSV|Minilang)/.*/generated$'
```

Each capture compares the commit of a record with its parent. A last capture compares the commit of record 149 with
`6acbe16ee`, the parent of record 147, with the closure rows as a control. Deterministic numbers come from
`go run ./bench/metrics collect` and `compare` at each commit, and the retained memory from `bench/document-retained` (`GOGC=off GOMAXPROCS=1`, 100,000 lines).

## Results

Head against record 148; A/A geomean 0.9996, accepted:

| Workload | Mean | Ratio | Median ratio | p99 ratio |
|:--|--:|--:|--:|--:|
| CSV, same length | 1.17 ms | 0.769 | 0.905 | 0.403 |
| CSV, length-changing | 3.56 ms | 0.921 | 0.948 | 0.576 |
| Minilang, same length | 0.88 ms | no difference | no difference | noisy |
| Minilang, length-changing | 5.97 ms | no difference | no difference | no difference |

An edit of the CSV allocates 0.88 MB instead of 5.39 MB (`-benchmem`, 300 edits). Minilang has no repetition of that
length. The retained memory of the CSV Document grows by the kept stack, 0.96 MB (from 2.48 to 2.49 times its tree), and
of the Minilang Document by 66 KB. Units and the other deterministic metrics are unchanged.

The series, the commit of this record against `6acbe16ee`, the parent of record 147, with the closure backend as a
control; A/A geomean
1.0003 for the means, accepted:

| Workload | Generated mean | Ratio | Generated p99 | p99 ratio | Closure mean | Closure p99 |
|:--|--:|--:|--:|--:|--:|--:|
| CSV, same length | 1.14 ms | 0.064 | 3.32 ms | 0.125 | 1.32 ms | 3.91 ms |
| CSV, length-changing | 3.61 ms | 0.135 | 6.58 ms | noisy | 4.69 ms | 8.72 ms |
| Minilang, same length | 0.86 ms | 0.106 | 2.36 ms | 0.159 | 1.21 ms | 3.40 ms |
| Minilang, length-changing | 5.96 ms | 0.347 | 13.2 ms | noisy | 6.71 ms | 14.9 ms |

The closure rows show no difference or differences within 3% between the two arms, as expected of code that the series
does not change.

## Correctness and Tradeoffs

`TestDocumentKeepsTheChildStack` checks that the stack that two Document parses leave is empty, holds no value in its
capacity, is counted at its capacity, and is dropped by `ResetHistory`. `TestDocumentMemoryLimitCountsBothMemos` checks
that a kept stack of 1,000 entries makes a Node parse and a typed parse that reuse the memo count exactly 8,000 bytes
more. The Document stays as large as its largest stack, the one of its longest repetition, until `ResetHistory`.

## Decision and Limits

Adopted, with no setting. Keeping the stack is not a separable optimization (record 080, criterion 2): it is the
storage of the Document as the engine defines it, counted by the memory limit in the same way, and a stack grown again
at every parse is work in proportion to the longest repetition of the text, against the bound of
[Work of a Document Parse](../../../spec/runtime/documents.md#work-of-a-document-parse).

Revisit the typed parse's stacks when a typed Document is measured.
