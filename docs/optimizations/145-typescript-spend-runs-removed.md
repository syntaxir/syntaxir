# 145. TypeScript Spend Runs Removed

- **Status**: Removed
- **Date**: 2026-11-27
- **Mechanisms**: none
- **Flags**: `ts-spend-runs` (removed)

## Problem and Mechanism

`ts-spend-runs` was an Exec pass of the TypeScript backend that made consecutive budget charges of a lowered function
one charge of their sum. Generated TypeScript then charged its own budget form: spends at its operations, chunks for
strings and runs, and reservations that the engine and generated Go no longer made.

## Decision

When generated TypeScript came to count the logical compute units that the engine counts (a unit per call, the key of
a memoized rule, the input that terminals advance over and the other events of the compute contract), checked against
the limit at structural points, the charges that the pass merged were no longer printed, and the pass had nothing left
to merge. It was removed with the form it served.

Before the first release, `-opt` and `-no-opt` reject the name with an error that names this record.
