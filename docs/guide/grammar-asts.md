# Building Grammars as ASTs and JSON

Package `ast` (`syntaxir.dev/sxir/pkg/grammar/ast`) is the grammar AST: use it when a tool builds
a grammar in Go or reads its JSON representation. Package `grammar` (`syntaxir.dev/sxir/pkg/grammar`)
loads a grammar source in grammar syntax or JSON into the AST, and validates, formats and converts
it; the specification defines [grammar sources](../../spec/language/grammar-files.md#grammar-sources),
[validation](../../spec/language/grammar-files.md#validation-of-a-grammar-ast) and
[formatting](../../spec/language/grammar-files.md#formatting). `compiler.CompileGrammar` checks a grammar and returns a parser; Go and TypeScript generation use the
same compiler.

## Load a Grammar Source

A grammar source is one named input, made from bytes, a string, a reader or a path in a file
system:

```go
g, err := grammar.Load(grammar.FromFS(os.DirFS("grammars"), "json.sxir"))
if errors.Is(err, grammar.ErrCompiled) {
    // A compiled grammar (.sxirc) holds bytecode, not the grammar.
}
```

The encoding is grammar syntax or JSON. `As` sets it explicitly, as in
`grammar.FromString("", text).As(grammar.EncodingJSON)`; otherwise the extension of the name decides
(`.sxir` or `.json`), and then the content, which is JSON when its first character other than white
space is `{`, and `Source.Decide` reports the encoding that Load will use. A reader is read in full when the source is first loaded, and the source keeps what it
read, so it can be loaded again. JSON is decoded strictly: an unknown field, a duplicate key or
trailing data is an error. Load's errors carry source positions but not the name of the source, so
a tool that reports them adds `Source.Name`. `grammar.Convert(source, grammar.EncodingJSON)` loads a
source and writes it in the encoding given, here JSON, whatever the encoding of the source.

A source in grammar syntax with errors gives a `grammar.SyntaxErrors`, the list of its errors, each
with a position, an end and a message. `errors.As` obtains the list, for example to place a
diagnostic for each error in an editor:

```go
src := grammar.FromString("g.sxir", "rule main = \"a\" /\n")
_, err := grammar.Load(src)
var errs grammar.SyntaxErrors
if errors.As(err, &errs) {
    for _, e := range errs {
        fmt.Printf("%s:%d:%d: %s\n", src.Name(), e.Pos.Line, e.Pos.Col, e.Msg)
    }
}
```

Lines and columns are 1-based, and columns count characters, as do the positions of the AST.
`End` is the position just after the text an error covers, such as an invalid escape sequence, and
the zero `ast.Pos` when the error is at a point, such as a missing token. The lexical errors come
first, then the others in source order, and a source with more than 100 errors ends the list with
one that says `too many errors`. The error text is the errors' `line:column: message`, one per line.
The list is the error itself and does not unwrap to its elements, so ask `errors.As` for
`grammar.SyntaxErrors`, not `*grammar.SyntaxError`.

## Validate a Hand-Built Grammar

`grammar.Validate(g)` checks the structure before compilation or formatting.
It returns the first `*ast.ValidationError` without changing the AST:

```go
g := &ast.Grammar{File: ast.File{Decls: []ast.Decl{
    &ast.RuleDef{Name: "main", Expr: &ast.Optional{}},
}}}
err := grammar.Validate(g)
var problem *ast.ValidationError
if errors.As(err, &problem) {
    fmt.Println(problem.Path) // $.file.decls[0].expr.expr
}
```

Import `errors`, `fmt`, `syntaxir.dev/sxir/pkg/grammar` and `syntaxir.dev/sxir/pkg/grammar/ast`
for this snippet. `grammar.Validate` is `ast.Validate`.
`Path` uses JSON field names and zero-based indexes. `Pos` is the closest known
diagnostic position. Source-parsed grammars carry source spans and comment
ownership in `SourceInfo`; hand-built nodes may leave those fields zero-valued.
`Msg` describes the problem.

For large grammars, `ast.ValidateContext(ctx, g)` performs the same
structural checks and deterministic first-error traversal cooperatively. If it
observes cancellation, it returns `ctx.Err()` instead of a partial validation
diagnostic; validation does not modify the AST. Cancellation is not a hard
latency bound: individual string checks and sorting are uninterruptible, and
character ranges are checked in chunks of at most 256 ranges between polls.

Validation rejects a nil grammar, missing required children, nil entries in
node collections, interfaces holding typed nil pointers, unsupported embedded
node types, pointer cycles and malformed
repetition and character-range bounds. A repetition's minimum must be nonnegative; a finite
maximum must be at least the minimum, and an unbounded repetition has the
maximum -1, as documented by `ast.Repeat`; any other negative maximum is
rejected. Shared subtrees and genuinely nil optional
children remain allowed.

Validation also rejects every tree that grammar syntax cannot write, so that
`grammar.Format` of a valid AST always parses. A hand-built tree therefore
follows the shapes the parser produces: a sequence or choice has an element and
a join or meet type two, a block is the whole action of a rule, an inline
action, a predicate or an Operators Rule line, a reference to a lambda
parameter is an `ast.CaptureRef` with `Param` set, and the only pragma
attributes are `#ignorecase` and `#missing`. The [validation
table](../../spec/language/grammar-files.md#validation-of-a-grammar-ast) lists
every rule.

A character range's `Lo` and `Hi` must be Unicode scalar values (0 through
0x10FFFF except 0xD800–0xDFFF), with `Lo <= Hi`, and a class has at least one
range. A valid range can span the surrogate interval; overlapping ranges retain
their existing meanings. Invalid endpoints are reported at paths such as
`$.file.decls[0].expr.ranges[0].lo`. This also applies to JSON intake,
compilation and generation. Remove unreachable surrogate exclusions from
older grammars and recompile AST-bearing saved parsers that contain them.

Every string of the AST is valid UTF-8: literal values, string constants, string arguments of
attributes and comments, including the comment tokens of the source layout. JSON and grammar
text cannot hold anything else, so validation rejects a Go string such as `"\xc3"`, at the path
of the string, and both round trips are exact for every valid AST. To match an invalid input byte, write the literal `"\uFFFD"` (`"�"`): it matches each
invalid byte of the input, in both position units and every backend. String constants in actions
are separate from matching literals.

The text of a string node follows the input: byte positions preserve consumed source bytes, while code-point
positions return decoded text. For example, the literal `"�"` can match either a valid U+FFFD or
an invalid input byte, but byte-mode `text($capture)` distinguishes their spellings.

This is structural validation. It does not resolve names, check action types,
validate attributes or enforce Operators Rule semantics; compile the grammar for those
checks. It also does not check source layout, except that every string in it must be
valid UTF-8, or impose size limits. `grammar.Format` and `ast.MarshalJSON` expect structurally
valid input; call `Validate` or `ValidateContext` first for ASTs your tool constructs.
Both validation functions walk iteratively, though later formatting,
serialization and compilation have their own nesting costs. Do not mutate the
AST concurrently with these calls.

Integer constants use Go's native `int` range. Formatting the minimum integer
writes it as one value, such as `-9223372036854775808` for a 64-bit `int`, which
the parser reads back as the same constant.

## Print a Simplified Syntax View

Use `grammar.FormatSyntax(g)` for documentation that shows the grammar's
recognition syntax. It leaves the input AST unchanged and omits Type Declaration
Statements, result types, Value Actions, metadata attributes, captures and
`#recover` wrappers. It removes Rule Expression groups whose parentheses are not
needed for precedence or layout, while retaining parentheses required to express grouping
or multiline layout. Predicate Actions and recognition-relevant pragmas,
including `#repeat`, `#atomic` and `#stream`, remain. Predicates that refer to
captures or values/types produced by omitted syntax may not work in the
projection; use the full grammar for those cases. `grammar.Format(g)` continues
to print the full grammar.

The command-line form writes to standard output:

```bash
sxir fmt -syntax grammar.sxir
```

`-syntax` cannot be combined with `-w`, `-l` or `-check`. Comments on retained forms
follow the formatter's layout rules; comments attached only to omitted forms
may be omitted.

## Format With Cancellation

Use the context-aware formatting functions when a large AST may need to be
abandoned:

```go
formatted, err := grammar.FormatContext(ctx, g)
if err != nil {
    return err
}
```

`FormatExprContext`, `FormatActionExprContext` and `FormatTypeContext` provide the
same behavior for individual AST values. They produce the same text as
`Format`, `FormatExpr`, `FormatActionExpr` and `FormatType`. If cancellation is
observed, the result is an empty string and the context error; partial text is
not returned. Keep the AST immutable for the duration of formatting.

Cancellation is cooperative. The formatter checks while traversing the AST,
processing layout and keyword ordering, and constructing output. Individual
allocations, concatenations and string comparisons can finish before a check,
so these functions do not guarantee a wall-clock deadline.

## Collect Metadata With Cancellation

For large grammar ASTs, use the context-aware collection methods:

```go
rules, err := g.RulesContext(ctx)
if err != nil {
    return err
}
comments, err := g.AllCommentsContext(ctx)
if err != nil {
    return err
}
```

`TypesContext` and `Attribute.ExpressionArgsContext` provide the same contract
for type definitions and expression-valued attribute arguments. On cancellation,
each method returns a nil slice and `ctx.Err()` rather than partial results.
Successful results preserve the corresponding ordinary method's contents and
ordering: rules and types stay in declaration order, expression arguments keep
positional order followed by sorted keyword names, and comments follow the
existing `AllComments` traversal. The returned slice is newly collected, but
its elements retain their original AST pointers; collection does not modify the
AST. Do not mutate the grammar or attribute concurrently with collection.

Cancellation is cooperative rather than a hard latency bound. Polls cover
collection, recursive traversal and keyword sorting, but an individual
allocation or string comparison may finish before cancellation is observed.

## Load JSON

`ast.UnmarshalJSON(data)` accepts exactly one complete JSON value,
followed by optional whitespace, and runs structural validation before
returning a grammar. It rejects JSON `null` as a grammar, trailing values or
garbage, and numeric fields that overflow their Go destination type.
For example, a null struct field reports
`$.file.decls[0].type.fields[0]` rather than panicking during compilation.

The representation uses the AST fields' `json` tags. Each interface node has
an `"@type"` member, such as `"RuleDef"` or `"Literal"`. Parsed source metadata
is serialized under `source` (including `file.source` and `file.pkg.source`) or `skipSource`; formatter layout
and exact comment tokens are omitted. Unknown fields are ignored and duplicate object keys
follow `encoding/json`'s behavior. Use `ast.DecodeJSON(data)` when the
input should reject unknown fields and duplicate keys at every object. Its
`*ast.JSONDecodeError` reports a JSON path and zero-based byte offset for
those strictness errors; malformed JSON wraps the standard syntax error. Both
entry points check one complete value and run structural validation, while
`UnmarshalJSON` keeps the standard signature and is permissive: it ignores unknown fields.

Source `@` attributes use `Attributed` and `Attribute`: `args` is an ordered list
of integers, strings, booleans, null slots or expression nodes, and `kwArgs`
maps argument names to the same value forms. A string argument is a Go
`string`; a case-sensitive `*ast.Literal` argument is an expression, which
source writes in parentheses and reads back as a `Group` around the literal. For example, `@custom(nil, 2)`
preserves `[null, 2]`, whereas `@custom(2,)` and `@custom(2)` both preserve `[2]`. Integer argument
values, including the native minimum, survive source formatting and parsing. Keyword arguments
follow positional arguments in source. Strict JSON decoding also rejects
repeated keyword keys.

## Read Source Metadata

Source-bearing grammar nodes embed `ast.SourceInfo` and satisfy
`ast.SourceNode`, so tools can inspect metadata without a type switch:

```go
func report(n any) {
    if sourced, ok := n.(ast.SourceNode); ok {
        source := sourced.Source()
        fmt.Println(source.Pos, source.End, source.LeadingComments)
        fmt.Println("diagnostic token:", source.Diagnostic())
    }
}
```

`Pos` and `End` are the node's half-open source span; `End` is exclusive. The
span excludes leading and trailing comments owned at its boundaries, but
includes comments inside that continuous source interval.
`LeadingComments` holds normalized text, each entry in `TrailingComments` is
one comment at the end of the node's line, and each entry in
`LeadingDetachedComments` is one paragraph. Comment markers are removed;
line endings and content whitespace are retained according to the grammar
comment-normalization contract. `DiagnosticPos` preserves a diagnostic token
that differs from the span start; `Diagnostic()` returns it when valid and
otherwise returns `Pos`.

The package clause is `File.Pkg`: `File.Pkg.Name` is the package name, and `File.Pkg.SourceInfo`
holds the clause metadata. `File.Decls` holds the declarations in order, each a `*TypeDef`, a `*TypeAlias`
or a `*RuleDef`; `Grammar.Rules()` and `Grammar.Types()` list the rule and type declarations. An Operators Rule's `SkipSource` holds metadata for the whole
skip clause, from its `skip` keyword through the expression. `Grammar.Clone()`
returns an independent copy of the supported grammar AST and attribute
argument values, including detached-comment slices.
Shared AST subtrees remain shared within the clone, and AST pointer cycles are
preserved for `grammar.Validate` to report. Arbitrary application objects or
cyclic attribute maps and slices are outside the clone contract.

`grammar.Load` returns the source AST with its source metadata. Compilation
does not change it, and a compiled parser keeps its own copy of the grammar, so a
tool may edit an AST after compiling it without changing the parser. Use
`g.Clone()` for an independent editable copy.

JSON stores normalized source ownership, not raw formatter trivia or layout.
An AST decoded from JSON therefore cannot promise lossless source formatting.
Keep the original parsed AST when a tool needs to preserve exact comment
placement.

For in-memory formatting, `SourceInfo.Layout` is a raw-only adjunct. Its
`Before` field retains the `ast.Comment` tokens at that syntax boundary;
`PreviousLine` and `NextLine` preserve their line relationship to surrounding
syntax, and `After` retains the comments between a list element and the comma
that follows it. A nil layout uses canonical formatting. `Grammar.Clone()` copies the
layout and its comment tokens. JSON omits layout, and layout alone does not
cause a `source` object to be emitted. When layout is present, `grammar.Format`
uses the retained comment tokens rather than the normalized comment strings,
so editing `LeadingComments` or `TrailingComments` alone does not change the
formatted comment. Edit the raw layout tokens or reparse the source to
resynchronize them. Layout is one of several places that keep comment tokens:
statements, struct fields and operators lines keep theirs in `LineBreak`
values, declaration headers in `File.HeaderComments`, the comments between
the name and the type of a field in `Field.HeaderTokens`, those inside the `#operators {`
of an Operators Rule in `OperatorsRule.OpenComments`, and those inside the
parentheses of a level call in `Ref.LevelGaps`. Every comment of a parsed
source is kept once among them, and `Grammar.AllComments` returns each once,
so formatting a parsed grammar keeps every comment with the node that owns it.

Each comment has at most one owner. The comments inside a declaration header,
from the keyword to the `=` or `~` that ends it, belong to the declaration's
`HeaderComments`; the comments after the `=` or `~` belong to the type,
expression or value that follows it. The full rules are in
[Comment Grouping and Ownership](../../spec/language/grammar-files.md#comment-grouping-and-ownership).

Direct grammar JSON includes normalized source metadata. A compiled grammar
(`.sxirc`) holds bytecode only, with no tree, source positions or comments.

Source-only syntax has dedicated AST nodes. An explicit Rule Expression grouping is `Group{Expr}`; formatting and JSON preserve the group, while compilation normalizes it away before matching. The source-aware AST retains the group body, closing-delimiter comments and layout. Parenthesized type and action expressions likewise use source-only `TypeGroup{Type}` and `ActionGroup{Action}` nodes; `TypeGroup` implements `TypeExpr`, and `ActionGroup` implements `ActionExpr`, the interface of Action Expressions. These groups preserve their delimiters and enclosed comments in source formatting. Their `OpenBreak`, `BodyBreak`, `CloseBreak` and `OneLine` fields retain raw line placement; `TupleLit`, `Call`, `Constructor` and `ListConstructor` keep the comments at their delimiters in `OpenBreak`, `BodyBreak` and `CloseBreak`. A tuple, call, constructor, list constructor, tuple type, type application, attribute or lambda writes one element per line when it has a trailing comma (`TrailingComma`) or a comment that needs its own line, and is otherwise written on one line; the comma of a one-element tuple does not count. Compilation and generators lower the wrappers transparently before execution. JSON retains normalized source positions and ownership but omits formatter layout and exact raw comment tokens. A braced action sequence is `Block{Statements}`. A single-statement Predicate Action remains in `Predicate.Action`; a nonempty multi-statement block is represented by `Block{Statements}` and executes its statements in order, like adjacent Predicate Actions. Empty predicate blocks and braced Value Actions can be parsed, formatted and serialized, but semantic checking rejects them as unsupported.

Source `#` pragmas use dedicated expression nodes: `#repeat` is `Repeat`,
`#atomic` is `Atomic`, `#recover` is `Recovery`, and `#stream` is `Stream`.
Their payloads follow each pragma's own syntax. Compilation lowers recovery
and streaming into its executable representation without changing the source
AST. An `InlineAction` node is formatted and serialized as written; compilation
lowers it to a hidden rule called at its position. The grammar of a parser
(`Parser.Grammar`) and the formatter keep the inline action as written.

After loading, use `compiler.CompileGrammar(g, compiler.WithStart("main"))` to perform semantic checking.
Compile and the generators also run structural validation themselves. A
compiled file holds no AST; its trust and bytecode validation rules are in
[compiled grammars](compiled-grammars.md).
