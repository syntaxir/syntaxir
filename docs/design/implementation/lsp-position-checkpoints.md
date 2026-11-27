# LSP Position Checkpoints

<!-- former-mechanism G120: A technique of the language server, off the generation path: no generated parser depends on it, so its output is the same with and without it -->

An editor that converts positions on a long line again and again does not scan the line from its start each time.
Checkpoints bound the work of converting between UTF-16 and code points.

## Design and Invariants

The text index of the language server records cumulative counts of UTF-16 units and code points every 64 bytes. A
conversion starts at the checkpoint before it and scans the rest of that segment. UTF-8 boundaries, supplementary
characters and line and column behavior stay as the text index defines them.

## Scope

Position conversion in the language server only. No parser runs it.

## Why It Is a Design

It is a technique of a tool, and the generation path does not reach the language server, so every generated parser is
the same with and without it. The [classification criteria](../records/080-optimization-management.md) make such a
technique a design of its tool.

## Evidence

Record 065 reports a one-megabyte grammar on a single line analyzed and tokenized in about 0.35 s instead of over 10 s,
while ordinary grammars still take around 11 ms. The result on a long line does not measure parsing.

Evidence records: [065](../../optimizations/065-position-conversion-in-the-language-server.md).

Packages: `internal/lsp`.

Related: [diagnostic line index](../../optimizations/mechanisms/M090-diagnostic-line-index.md).
