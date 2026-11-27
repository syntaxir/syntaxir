# 148. Moving Reused Trees of Generated Go Documents

- **Status**: Adopted
- **Date**: 2026-11-27
- **Mechanisms**: R090 (generated Go Node Documents and the basic values of typed Documents)
- **Flags**: none

## Problem and Mechanism

After an edit that changes the length of the text, a generated Go Document copied every reused result whose positions
the edit shifted: each node of the subtree, at each parse that reused it. The engine moves such a result in place
once (R090, record 045). On the 100,000-line CSV and Minilang texts of the edit-latency benchmark, a length-changing
edit and the reparse that follows took 10.3 ms and 10.2 ms on average after record 147, against 4.69 ms and 6.71 ms on
the closure backend (Results of records 147 and 149); the copies allocated 51 MB per edit on CSV. A CPU profile put
most of the parse in the copies and in the collector, which had to scan them.

The generated library now moves reused results as the engine does, in the libgo package:

- Each node keeps in `Data.Mark` the generation of the Document whose positions it holds: the number of edits when it
  was built or last moved. A memo entry keeps the generation of its result with the shift that is still to be applied.
- When a parse reuses a shifted entry, it moves the result in place and stamps the current generation on each node it
  moves. A descendant that is already current, because another result shares it, is left alone; a descendant of
  another generation is moved by the shift of the edits since its own generation, replayed from the edit log and
  cached per generation for the parse.
- An empty node at the position of an edit is ambiguous (it may belong before or after the inserted text), so it is
  copied with the shift of its entry, as the engine does; its descendants move in place.
- A repetition run reused across an edit moves its saved element values in the same way.
- Typed Documents move basic values as before; typed values that hold nodes are still copied.

Moving costs no compute units: it is work of the Document, which units do not count (the exact Cost contract of
`spec/runtime/parsing.md` is unchanged). The memory accounting is unchanged, since the moved nodes are the ones the
memo already held. A memo entry gains the generation of its node: 144 bytes instead of 136.

## Applicability

| Execution profile | Status | Reason |
|:--|:--|:--|
| Generated Go Node Document | Applied | Every reused shifted result |
| Generated Go typed Document (`ParseValue`) | Partial | Basic values move; typed values that hold nodes are copied |
| Engine Documents | Not applicable | Already moves (R090) |
| Generated TypeScript Documents | Not applied | Copies a reused value at each reuse; a follow-up |

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

Head against record 147; A/A geomean 0.9999, accepted:

| Workload | Mean | Ratio | Median ratio | p99 ratio |
|:--|--:|--:|--:|--:|
| CSV, same length | 1.53 ms | 1.009 | 1.015 | noisy |
| CSV, length-changing | 3.88 ms | 0.380 | 0.438 | noisy |
| Minilang, same length | 0.88 ms | 1.067 | 1.049 | noisy |
| Minilang, length-changing | 6.02 ms | 0.589 | 0.643 | 0.366 |

Same-length edits shift nothing and gain nothing; they pay 1–7% for the generation that every node and memo entry now
records, and for the memo entry of 144 bytes instead of 136. The compaction of memo entries, the next step on Document
memory, is where that size is to be won back. Units and the other deterministic metrics are unchanged; retained memory
is unchanged (the entries fit the same pages). The library grows by 0.9–1.3 KB.

## Correctness and Tradeoffs

The spec allows a parse to move the nodes that an earlier parse returned
([Documents](../../../spec/runtime/documents.md)), and takes `Clone` as the way to keep a tree; the guide to generated
Documents now says the same for generated Go. `TestDocumentMoveReplaysEditsOfSharedNodes` builds a node shared by two
results of different generations and checks that each reuse moves it once by its own shift;
`TestDocumentMoveGenerationsRestartWithTheLog` checks the generations across the restart of the edit log;
`TestDocumentMoveMovesEveryValue` checks every kind of value a memo entry holds, and
`TestDocumentMoveKeepsSharedNodesShared` that a shared node stays one node. The Document tests of the library,
`TestDocumentTrace` of package `bench` (each edit of the trace reparsed and compared with a fresh parse), the mutant
tests of the repetition runs and the tests of the ready-made parsers pass.

A tree returned by an earlier parse changes in place; a caller that kept it without `Clone` sees new positions, as with
the engine. The gap between generated Go and the engine in the spec's list of differences is removed.

## Decision and Limits

Adopted, with no setting. The mechanism is the implementation of an existing contract, not a separable optimization
(record 080, criterion 2): the spec's bound on the work of a Document parse counts each reused result once, and copying
a reused tree at each reuse makes the work of a parse grow with the size of the trees it reuses, not with the text that
changed. The copying form is not a maintained alternative; the engine has no copying control either (R090).

Revisit typed values that hold nodes, which are still copied, when a typed Document is measured with length-changing
edits.
