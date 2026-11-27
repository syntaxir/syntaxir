# Lint Prefix Bounds

<!-- former-mechanism G125: A technique of the linter, off the generation path: no generated parser depends on it, so its output is the same with and without it -->

A lint diagnostic shows a shortened expression when its formatted text is long. The display keeps only the displayed
prefix and a bounded lookahead, instead of copying the whole formatted string into runes.

## Design and Invariants

Formatting still runs first. The display stores at most 37 runes and reads through the 41st to decide whether the
truncation rule applies: an expression of at most 40 runes stays unchanged, and a longer one shows its first 37 runes
followed by `...`. On truncation, the conversion to runes keeps the replacement of invalid UTF-8 in ASTs that callers
build; text that is not truncated stays verbatim. This bounds the processing and temporary storage of the prefix, not
the work of the formatter or the size of the formatted string.

## Scope

Lint diagnostic messages, in the command line and the language server. No parser runs it.

## Why It Is a Design

It is a technique of a tool, and the generation path does not reach the linter, so every generated parser is the same
with and without it. The [classification criteria](../records/080-optimization-management.md) make such a technique a
design of its tool.

## Evidence

The measurements of formatting and lint cancellation in
[record 042](../records/042-responsive-lsp-analysis-and-code-actions.md#grammar-and-lsp-formatting-cancellation)
include these diagnostic paths but do not isolate the prefix. They are not evidence of an isolated speedup, a lower
peak memory or a bound on latency.

Packages: `internal/analysis/lint` (the display of expressions and its benchmark).

Related: [LSP position checkpoints](lsp-position-checkpoints.md).
