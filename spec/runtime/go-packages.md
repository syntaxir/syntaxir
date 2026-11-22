# Go Packages

The Go API of SyntaxIR is a set of packages of the module `syntaxir.dev/sxir`. This chapter defines which packages
they are, what each is responsible for and where its contract is defined, how the packages depend on each other as a
program sees it, and the package that compiles grammars.

## Scope and Terminology

The **public packages** are the packages under `syntaxir.dev/sxir/pkg`. The other packages of the module are not
part of the API: the `internal` packages, which Go does not let another module import, the commands under `cmd`, and
the packages of the benchmarks, the examples and the tests of the specification. A package is named by its import
path below without the prefix `syntaxir.dev/sxir/`.

The **runtime side** is the packages that run a parser: `pkg/runtime`, its subpackages and `pkg/bytecode`. The
**compiler side** is the packages that read, check or compile a grammar: `pkg/grammar`, `pkg/grammar/ast`,
`pkg/compiler`, `pkg/lint`, `pkg/sample` and `pkg/editfuzz`.

## Contract

### The Public Packages

| Package | Name | Responsible for | Defined in |
|:--|:--|:--|:--|
| `pkg/grammar` | `grammar` | Grammar sources, loading them into the grammar AST, the errors of loading, validation, formatting and conversion between grammar syntax and JSON | [Grammar Sources](../language/grammar-files.md#grammar-sources), [Formatting](../language/grammar-files.md#formatting) |
| `pkg/grammar/ast` | `ast` | The grammar AST, its source positions and comments, its validation and its JSON form | [Grammar Files](../language/grammar-files.md) |
| `pkg/compiler` | `compiler` | Turning a grammar into something that runs: parsers on the bytecode VM, the reference parser, compiled grammar files, generated Go and TypeScript parsers and output schemas | [Compiling Grammars](#compiling-grammars), [Compiled Grammars](compiled-grammars.md), [Generated Parsers](generated-parsers.md) |
| `pkg/lint` | `lint` | The checks of the linter, their findings and rewrites, and the complexity classification | [Grammar Lint](lint.md) |
| `pkg/sample` | `sample` | Generating inputs that a grammar accepts, near-miss inputs that it rejects, and coverage | [Input Generation](sampling.md) |
| `pkg/editfuzz` | `editfuzz` | Checking incremental parsing against fresh parses with edit sequences, and their saved form | [Edit Fuzzing](edit-fuzzing.md) |
| `pkg/bytecode` | `bytecode` | The instruction set, the module and the compiled-file format, its encoding and its validation | [Bytecode Specification](../bytecode/README.md) |
| `pkg/runtime` | `runtime` | The parser interface, parse options, limits and their errors, the stream iterator and the statistics of a Document | [Parsing](parsing.md), [Incremental Documents](documents.md), [Streaming](streaming.md) |
| `pkg/runtime/input` | `input` | Position units, locations, the text edits of a Document and the kinds of its change notifications | [Positions](parsing.md#positions), [Incremental Documents](documents.md) |
| `pkg/runtime/value` | `value` | Nodes, their traversal, cursors, text and JSON forms, and syntax errors, including the errors that recovery records | [Nodes and Values](nodes.md), [Syntax Errors](parsing.md#syntax-errors) |
| `pkg/runtime/value/impl` | `valueimpl` | The node representation that the runtime and generated Go parsers share. It is not an API to call: programs use the methods of `value.Node` | [Generated Parsers](generated-parsers.md) |
| `pkg/runtime/schema` | `schema` | Output schemas, their JSON form, validation and comparison | [Output Schemas and Structural Queries](schemas-and-queries.md) |
| `pkg/runtime/query` | `query` | Structural queries: patterns, their compilation against an output schema, and matching | [Output Schemas and Structural Queries](schemas-and-queries.md) |
| `pkg/runtime/instrument` | `instrument` | Trace events, call sites and profiles of a parse | [Tracing and Profiling](observability.md) |
| `pkg/runtime/vm` | `vm` | Loading a compiled grammar into a parser and running it on the bytecode VM | [Compiled Grammars](compiled-grammars.md) |
| `pkg/runtime/libgo` | `libgo` | The library of generated Go parsers. It is not an API to call: generated code imports it, or a copy of it | [Generated Parsers](generated-parsers.md) |

The name of each package is the last element of its import path, except that `pkg/runtime/value/impl` is named
`valueimpl`. No two public packages have the same name, so a program can import any of them together without
aliases.

The public packages are the whole Go API: a program MUST NOT depend on a package of the module that this table does
not list. Like the rest of the module at v0.x, a public package may change in any release, and the exported fields of
`pkg/bytecode` describe the instruction set, which changes with its version.

### Runtime and Compiler Sides

A package of the runtime side imports no package of the compiler side. A program that runs parsers without compiling
a grammar, by loading compiled grammars with `vm.LoadParser` or by calling generated Go parsers, therefore does not
link the grammar parser, the front end or the compiler. Generated Go parsers depend only on packages of the runtime
side and on the standard library, as [Generated Parsers](generated-parsers.md#the-generator) defines.

Every parser returns the types of the runtime side, whatever made it: a parser of `compiler.Compile`, one that
`vm.LoadParser` loaded and a generated Go parser give nodes as `*value.Node`, syntax errors as `value.SyntaxErrors`
and limit errors as `*runtime.LimitError`, so code that handles a result does not depend on where its parser came from.

`pkg/grammar/ast` imports only the standard library, so a program can read, build and write grammar ASTs and their
JSON form without the parser or the compiler.

### Compiling Grammars

`pkg/compiler` compiles a grammar given as a [grammar source](../language/grammar-files.md#grammar-sources) or, for
`CompileGrammar`, as a grammar AST.

| Function | Returns |
|:--|:--|
| `compiler.Compile(src, opts...)` | A `runtime.Parser` that runs on the bytecode VM; it is a `*vm.Parser` |
| `compiler.CompileGrammar(g, opts...)` | The parser of `Compile` for a grammar AST, such as one a program builds |
| `compiler.NewReferenceParser(src, opts...)` | The reference parser, which runs the grammar's Core IR as Go closures. The other backends are tested against it. It is the only parser that inserts missing tokens, and its Documents have only the methods of `runtime.Document` |
| `compiler.Marshal(src, mode, opts...)` | A [compiled grammar](compiled-grammars.md) in the mode `mode` |
| `compiler.GenerateGo(src, config, opts...)`, `compiler.GenerateTypeScript(src, config, opts...)` | The files of a [generated parser](generated-parsers.md) |
| `compiler.Generate(src, targets, opts...)` | The files of a generated parser for each target, a `GoConfig` or a `TSConfig`, in order, from one check of the grammar: each equals what `GenerateGo` or `GenerateTypeScript` returns for that target alone, and an error returns no files |
| `compiler.GenerateLibGo(module)`, `compiler.GenerateTypeScriptLib()` | The files of the library of generated parsers alone |
| `compiler.OutputSchema(src, opts...)` | The [output schema](schemas-and-queries.md#output-schemas) of the start rule |

Every function that takes a grammar takes the same options, so one grammar with one set of options is the same
parser in every artifact:

| Option | Meaning |
|:--|:--|
| `compiler.WithStart(name)` | The [start rule](parsing.md#start-rule); an empty name selects the default start rule |
| `compiler.WithSourceInfo()` | Record [source information](nodes.md#source-information) |
| `compiler.WithDefaultValue()` | Return [default values](../language/actions.md#default-values) |
| `compiler.WithOptimize(o)` | The [optimization setting](generated-parsers.md#optimization-settings): the profile `o.Profile` (`compiler.Speed`, `compiler.Memory` or `compiler.Size`), then the values `o.Values`, then the optimizations `o.Off` turned off. No setting changes what a parser returns. Every compile function takes it, and which backends apply it is stated in [Optimization Settings](generated-parsers.md#optimization-settings). An unknown profile or optimization, or a value that an optimization does not take, is an error of every function |

A grammar source that does not load gives the errors of `grammar.Load`, among them `grammar.SyntaxErrors` for grammar
syntax. A grammar that loads but does not check, such as one that refers to a rule that it does not define, gives
`grammar.CheckErrors`, a non-empty list of `*grammar.CheckError`, each with the position `Pos` (an `ast.Pos`, the
zero `Pos` when the error has no position in the source) and the message `Msg`. The functions return the list itself,
which a program obtains with `errors.As`; the list does not unwrap to its elements. Its text is its errors, one per
line, each as `line:column: message` when its position is known and as the message otherwise. `lint` and `editfuzz`
report a grammar that does not check with the same type.

## Compatibility and Support

At v0.x, the packages, their names and their exported API may change in any release. The compiled-file format and
the library protocol of generated parsers have versions of their own, which
[Compiled Grammars](compiled-grammars.md#versions-and-compatibility) and
[Generated Parsers](generated-parsers.md#compatibility-and-support) define.

## Conformance

An implementation of the Go API conforms when the public packages are those of the table, with those names, when the
runtime side imports no package of the compiler side, when `pkg/grammar/ast` imports only the standard library, and
when the contracts of the chapters that the table links hold.
