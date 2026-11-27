# Limit-Control Forms of Generated Go

<!-- former-mechanism G050: Not a separable choice: the two forms are the user option -no-limits, and the specification states that a parser generated without limit controls counts no units -->

Resource checks would cost generated parsing even when no limit is set. A generated Go parser therefore has one form of
its functions, which the options of the generation choose.

## Design and Invariants

The generator prints each execution function (rule bodies and the other functions that take the parser as their first
parameter) once, from Flow IR, in the form the options select:

- By default, the parser holds the controlled form only: budget charges, the general runtime helpers and class tests
  that consult the control. A parse that sets no limit and no cancelable context has no control and runs the same code:
  the inline charges accept an absent control and do nothing, the class tests of large classes take the compact path,
  and the helpers test the control themselves. The action expressions of an action carry no charge, and neither do the
  generated constructors of typed values: only repetition is charged, by the runtime's loops over the elements of a
  list (`map`, `filter`, `foldl`, `foldr`), one step for each element.
- With `-no-limits` and without `-document`, the parser holds the ordinary form only: the runtime helpers that have a
  hand-written ordinary form, no budget charges, no control reads in conditions and class tests, and a compact peek. A
  parse that passes a control option fails.
- With `-document`, the in-place reads of literals and characters are guarded so that a Document parse reaches the
  primitives that record what a rule examined. With `-no-limits`, that form has no charges and no control reads.

There is no second body, no choice between forms when the parser runs, and no splice. Metered paths keep their checks,
cancellation and budgets. A user who needs both forms generates two parsers.

## Scope

Generated Go Node, recognition and typed Go output. Generated TypeScript chooses its form with the same option but
prints its own charges (see [TypeScript printing](typescript-printing.md)). The budget controls of the closure engine
and the VMs are their own.

## Why It Is a Design

The two forms are not an optimization and its plain form but two contracts that the user chooses: `-no-limits`
(`NoLimitControls`) is a user option of the generation, and the
[compute units](../../../spec/runtime/parsing.md#compute-units) say that a parser generated without limit controls
counts no units. Budget accounting is a resource contract, not an optimization in itself.

## Evidence

The default parser has one controlled form of its functions. The `parser.go` of the same parser generated with
`-no-limits` is 0.50 to 0.85 of its size (every grammar of `testdata/grammars/`, Node-only, with `-types`, with
`-types -recognize` or with `-types -typed-only`; larger grammars save more, and DuckDB with `-typed-only` is the
smallest at 0.50). Against a parser generated with `-no-limits`, a parse of the controlled form that passes no option
takes 1.08 to 1.13 times as long (`Parse`) and 1.08 to 1.17 times (`ParseValue`) on the JSON, XML, YAML and CSV inputs,
and one that passes a limit takes 1.25 to 1.33 times (`Parse`) and 1.26 to 1.45 times (`ParseValue`). The aim of 1.15
times for a parse with a limit is not met; with every charge made free JSON still takes 1.15 times, so the rest is the
shape of the controlled bodies. The table is in record 103, which measured the forms when the controlled one was
selected by an option. Records 096 and 121 hold earlier measurements of the ordinary body of a function and of the
inlined runtime helpers.

Evidence records: [096](../../optimizations/logs/096-specialize-generated-go-method-helpers.md),
[103](../../optimizations/logs/103-inline-the-common-budget-spend.md),
[121](../../optimizations/logs/121-inactive-budget-control-checks.md).

Packages: `internal/backend/codegen/golang/print` (the Flow printer and the names of helpers) and `pkg/runtime/libgo`
(the metered runtime helpers).

Related: [generated helper sharing](../../optimizations/mechanisms/G010-generated-helper-sharing.md),
[typed Go printing](typed-go-printing.md).
