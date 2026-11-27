# Memo Entry Retirement

<!-- former-mechanism R040: The stream contract bounds a stream's memory by its window, which needs the storage of dropped entries returned, and Documents report retired entries in their statistics -->

Removing the lookup slot of a memo entry is not enough while the slab that holds the entry still refers to its result.
Retirement clears those references and makes the storage of the entry reusable.

## Design and Invariants

When an entry is replaced, pruned or invalidated, retirement clears what it owns: the Node, the environment, the
recovered errors and the expectations. Pruning a stream keeps a seed that an active left-recursion frame still owns;
replacing a seed retires the old entry only after the frame has installed its next result. A free entry is reused
without changing any result already handed out. This bounds the storage of obsolete entries, not what results keep.

## Scope

The memo storage of the engine, shared by the closure engine and both VMs, in streams and Documents in particular. The
stream runtimes of generated Go and TypeScript keep their own account of the storage that each commit lets go of.

## Why It Is a Design

A stream returns the storage of the elements it has delivered, so its memory count bounds its window, not its length
([Memory Limit](../../../spec/runtime/parsing.md#memory-limit)); an entry that is unlinked but still holds its result
would keep that storage. A Document reports the retired entries it keeps for reuse (`RetiredMemoEntries` of its
[statistics](../../../spec/runtime/documents.md#statistics)). A memo that did not retire entries would break the first
and change the second, so it is not a maintained form of the same contract.

## Evidence

Record 067 measures the retention growth of discarded streams falling from tens or hundreds of MiB to 34–137 KiB, and
fewer allocated bytes on streams that memoize and grow left recursion. Batch timing stayed within about 4%. The result
bounds obsolete ownership on those workloads; it is not a bound on the total heap.

Evidence records: [067](../../optimizations/067-retire-memo-entries-when-their-lookup-slots-are-removed.md).

Packages: `internal/runtime/state` (the memo and the ownership of left-recursion seeds) and `internal/runtime/document`
(the invalidation of entries by edits).

Related: [left-recursion growth](../../optimizations/mechanisms/M060-left-recursion-growth.md),
[bounded stream storage](stream-storage.md),
[Document memo edits](../../optimizations/mechanisms/R070-document-memo-edits.md).
