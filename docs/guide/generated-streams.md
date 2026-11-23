# Streams in Generated Parsers

Generate with `sxir gen -g grammar.sxir -module example.com/parser -stream -o parser`, or set `Stream: true` in
`compiler.GoConfig`. The start rule must have a [`#stream`](../../spec/language/pragmas.md#stream) repetition; otherwise generation
fails. This adds a Go `ParseStream` beside the ordinary `Parse`, which still treats `#stream` as a plain repetition.
In TypeScript, `-stream` adds the push stream `createStream`; see [TypeScript streams](#typescript-streams).

## Parsing a Stream

```go
err := parser.ParseStream(r, func(n *parser.Node) error {
	fmt.Println(n.Kind(), n.Start(), n.End())
	return nil
}, parser.WithUnit(parser.Bytes))
```

`ParseStream` follows the engine's [stream contract](streaming.md): it reads only as much input as matching needs,
hands each element of the start rule's `#stream` repetition to the callback as soon as it matches, and then discards the
element with the input and memo entries before it. Elements are final and own their text. A callback error stops the
parse and is returned unchanged; a non-EOF reader error ends it as well. A nil callback parses the stream without
handing elements over. Recovered diagnostics are returned together as `SyntaxErrors` at the end, and positions and
lines stay absolute. `WithLimits` (its `Compute` unless generated with `-no-limits`), `WithContext` unless generated with `-no-limits` and, with `-observability`, tracing and profiling
apply to the whole invocation; a cancelled or limited stream checks between reads and elements, and a reader blocked
in `Read` is not interrupted. The measurements are in
[design record 044](../design/records/044-generated-incremental-and-stream-parsers.md#local-measurements-for-generated-go-node-streams).

## Pull Iteration and Diagnostic Controls

The package also has the engine's pull iteration and diagnostic controls, with the same contracts as in
[streaming](streaming.md):

```go
it, err := parser.Iterate(r, parser.WithDiagnosticCallback(func(e *parser.SyntaxError) error {
	log.Print(e) // a committed recovery, just before its element
	return nil
}), parser.WithDiagnosticLimit(100))
if err != nil {
	return err // invalid options; r has not been read
}
defer it.Close()
for {
	n, ok, err := it.Next()
	if !ok {
		return err // nil at a clean end, or the result ParseStream would return
	}
	use(n) // nil for an element without a value
}
```

`Iterate` returns a `*StreamIterator` whose `Next` parses only as far as the next committed element; `Close`
abandons the parse without closing `r`, and `Next` then returns `ErrStreamClosed`. `WithDiagnosticCallback`
receives a copy of each committed recovered error before its element, and its error ends the parse without that
element. `WithDiagnosticLimit(n)` keeps only the latest `n` recovered errors and returns `*TruncatedSyntaxErrors`
when it dropped some, so a long faulty stream holds a bounded number of them. Both options are accepted only by
the stream entry points; `Parse`, `ParseRule`, `ParseValue`, `Recognize`, `NewDocument` and the `Document` methods
reject them.

## Typed Elements

With `-types`, the package also has

```go
type StreamValue = ... // the typed value of one element of the #stream repetition
func ParseStreamValue(r io.Reader, emit func(StreamValue) error, opts ...ParseOption) error
func IterateValue(r io.Reader, opts ...ParseOption) (*StreamValueIterator, error) // Next returns StreamValue
```

`ParseStreamValue` behaves like `ParseStream` but passes each element as the value its expression has in the
[Go type mapping](generated-typed-values.md#mapping-grammar-types-to-go) — a rule's declared type, a join, a list, a `*TNode[T]` or a tuple — and the zero value for an element without a value. `StreamValue` is the type of the repeated element,
not of the start rule:

```go
// rule main = item* #stream
// type Pair = { Key string; Value string }
// rule item Pair = k:[a-z]+ "=" v:[0-9]+ "\n" => Pair{Key: text($k), Value: text($v)}
err := parser.ParseStreamValue(r, func(p *parser.Pair) error {
	fmt.Println(p.Key, p.Value)
	return nil
})
```

Each element is converted on its own once it is committed, so values stay valid after the callback returns and share
nothing with later elements. The conversion charges no compute units and observes `WithContext`.

## TypeScript Streams

`sxir gen -lang ts -g grammar.sxir -stream -o parser`, or `Stream: true` in `compiler.TSConfig`, adds
`createStream` to `parser.ts`. JavaScript cannot block in a read, so the TypeScript stream is pushed: the caller
feeds the input chunk by chunk, and each feed returns the elements committed while it ran.

```ts
import { createStream, Bytes } from "./parser/parser.ts";

const { stream, error } = createStream({ unit: Bytes });
if (stream === null) throw error; // invalid options
for await (const chunk of source) { // Uint8Array chunks, such as those of a Node.js readable stream
  const r = stream.feed(chunk);
  for (const n of r.elements) use(n);
  if (r.state === "failed") throw r.error;
}
const r = stream.feed(new Uint8Array(0), true); // the end of the input
for (const n of r.elements) use(n);
if (r.error !== null) throw r.error; // a syntax error, or the recovered SyntaxErrors of a done stream
```

`feed` copies the chunk and parses until it needs input that has not arrived (`"waiting"`) or the parse ends
(`"done"` or `"failed"`). The parse suspends and resumes rather than starting again, so the elements, their positions,
the error and the compute units are those of the Go `ParseStream` of the same input, whatever the chunks. The options
of `createStream` (`unit`, `expectations`, `limits`, `signal`, `computeReport`) apply to the whole stream; a signal
cancels the feed that observes it, and a value thrown from a callback or a signal leaves `feed` and fails the
stream. A feed after the end returns an error and changes nothing.

The diagnostic options of a stream report recovered errors (`#recover`) as they are committed, as
`WithDiagnosticCallback` and `WithDiagnosticLimit` do in Go:

```ts
const { stream } = createStream({
  diagnostic: (e) => { log(e.message); return null; }, // return an Error to fail the stream
  diagnosticLimit: 100, // keep only the latest 100 errors for the result
});
```

The callback runs during the feed that commits the error, before the element after it. With a positive
`diagnosticLimit`, a stream that recovered from more errors ends with `TruncatedSyntaxErrors`, the retained errors
with the `total` and `dropped` counts.

With `-types` as well, `createStreamValue` makes the same stream with typed elements: each feed returns the committed
elements as `StreamValue`, the type of the repeated element in the [TypeScript types](typescript.md#typed-values), as
`ParseStreamValue` does in Go:

```ts
import { createStreamValue } from "./parser/parser.ts";

const { stream } = createStreamValue();
const r = stream!.feed(new TextEncoder().encode("a=1\nbc=22\n"), true);
for (const pair of r.elements) use(pair); // StreamValue, here the grammar's Pair
```

## Related Topics

- [Streaming](streaming.md): the stream contract, shared by the engine and the generated parsers.
- [The Generated Go API](generated-go-api.md): the entry points that `-stream` sits beside.
- [Typed Values in Generated Go](generated-typed-values.md): `-types`, which `ParseStreamValue` needs.
