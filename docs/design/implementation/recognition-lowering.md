# Recognition Lowering

<!-- former-mechanism E040: The specification makes recognition build values only where a predicate can read them, with the same units at every setting; it is an operation of the contract, not a form that can be switched off -->

Recognition decides whether an input conforms without publishing a parse tree. It runs a program of its own that leaves
out the values and actions no one reads, and keeps those that decide predicates.

## Design and Invariants

Recognition builds a rule graph of its own, with memo identities of its own. Captures and actions that no predicate
reads are left out; an action whose value a predicate reads still runs and can fail. Syntax errors, recovered errors and
the rollback of matching keep the recognition contract. Recognition is an operation that the caller selects, not the
full parse with a hidden setting.

In generated Node parsers, the recognition rules are printed from Flow IR: the lean value-free rules as direct rules and
the others as framed bodies, in every build form. The first-character guards of the Flow copy are built after the
analysis pass, as in the table pipeline.

## Scope

Engine recognition in the closure engine and both VMs when the grammar is present. Generated Go and TypeScript print
recognition when it is requested; typed Go packages use their generated recognition entry point rather than building
typed output. The full parse stays valued.

## Why It Is a Design

The parsing specification defines what a recognition builds: values only for the rules that a call inside a capture
that a predicate can read reaches ([Evaluations of Rules](../../../spec/runtime/parsing.md#evaluations-of-rules)), and
the same units at every optimization setting ([Compute Units](../../../spec/runtime/parsing.md#compute-units)). A recognizer that built every value would be a
different operation, the full parse, not the same one without an optimization.

## Evidence

Record 007 measures recognition at about 1.5–1.8 times as fast as the full parse, and record 047 compares generated
recognition with the Node parse. These compare operations on their stated workloads; they do not ablate a mechanism.

Evidence records: [007](../../optimizations/logs/007-recognition-mode-3f7e9bf.md),
[047](../../optimizations/logs/047-recognition-in-generated-parsers.md).

Packages: `internal/runtime/engine` (the recognizer program), `internal/frontend/check` (the recognition check and the
analysis of the captures predicates read), and the Go and TypeScript generators.

Related: [value-free rule bodies](../../optimizations/mechanisms/E010-value-free-rule-bodies.md),
[lazy backend preparation](../../optimizations/mechanisms/G020-lazy-backend-preparation.md),
[whole-input scratch pooling](../../optimizations/mechanisms/A160-whole-input-scratch-pooling.md).
