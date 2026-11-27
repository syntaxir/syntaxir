# 142. Canonicalization Without Flags

- **Status**: Removed
- **Date**: 2026-11-27
- **Mechanisms**: none
- **Flags**: `canonicalize`, `drop-after-exit`, `drop-unused-labels`, `fold-branches`, `fold-constants`, `splice-blocks` (removed)

## Problem and Mechanism

The pass `canonicalize` applies local rewrite patterns of Code IR to each lowered function until none applies, at the
Flow and at the Exec stage: `fold-constants`, `fold-branches`, `drop-after-exit`, `drop-unused-labels` and
`splice-blocks`. The pass and each pattern were registered as optimizations, so `-no-opt` turned them off.

An optimization changes a parser: its off form is the plain form of the same construct. These do not. The printers
already leave out the labels and blocks that the Exec patterns remove, and the statements that the Flow patterns drop
after an exit never ran, so the generated Go and TypeScript and the compiled bytecode are the same with and without
them. By the classification of [design record 080](../design/records/080-optimization-management.md), a technique of
the compiler whose output is byte-identical with and without it is an implementation design of the compiler, not an
optimization.

## Evidence

- **Backend matrix.** Over the cases of the flag parity corpus of the code-generation tests and four further cases
  (outlined keywords, class tables, terminals and shared bodies), turning off the pass or any one pattern left
  unchanged the printed Core IR of the closure engine, the VM bytecode, the generated Go of the Node parser (without
  limit controls, with them, and with a Document), of the recognizer and of the typed parser (lean and with limit
  controls), and the generated TypeScript (without limit controls, with them, and typed). Taken on `771138ae6`.
- **Language grammars.** The ten grammars of `testdata/grammars`, generated with the options of
  `testdata/grammars/generate.txt` and `-lib embed` as Go and TypeScript, are byte-identical with each of the six names
  turned off. Taken on `771138ae6`.
- The commit that introduced the patterns reported the same for the generated parsers and the bytecode.

## Decision

The six names are removed from the registry of optimizations. `canonicalize` is a required step of the Flow and the
Exec pipeline, and its patterns have no flag. Each application of a pattern stays a decision of the pass manager:
`-opt-bisect-limit` numbers it and can skip it, `-print-pipeline` lists the patterns under the pass, and `-stats`
counts their applications, so a miscompilation in a pattern remains findable. Before the first release, `-opt` and
`-no-opt` reject the six names with an error that names this record.
