# Implementation Designs

The representations and algorithms that the rest of the implementation is built on, whose absence would be a different
architecture rather than a slower parser, and the techniques of the compiler and the tools that change no parser. They
have no control: a parser without one is not a maintained form of the same contract. The
[optimizations](../../optimizations/README.md), by contrast, each have a control that turns them off for a comparison.
[Record 080](../records/080-optimization-management.md) gives the criteria that tell the two apart.

Several of these pages were mechanisms of the optimization catalog; the catalog maps each former ID to its page.

| Page | Contents |
|:--|:--|
| [Node Layout](node-layout.md) | The compact fields and interned kinds of a Node |
| [Expectation Sets](expectation-sets.md) | Expectations as compile-time IDs in windows of one stack |
| [Variable Environments](variable-environments.md) | Persistent environments with one current binding for each name |
| [Memo Chains](memo-chains.md) | The memo indexed by position, with keys that hold the variables a rule reads |
| [Deferred Memoization](deferred-memoization.md) | Storing a result from the second call of a rule at a position |
| [Transient Rule Memo Policy](transient-rule-memo-policy.md) | The rules that an ordinary parse does not memoize |
| [Left-Recursion Member Memoization](left-recursion-member-memo.md) | Memoizing the members of a left-recursion cycle in the context of growths |
| [Memo Entry Retirement](memo-entry-retirement.md) | Clearing and reusing the storage of dropped memo entries |
| [Skip-Run Records](skip-run-records.md) | Shared walks of skip expressions, run sites and scan sites |
| [Document Repetition Records](document-repetition-records.md) | Resuming the runs of long repetitions after an edit |
| [Bounded Stream Storage](stream-storage.md) | The window of input and memo that a stream keeps |
| [Value-Free Twins](value-free-twins.md) | Rules that match as their rule does and build no value |
| [Recognition Lowering](recognition-lowering.md) | The program of a recognition, which builds only the values predicates read |
| [Typed Go Printing](typed-go-printing.md) | Flat rules and framed bodies of typed Go, with Go locals for captures |
| [Go Node Bodies](go-node-bodies.md) | Direct and framed bodies of generated Go Node parsers |
| [TypeScript Printing](typescript-printing.md) | TypeScript printed from Flow IR |
| [Limit-Control Forms of Generated Go](limit-control-forms.md) | One form of the generated functions for each set of options |
| [Capture Types](capture-types.md) | Joins of equal capture types, and the positional items the language no longer has |
| [Variable Read Components](variable-read-components.md) | The variables each rule reads, by dependency component |
| [Attribute Normalization](attribute-normalization.md) | Copy-on-change normalization of the attributes of a grammar |
| [Sample Constructor Dependencies](sample-constructor-dependencies.md) | The worklist of the sample generator's analysis |
| [LSP Position Checkpoints](lsp-position-checkpoints.md) | Checkpoints of the language server's position conversion |
| [Lint Prefix Bounds](lint-prefix-bounds.md) | The bounded prefix of expressions in lint diagnostics |
