<!-- generated:facts -->
# G220: Outlined TypeScript Nodes

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Large and repeated nodes of TypeScript Node and recognizer tables
- **An eligible subset, named under Scope, of**: TypeScript

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `outline` | setting (pass) | on | speed on, memory on, size on | adopted (speed, memory, size) | TypeScript | [146](../logs/146-outlined-typescript-nodes.md) |
<!-- /generated:facts -->

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

## Scope

The TypeScript pipeline only, for the Node and recognizer tables. Go and the engine
backends print each rule as one function.

## Controls and Measurements

With the setting off, the same generator prints every node in place, so a comparison measures what outlining
adds to the source size and to the time the JavaScript engine takes to compile and run the parser.

## Evidence and Implementation

Record [146](../logs/146-outlined-typescript-nodes.md) decides the state on the generated size; the outlined
form was part of the printer that record [122](../logs/122-typescript-from-flow-ir.md) measured.

Source owners: the `flowplan` package of the middle end (the outline pass).

Related mechanisms: [G010](G010-generated-helper-sharing.md).

Related designs: [TypeScript Printing](../../design/implementation/typescript-printing.md).

[Optimization Catalog](../README.md)
