# 147. Decoded Text of Generated Go Documents

- **Status**: Adopted
- **Date**: 2026-11-27
- **Mechanisms**: R060 (generated Go Node and typed Documents)
- **Flags**: none

## Problem and Mechanism

A Document generated with `-document` was much slower to parse after an edit than the engine's Document on the same
edits: on the 100,000-line CSV of the edit-latency benchmark, a one-digit replacement and the reparse that follows
took 18.4 ms on average with a p99 of 26.7 ms, against 1.32 ms and 3.91 ms on the closure backend (Results), and
allocated 41.7 MB against 1.14 MB. A CPU profile of 300 such edits put 2.84 s of the 3.91 s of the parses in the
preparation of the input, which decoded the whole text into a new `[]rune` and built a new table of byte offsets on
every parse, and 2.06 s in `Document.Edit`, which counted the code points of the whole text and scanned it twice from
the start to turn code-point positions into byte offsets. Parsing itself took about 0.6 s. The collector took most of
the rest. A Document parse did work in proportion to the whole text before it reused anything, against the bound of
[Work of a Document Parse](../../../spec/runtime/documents.md#work-of-a-document-parse), whose constant does not depend
on n.

In the libgo package, the Document now keeps its text in the form a parse reads, and an edit changes that form instead
of rebuilding it:

- In `CodePoints`, the Document keeps the decoded code points, which a parse reads as its input (`Parser.In`), and
  splices them in place at each edit. It also keeps the text in chunks of about 4,096 code points, each with its
  position and, unless it is ASCII, the byte offset of every 32nd of its code points, from which the offset of any of
  them is decoded. An edit rebuilds only the chunks that it touches, in their places in the list of chunks, joins a
  chunk that falls below 1,024 code points to a neighbor, and moves the positions of the later chunks. The token text
  of a node (`Parser.Text`) is a substring of its chunk; a text that crosses chunks is copied, and its copy is charged
  to the memory limit as any other copied text. `Document.Text` joins the chunks once per revision.
- The length of the text is known, so the input limit and the default compute budget take it without counting.
- In `Bytes`, a parse reads the Document's text directly, as before.

The memory limit counts the code points, the chunks and their offsets as the decoded input, as it counted the arrays
that each parse built; the sampled offsets cost an eighth of a byte per code point instead of four. The
text of the chunks is the Document's text, which the count leaves out as it leaves out the source of a parse.

## Applicability

| Execution profile | Status | Reason |
|:--|:--|:--|
| Generated Go Node Document, `CodePoints` | Applied | Every parse and edit |
| Generated Go typed Document (`ParseValue`), `CodePoints` | Applied | The typed parse reads the same input |
| Generated Go Documents, `Bytes` | Partial | A parse reads the text directly; an edit still copies the text into one string |
| Engine Documents | Not applicable | Piece storage of their own (R060) |
| Generated TypeScript Documents | Not applied | `DocumentBase` decodes and copies the whole text at each edit in the same way; a follow-up |

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

Edit and reparse of the generated Go Document (`-document`), head against base; A/A geomean 0.9975, accepted:

| Workload | Mean | Base mean | Ratio | Median ratio |
|:--|--:|--:|--:|--:|
| CSV, same length | 1.51 ms | 18.4 ms | 0.082 | 0.066 |
| CSV, length-changing | 10.3 ms | 26.5 ms | 0.386 | 0.325 |
| Minilang, same length | 0.83 ms | 8.0 ms | 0.104 | 0.089 |
| Minilang, length-changing | 10.2 ms | 16.9 ms | 0.604 | 0.582 |

The p99 rows of this capture are noisy (their A/A ratios lie outside the bound), so this record makes no p99 claim; the
last capture of the series below does.

Units, evaluations, memo hits and the allocations of the batch workloads are unchanged (`bench/metrics compare`, all
within tolerance). The retained memory of a Document after a parse grows by the decoded code points, which a parse
built and dropped before: CSV from 279.2 MB to 295.8 MB (from 2.34 to 2.48 times its tree of 119.1 MB), Minilang from
863.3 MB to 872.9 MB (from 4.71 to 4.76 times its tree of 183.4 MB). Offsets for every code point instead of every 32nd
cost another 17 MB on CSV. The library grows by 5.5–10 KB of Go source; generated parsers are unchanged, binaries grow
by at most 33 KB (page rounding) and compile allocations by 0.1–0.6%, from the larger embedded library.

## Correctness and Tradeoffs

`TestDocumentEditSplicesText` applies 300 random edits, growing and shrinking, with multi-byte characters, invalid
UTF-8 and pieces longer than a chunk, to four initial texts, and checks after each the code points, the chunk
invariants (positions, lengths, offsets, sizes), the token text of random ranges and the whole text. The generated
Document test `TestWorkWithinFactorOfFresh` holds an edit to two allocations on average: the new text of the chunk it
rebuilds and the growth of the edit log. The Document
tests of the library, `TestDocumentTrace` of package `bench` (each edit of the trace reparsed and compared with a
fresh parse) and the tests of the ready-made parsers pass.

The code points stay one flat array, spliced with a copy of the suffix at each edit that changes the length: four
bytes for each code point after the edit. That copy is work in proportion to the text after the edit, which the bound
of Work of a Document Parse does not allow; it is the one part of an edit that is left so. Generated code indexes
`Parser.In` directly, and a piece table under it would cost every read of every parse a lookup, so removing the copy
needs a different read path of the generated code, not a different storage alone. The chunks bound the copy of the
token text, which a parse never reads whole.

## Decision and Limits

Adopted, with no setting. The mechanism is the implementation of an existing contract, not a separable optimization
(record 080, criterion 2): the alternative, decoding the text at each parse, is not a maintained form of the same
contract, since it makes every reparse do work in proportion to the whole text against the bound of Work of a
Document Parse. The chunked token text is part of the same representation: a Document that kept the text as one string
would copy the whole text at each edit, against the bound on the work of an edit in the same section.

The splice of the flat array of code points is a known gap against the bound on the work of an edit. Revisit it when
the copy of the suffix shows in the profile of a length-changing edit at the measured sizes, or when a Document
larger than the 100,000-line benchmarks is a target; revisit the `Bytes` text when a `Bytes` Document is measured.
