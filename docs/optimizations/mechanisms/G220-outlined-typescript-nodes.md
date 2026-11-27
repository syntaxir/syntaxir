# G220: Outlined TypeScript Nodes

A TypeScript function printed in one piece grows with the grammar: a rule of many alternatives, or a
body that nests the same few matchers many times, prints as thousands of lines that no other rule can
share, and a function that large is neither compact nor fast for the JavaScript engine to compile.
Running some nodes of a rule as functions of their own keeps every function small and lets equal
nodes share one body.

## Mechanism and Invariants

The pass `outline` runs on the Core IR of each Node and recognizer table right before the program is
planned. It chooses the nodes whose structure occurs in more than one place of the module and the
nodes that are large, so that no function grows beyond a bound, and sets them as the outlined nodes of
the table's lowering options. The lowering then calls each outlined node (`flow.CallOutline`) and adds
its function to the table, and the TypeScript printer shares the functions whose printed text is equal
once the constants they name are replaced by their declarations ([G010](G010-generated-helper-sharing.md)).
An outlined node runs as it does in place: the same input, results, expectations and charges.

## Scope and State

Adopted in the TypeScript pipeline only, for the Node and recognizer tables. Go and the engine
backends print each rule as one function.

## Reference Control

`-no-opt outline` prints every node in place from the same generator.

## Evidence and Implementation

Record [146](../146-outlined-typescript-nodes.md) decides the state on the generated size; the outlined
form was part of the printer that record [122](../122-typescript-from-flow-ir.md) measured.

Source owners: the `flowplan` package of the middle end (the outline pass).

Related mechanisms: [G010](G010-generated-helper-sharing.md), [G210](G210-typescript-from-flow-ir.md).

[Optimization Catalog](../README.md)
