# TypeScript Printing

<!-- former-mechanism G210: The only route by which TypeScript rules are printed; a rule that Flow IR does not print fails generation, and there is no second printer -->

Generated TypeScript prints its rules, Operators Rule lines and outlined nodes from the Flow IR that generated Go uses,
so a rewrite of the Core or Flow IR reaches both.

## Design and Invariants

Every rule of the Node and recognizer tables is lowered to Flow IR (as a rule, a framed body or an outlined node) and
printed by the TypeScript printer: a block becomes a labeled block, a loop `for (;;)`, a condition a Flow IR
expression. A rule that Flow IR does not print fails generation; there is no second printer.

The printer keeps the text small and the functions few:

- Printing runs dry to a fixpoint of what is read, so a part of a mark (position, environment, trail, recovered count)
  is declared only as far as it is read, and an unread assignment is not printed.
- The TypeScript Exec passes, each with a flag, make consecutive step charges one charge of their sum, an expectation
  unisolate followed by a merge of the same variables one merge in place, and the test of an `if`'s first call its
  condition.
- Functions whose printed bodies are equal share one method. The key of a function replaces each constant it names (a
  literal table, the capture names, a bind function) by the declaration of the constant, so functions that read equal
  tables under different names share one method although the literal tables themselves are not shared.
- Framed nodes that are large, or whose structure occurs more than once, run as functions of their own, so no function
  grows with the grammar and equal nodes share a body. The planning of the table chooses these nodes, and its lowering
  adds their functions to the table.
- A recovered error carries the header line only in `stack`; building it suspends `Error.stackTraceLimit`, because
  capturing a stack for each recovered error dominated recovery time. The error of a failed parse keeps its stack.

Budget charges follow the budget model of the IR: one at rule entry and one for each loop iteration. A parser generated
with `-no-limits` prints none.

## Scope

Generated TypeScript, Node and recognizer tables, with and without `-types`.

## Why It Is a Design

This is the only route by which TypeScript rules are printed. A parser without it would need another printer, not a
slower form of this one, so there is nothing a control could switch off. The separable choices of the printer (the
Exec passes, outlined nodes, compact Exec forms) are optimizations with flags of their own. The generated parsers are
compared with the engine over the corpus in both limit-control forms, and the Core and Flow IR goldens freeze the
decisions.

## Evidence

Record 122 measures the printer against the grammar-tree printer it replaced, in six rotated rounds on Node 24. The old
printer always spent budget, so its output is the controlled form; the comparison below sets it against the form
without limit controls, and the comparison of like with like is in the record.

- Speed: parse and recognition of JSON, CSV and XML are 17% to 46% faster (parse 0.83x, 0.83x and 0.68x, recognition
  0.70x, 0.61x and 0.54x).
- Typed workloads: of nine pairs of workload and mode, the two recovery ones are 4% (node) and 6% (tree) slower with
  ranges that do not overlap, the left-recursion node workload is 8% faster with ranges that do not overlap, and the
  other six have overlapping ranges.
- Size: `parser.ts` is 16% to 23% smaller in bytes for every grammar but DuckDB, which is 14% larger, but it is larger
  after gzip for every grammar, by 6% to 46%: the printed text is shorter and less repetitive. Sharing outlined
  functions of equal structure removes 691 of the 1,924 outlined functions of DuckDB and 3.8% of its bytes.
- Limit controls: the form without them is at most 5.5% from the controlled form in time and 0.9% to 3.1% smaller.

Evidence records: [122](../../optimizations/logs/122-typescript-from-flow-ir.md).

Packages: `internal/middleend/flowlower` (the lowering) and `internal/backend/codegen/typescript/print` (the printer).

Related: [limit-control forms](limit-control-forms.md),
[outlined TypeScript nodes](../../optimizations/mechanisms/G220-outlined-typescript-nodes.md),
[compact TypeScript Exec forms](../../optimizations/mechanisms/G240-typescript-exec-forms.md).
