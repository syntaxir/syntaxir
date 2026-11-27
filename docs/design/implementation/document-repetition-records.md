# Document Repetition Records

<!-- former-mechanism R100: The Document contract makes a resumed element no event, neither performed nor counted, and reports resumed elements in its statistics -->
<!-- former-mechanism R110: Part of the repetition records that a Document keeps for its whole text between parses, which the Document contract requires -->

A long repetition would spend its reparse looking up every unchanged element. A Document keeps records of the runs of
long repetitions, so a reparse reuses the prefix of a run, matches again around an edit, and splices in the unchanged
suffix.

## Design and Invariants

Repetitions of at least 16 elements record the positions, examined ranges, expectations and values of their elements.
After an edit, a resumption restarts near the affected element and rejoins the record where the new and old boundaries
of elements align. Reused Nodes move under the Document contract; the eligibility of captures, variables and errors
stays conservative. Several edits without a parse between them, an aborted parse and a reset or rollover of the edit log
invalidate the saved runs. The VMs find the run sites of their bytecode from the module.

Only an edit advances the generation of the runs. A parse without an edit since the last parse keeps the map of runs
of that parse, and a rebuilt repetition can replace a record in it; a parse after an edit makes the next map. No record
and no map is copied when nothing changed.

## Scope

The Documents of every backend.

## Why It Is a Design

The [Document contract](../../../spec/runtime/documents.md#reuse-model) defines runs as one of the two kinds of reuse,
which the statistics report (`ResumedElements` of [Statistics](../../../spec/runtime/documents.md#statistics)), and a
resumed element neither performs nor counts the elements it reuses
([Compute Units](../../../spec/runtime/parsing.md#compute-units)). A Document keeps its repetition records for its
whole text between parses ([Memory](../../../spec/runtime/parsing.md#memory)). A Document without runs would count and
report differently, so it is not a maintained form of the same contract.

## Evidence

Record 054 measures the reparse of a 100,000-line closure Document after an insertion and a deletion going from 11.0 to
5.1 ms, and a long CSV from 15.1 to 8.4 ms. Saved runs cost about 80 bytes for each element; Minilang Documents, whose
repetitions are short, cost about 2% more in that sample. Record 059 adds resumption to the VMs. Record 070 keeps the
runs across a parse without an edit: on a 100,000-line fixture with such a parse before the edit, 99,999 elements are
resumed again, and the closure engine goes from 6.90 to 0.778 ms and from 48.83 to 1.15 MB; without that parse, the
timing ranges overlap and allocations are the same. The benefit depends on the length of runs and on chances to rejoin.

Evidence records: [054](../../optimizations/054-resuming-long-repetitions-in-a-document.md),
[059](../../optimizations/059-resuming-long-repetitions-in-the-vms.md),
[070](../../optimizations/070-keep-repetition-records-across-unchanged-document-parses.md).

Packages: `internal/runtime/state` (run records and resumption), `internal/runtime/vm` (the run sites of the VMs),
`internal/runtime/document` (saved runs, their generations and eligibility), `pkg/runtime/libgo` (the Documents of
generated Go parsers) and the TypeScript library.

Related: [Document VM value stack](../../optimizations/mechanisms/A100-document-vm-value-stack.md),
[skip-run records](skip-run-records.md).
