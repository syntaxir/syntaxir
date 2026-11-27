# Go Node Bodies

<!-- former-mechanism G100: The only route by which Node and recognizer bodies of generated Go are printed; a body that neither form prints is a generation error -->

Generated Go Node parsers print their bodies as Go code, without dispatching each expression, while the runtime keeps
the invocation of rules and their results.

## Design and Invariants

Value-free plain bodies are printed from Flow IR as direct rules; the other bodies (value-building and memoized bodies,
cuts, recovery, left-recursion leaders and Operators Rule lines) are printed as framed bodies that the runtime calls
under the frame it owns. The wrapper of a rule keeps its depth, memoization, naming, rollback and recovery state. Direct
bodies may call framed rules through the rule table. The entry bodies of an external rule table leave the invocation to
their caller.

## Scope

Generated Go Node parsing and recognition. Typed Go prints its rules through [its own route](typed-go-printing.md);
TypeScript is [printed from Flow IR](typescript-printing.md).

## Why It Is a Design

A Node or recognizer body is printed either as a direct rule or as a framed body; a body that neither prints is
reported as an error, and there is no reference route that dispatches each expression. A parser without it would need
another printer, not a slower form of this one.

## Evidence

Record 076 measures generated JSON recognition over code points going from 3.169 to 1.899 ms when value-free bodies
were first printed inline, with more generated source. Record 077 then measures median ratios of 0.817–0.937 on
selected Node workloads against its own parent, with different source and build trade-offs. These compare successive
revisions on supported bodies.

Evidence records: [076](../../optimizations/logs/076-inline-value-free-plain-generated-go-rules.md),
[077](../../optimizations/logs/077-inline-node-expressions-in-generated-go-parsers.md).

Packages: `internal/middleend/flowlower` (eligibility in Flow IR) and `internal/backend/codegen/golang/print` (the
direct and framed bodies and the choice between them).

Related: [typed Go printing](typed-go-printing.md),
[direct code-point access](../../optimizations/mechanisms/M080-direct-code-point-access.md),
[short literal comparisons](../../optimizations/mechanisms/M130-short-literal-comparisons.md).
