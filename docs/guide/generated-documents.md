# Incremental Documents in Generated Parsers

Generate with `sxir gen -g grammar.sxir -module example.com/parser -document -o parser`, or pass
`Document: true` to `compiler.GoConfig`. This adds a mutable Node `Document` alongside the ordinary batch `Parse` and
`ParseRule` functions. `NewDocument` returns a `runtime.Document`, the interface of the engine's Documents (`Text`,
`Unit`, `Edit`, `Parse`, `Stats`, `CacheStats`, `ResetHistory`); with `-types` it returns the package's own `*Document`,
which implements it and adds `ParseValue`. The engine's version of the same feature is described in
[Incremental Parsing](incremental.md).

## Editing and Reparsing

```go
doc, err := parser.NewDocument("first draft", parser.WithUnit(parser.CodePoints))
if err != nil {
	return err
}
tree, err := doc.Parse()
if err := doc.Edit(6, 11, "revision"); err != nil {
	return err
}
tree, err = doc.Parse()
stats := doc.Stats() // rule evaluations, memo hits and resumed repetition elements from this Parse
_ = tree
_ = stats
```

`Edit(start, end, replacement)` replaces the half-open range in the unit selected at construction. It rejects
invalid ranges and positions inside a UTF-8 character in `Bytes` mode without changing the text. `Text()` returns
the current text: the exact bytes in `Bytes`, and in `CodePoints` the text with each invalid UTF-8 byte of the
initial text and of every replacement replaced by U+FFFD, as in the engine's `Document`. Construction options become defaults for later `Parse` calls; per-call options can change
depth, context and compute limits, but cannot change the unit. An interruption during execution discards memo history so a
later call can retry. `Document` is single-goroutine, and results can share nodes with later parses. Call
`tree.Clone()` if an earlier tree must remain detached from the Document. It copies Node graphs, including Node
values in `[]any` and `[]*Node` fields, while preserving cycles and repeated references to the same slice view.
Overlapping slice views with different lengths or capacities are copied independently; arbitrary mutable action
values such as maps remain shared. `ParseStats` counts the rule evaluations (`Evaluated`) and reuses (`Reused`) of the last parse: the memo
results it reused (`MemoHits`) and the elements of long repetitions it resumed (`ResumedElements`), as the
engine's `Document` counts them. `ParseValue` keeps a typed memo of its own and resumes no repetitions.

## What Is Kept Between Parses

Both kinds of Document keep the memo results that examined only text before an edit where they are, and move those
that examined only text after it by the edit's change in length; the others are evaluated again. A result of a rule
whose actions or predicates read positions (`.Start`, `.End`), or that recovered errors, is not moved. The Node
Document moves the nodes of a moved result in place when a parse reuses it, as the engine's Document does, so a tree
returned earlier carries the new positions in the nodes that the parse reused: take `Node.Clone` before the next parse
to keep a tree as it was. `Edit`
applies itself at once only to the results at the text it replaced; a later parse moves any other result when it
looks the result up, so an edit costs time in proportion to those results and to its distance from the previous
edit, not to the size of the memo. A typed Document keeps its results
the same way and reuses a moved result only when its value is a basic value, a boolean, a number or a string; every
other value, such as a struct or a list, is evaluated again. The generated batch functions remain available. `-document` can be combined with `-types`: eligible
grammars use direct typed matching with a private memo and fresh invocation arenas. A typed result is
read-only and shared, as Node values are: later parses reuse the values of earlier results, so a result can share
values with the Document and with other results. Modifying a result can change what later parses return, including
through a `node[_]` field. Call `CloneValue(value)` to get a copy that shares nothing with the Document or with other
results; the copy can be modified freely, and aliases within it are preserved. Every returned slice has capacity equal
to its length, so appending to one result cannot write into another. Other grammars use the existing Node Document and
convert its final result on each call; generic `Node` members may share the mutable Document graph, so call
`Node.Clone` when a detached tree is needed. The conversion fallback applies to `any` and node results, struct fields
requiring SyntaxIR nodes, or actions and predicates that read struct fields.

## Diagnostics, Limits and Combinations

Both `Document.Parse` and `Document.ParseValue` return owned syntax diagnostics: recovered `SyntaxError` values and
their `Expected` and `Messages` slices are copied; ordinary failures are freshly constructed. Mutating a returned
error cannot affect memoized errors or a later parse. Parsing, diagnostic copying and typed publication share one
budget; interruption returns the result type's zero value and clears reusable state as needed for a retry.
Engine-only Document controls such as `EditBatch` are not generated. See the [direct typed measurements](../design/records/044-generated-incremental-and-stream-parsers.md#local-measurements-for-direct-typed-document-matching)
and the [conversion fallback measurements](../design/records/044-generated-incremental-and-stream-parsers.md#local-measurements-for-typed-go-document-conversion).

## TypeScript Documents

With `-lang ts`, `-document` (or `Document: true` in `compiler.TSConfig`) adds `createDocument` to `parser.ts`:

```ts
const { document: doc, error } = createDocument("first draft", { unit: CodePoints });
if (error !== null) throw error;
let result = doc!.parse();
const editError = doc!.edit(6, 11, "revision");
result = doc!.parse();
```

`createDocument(input, options)` copies the input and reads the options once; they become the defaults of
every `parse`, and `document` is null when they are invalid. `edit(start, end, replacement)` follows the Go
contract and returns an `Error` or null; a string replacement is encoded as UTF-8, with lone surrogates as
U+FFFD. In `Bytes` a `Uint8Array` replacement is taken as it is, so edits can join invalid bytes into
characters; in `CodePoints` each invalid UTF-8 byte of the input and of every replacement is replaced by
U+FFFD, as in the Go `Document`. `text()` returns the text as the nodes hold it, and `bytes()` returns a copy of
the current text as UTF-8: the exact bytes in `Bytes`, invalid bytes included, and the normalized text in
`CodePoints`. `parse(options?)` returns the `ParseResult` of `parse`, and with `-types` the Document also has
`parseValue(options?)`. An invocation cannot change the unit or the expectations, and a `parse` or `edit` made
from a trace callback of a parse returns an error. Like the Go `Document`, it keeps the memoized results that
the edits leave valid and the runs of long repetitions, and reuses them in the next `parse` and `parseValue`;
`stats()` returns `{ evaluated, reused, memoHits, resumedElements }` for the last parse, the same counts as
Go's `Stats` after `Parse`; Go's typed `ParseValue` keeps its own memo and can count differently. With
`-observability`, `parse` takes the `trace` and `profile` options of `parse`: a reused result is reported as a
memo hit, so a profile's evaluations and memo hits are the `evaluated` and `memoHits` of `stats()`. A returned tree can share nodes with later results, so do not
mutate a tree while the Document is in use.

## Related Topics

- [Incremental Parsing](incremental.md): the engine's `Document` and what reuse means.
- [The Generated Go API](generated-go-api.md): the entry points that `-document` sits beside.
- [Typed Values in Generated Go](generated-typed-values.md): `-types`, which `Document.ParseValue` needs.
