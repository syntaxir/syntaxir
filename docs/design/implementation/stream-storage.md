# Bounded Stream Storage

<!-- former-mechanism A050: The stream contract bounds what a stream keeps by its window: it discards the input and memo entries before what it delivered, and its Memory count bounds the window, not the length of the input -->

A stream discards the prefix it has committed and the memo entries before it, instead of growing its input storage
with every element it delivers.

## Design and Invariants

Loaded input keeps a window relative to the committed position. Discarding drops the prefix while it keeps the bytes
or code points that active matching, the memo and diagnostics still need. Delivered token strings and Nodes own their
storage independently. The chunks of a reader and the decoded storage are compacted by the stream's policy; a callback
that keeps results can still keep arbitrary output.

## Scope

Streams of the closure engine and both VMs, generated Go `-stream` parsers, whose typed `ParseStreamValue` converts
the elements the Node stream commits, and generated TypeScript push streams, which discard the input, memo entries and
skip records before the committed position as the Go stream does. The bound is on these owners of input and memo
storage, not on the total heap of the process.

## Why It Is a Design

The parsing specification requires it: a stream discards the memo entries and the input before the elements it has
delivered ([Memory](../../../spec/runtime/parsing.md#memory)), and returns their storage, so its count bounds its
window, not its length ([Memory Limit](../../../spec/runtime/parsing.md#memory-limit)). A stream that
kept its whole input would not be a slower form of the same stream but one that breaks that contract. The narrower
choices of how the window's storage is reused (capture frames, text snapshots, prefix ownership, undo and VM stacks)
are optimizations with reference controls of their own.

## Evidence

Record 022 combines the splitting of result chunks with in-place compaction of read-ahead. Streaming 50,000 CSV records
goes from 168.9 to 117.7 ms in the closure engine, with an allocated volume of 686 to 351 MB, and a retention fixture
goes from 233 to 0.5 MB after 300,000 records. These are different observations (time, cumulative allocation and
reachable heap), and later cleanups cannot be attributed to this ratio.

Evidence records: [022](../../optimizations/022-bounded-memory-and-less-copying-in-stream-parsing.md).

Packages: `internal/runtime/state` (the input window, the commit and the pruning of the memo).

Related: [stream capture frame reuse](../../optimizations/mechanisms/A200-stream-capture-frame-reuse.md),
[bounded stream text snapshots](../../optimizations/mechanisms/A210-bounded-stream-text-snapshots.md),
[stream prefix isolation](../../optimizations/mechanisms/A220-stream-prefix-isolation.md),
[stream undo cleanup](../../optimizations/mechanisms/A230-stream-undo-cleanup.md),
[stream VM stack cleanup](../../optimizations/mechanisms/A240-stream-vm-stack-cleanup.md),
[memo entry retirement](memo-entry-retirement.md).
