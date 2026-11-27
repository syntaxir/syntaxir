# G240: Compact TypeScript Exec Forms

Some sequences of Exec IR operations have a shorter form in the TypeScript library than the operations
print as one by one. Replacing them before printing makes the parser smaller and saves locals and
copies.

## Mechanism and Invariants

Two Exec passes of the TypeScript backend (`internal/backend/codegen/typescript/pass`) rewrite each
lowered function:

- `ts-merged-records`: the end of an expectation record followed by the merge of its expectations into
  the enclosing record becomes one `unisolateMerge`, which merges in place instead of copying the
  expectations.
- `ts-tested-inits`: an `if` that tests whether its first call matched tests the call's result in its
  condition, without the boolean local that the call sets.

The printer also prints the operations each pass replaces, so each flag turns its pass off and still
prints a parser. Each replacement ends the parse as the operations it replaces do, with the same
results, expectations and charges.

## Scope and State

Adopted for generated TypeScript. Go prints the operations of the Exec IR directly and the engine
backends do not print.

## Reference Control

`-no-opt ts-merged-records` and `-no-opt ts-tested-inits` print the replaced operations from the same
generator.

## Evidence and Implementation

Record [143](../logs/143-compact-typescript-exec-forms.md) decides the state on the generated size.

Source owners: the package of the TypeScript Exec passes in the TypeScript backend.

Related designs: [TypeScript Printing](../../design/implementation/typescript-printing.md).

[Optimization Catalog](../README.md)
