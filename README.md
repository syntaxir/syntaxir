<div align="center">

<h1>
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="site/static/assets/brand/syntaxir-lockup-horizontal-dark.svg">
  <img src="site/static/assets/brand/syntaxir-lockup-horizontal.svg" alt="SyntaxIR" height="60">
</picture>
</h1>

**One Grammar, Every Parser.**

[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENCE) [![Go Reference](https://pkg.go.dev/badge/syntaxir.dev/sxir/pkg/compiler.svg)](https://pkg.go.dev/syntaxir.dev/sxir/pkg/compiler) [![Go](https://img.shields.io/badge/go-1.27.1+-00ADD8?logo=go&logoColor=white)](go.mod)

[Website](https://syntaxir.dev/) · [Playground](https://syntaxir.dev/playground/) · [Quick start](docs/tutorial/getting-started.md) · [Guides](docs/guide/README.md) · [Specification](spec/README.md) · [Examples](examples/)

</div>

**Never write a parser again.** Describe your language once as a typed grammar, and SyntaxIR builds every parser you
need: generated code or a VM, fast, safe on any input and ready for your editor. Here a whole operator precedence table,
associativity included, is one rule:

```sxir
// calc.sxir
type Num = node[string]
type Bin = { Left Expr; Op node[string]; Right Expr }
type Expr ~ Bin | Num

@start
rule main Expr = #operators {
    skip " "*
    operand num
    operand "(" e:main ")" => $e
    level additive { infix left "+" / "-" => Bin{Left: $lhs, Op: $op, Right: $rhs} }
    level multiplicative { infix left "*" / "/" => Bin{Left: $lhs, Op: $op, Right: $rhs} }
    level power { infix right "^" => Bin{Left: $lhs, Op: $op, Right: $rhs} }
}

rule num Num = ([0-9]+) #atomic
```

```bash
sxir parse -g calc.sxir -f sexpr -i '1 + 2 * 3 ^ 2'
```

```text
Bin{Left: Num("1"), Op: "+", Right: Bin{Left: Num("2"), Op: "*", Right: Bin{Left: Num("3"), Op: "^", Right: Num("2")}}}
```

```bash
sxir parse -g calc.sxir -i '1 + * 2'
```

```text
sxir: 1:5: syntax error: expected "(", [0-9]
```

> [!NOTE]
> SyntaxIR is pre-release: the language, the Go API and the file formats may change. Generated Go is not yet as fast
> as Go's hand-written parsers, and closing that gap is the current work. [Why SyntaxIR](docs/why-syntaxir.md) has every
> measured number, the current limits and how SyntaxIR compares with other tools.

## Why SyntaxIR

- **Runs anywhere, any way.** Any target language, any engine, any optimization. One grammar becomes generated code,
  a VM or an interpreter, tuned for speed, memory or size, and every one of them returns the same tree, the same
  positions and the same errors. Whatever you build, SyntaxIR fits.
- **Unmatched expressive power.** Build values of any type you define, carry state for context-sensitive syntax and
  write a whole operator precedence table as one rule. Recover from errors with a complete tree, and parse endless
  streams as they arrive. If you can describe a language, you can parse it.
- **Performance pushed to the limit.** A multi-stage IR optimizes at every level, from grammar-wide rewrites down to
  the generated code. Incremental parsing reuses everything an edit did not touch, so a fresh tree is ready between
  keystrokes.
- **Safe on any input.** SyntaxIR analyzes your grammar to bound the time and memory a parse can take, and built-in
  compute and depth limits mean hostile input can never hang the parser or blow its stack.
- **A grammar anyone can write.** A grammar is short enough to review, so you can even let an LLM write it. Lint and
  cost classification report problems at their positions, and sampling and edit fuzzing exercise the parser, so
  mistakes surface before any parser ships.
- **Complete support for building your language.** A built-in language server brings completion, diagnostics,
  navigation and formatting to any editor. Trace, profile, lint and debug show exactly what the parser does, so you
  never have to guess.

## Install

```bash
go get syntaxir.dev/sxir                        # library
go install syntaxir.dev/sxir/cmd/sxir@latest    # command-line tool
```

Go 1.27.1 or later. No dependencies outside the standard library.

## Use It

**From Go**

```go
src := grammar.FromString("grammar.sxir", grammarSource)
p, err := compiler.Compile(src, compiler.WithStart("main")) // grammar errors come with positions

node, err := p.Parse(text)                         // positions in code points by default
p.Parse(text, runtime.WithUnit(input.Bytes))       // ... or in UTF-8 bytes
err = p.Recognize(text)                            // check the syntax without building a tree
json.Marshal(node)                                 // nodes encode with types and positions

err = p.ParseStream(r, func(n *value.Node) error { return nil }) // one #stream element at a time

doc, _ := p.NewDocument(text)
doc.Edit(10, 12, "new text") // replace [10, 12)
node, err = doc.Parse()      // reuses rule results the edit did not reach

data, _ := compiler.Marshal(src, vm.CompiledParse) // save; vm.LoadParser(data) skips parsing and checking
```

**From the command line**

```bash
sxir parse -g grammar.sxir -i 'abc=12'           # parse and print the tree (JSON, or -f sexpr)
sxir parse -g grammar.sxir -recognize < input.txt # check the syntax only
sxir explain -g grammar.sxir < input.txt         # which rule calls make up a syntax error
sxir trace -g grammar.sxir -i 'abc=12'           # every rule call as an indented tree (-f json for tools)
sxir profile -g grammar.sxir < input.txt         # cost per rule, with hints on wasted work
sxir lint -g grammar.sxir                        # likely mistakes: dead alternatives, unused captures, ...
sxir lsp                                         # language server for editors (errors, formatting, rename, ...)
sxir fmt -w grammar.sxir                         # format in place, comments kept
sxir compile -g grammar.sxir -o grammar.sxirc    # precompile
sxir gen -g grammar.sxir -module example.com/calc -o calc  # generate a standalone Go parser
sxir gen -g grammar.sxir -module example.com/calc -types -o calc # ... with Go types for the grammar's types
sxir gen -g grammar.sxir -module example.com/calc -recognize -o calc # ... with Recognize, which checks the syntax without a tree
sxir gen -lang ts -g grammar.sxir -o ts-parser    # generate a TypeScript parser into a directory
sxir gen -lang ts -g grammar.sxir -types -o ts-parser # also emit types.ts and parseValue
sxir gen -g grammar.sxir -module example.com/calc -max-depth 600000 -o calc # set its nested rule-call default
sxir sample -g grammar.sxir -n 20 -coverage      # generate inputs the grammar accepts (tests, fuzz seeds)
sxir edit-fuzz -g grammar.sxir -minimize -save-failures out  # check Document edits against fresh parses
```

Run `sxir` without arguments for every command and flag.

## Ready-Made Parsers

[alpha-parsers](https://github.com/syntaxir/alpha-parsers) (a separate repository) has parsers for common languages, written in SyntaxIR, generated into Go and tested against the
languages' conformance suites and reference implementations. They are packages of one module that depends only on the
standard library and SyntaxIR's runtime and library packages. Use them from a checkout of that repository; `go get` does not fetch
them yet (see [Ready-Made Parsers](docs/guide/ready-made-parsers.md)):

```go
import "syntaxir.dev/alpha-parsers/json"

v, err := json.Decode(`{"name": "sxir", "tags": ["peg", "operators-rule"]}`)
```

| Language | Module | Specification |
|:--|:--|:--|
| CEL | [`syntaxir.dev/alpha-parsers/cel`](https://github.com/syntaxir/alpha-parsers/tree/main/cel/) | The CEL language definition |
| CSV | [`syntaxir.dev/alpha-parsers/csv`](https://github.com/syntaxir/alpha-parsers/tree/main/csv/) | RFC 4180 |
| CUE | [`syntaxir.dev/alpha-parsers/cue`](https://github.com/syntaxir/alpha-parsers/tree/main/cue/) | The CUE language specification |
| DuckDB SQL | [`syntaxir.dev/alpha-parsers/duckdb`](https://github.com/syntaxir/alpha-parsers/tree/main/duckdb/) | DuckDB 1.5.6's SQL dialect |
| Go | [`syntaxir.dev/alpha-parsers/golang`](https://github.com/syntaxir/alpha-parsers/tree/main/golang/) | The Go specification (go1.27), as `go/parser` parses it |
| JSON | [`syntaxir.dev/alpha-parsers/json`](https://github.com/syntaxir/alpha-parsers/tree/main/json/) | RFC 8259 |
| Python | [`syntaxir.dev/alpha-parsers/python`](https://github.com/syntaxir/alpha-parsers/tree/main/python/) | Python 3.14 |
| TypeScript | [`syntaxir.dev/alpha-parsers/typescript`](https://github.com/syntaxir/alpha-parsers/tree/main/typescript/) | TypeScript 5.9 and TSX |
| XML | [`syntaxir.dev/alpha-parsers/xml`](https://github.com/syntaxir/alpha-parsers/tree/main/xml/) | XML 1.0 (Fifth Edition), non-validating |
| YAML | [`syntaxir.dev/alpha-parsers/yaml`](https://github.com/syntaxir/alpha-parsers/tree/main/yaml/) | YAML 1.2.2 |

## Examples

Small grammars with tests that show the features live in [examples/](examples/): a calculator (Operators Rule and
left-recursive), an indentation-based outline format and a small programming language. The complete parsers of
[alpha-parsers](https://github.com/syntaxir/alpha-parsers), such as the ones for **Go** and **Python 3**, are the larger examples.

## Documentation

| | |
|:--|:--|
| [Why SyntaxIR](docs/why-syntaxir.md) | What SyntaxIR aims to be, what it offers today, its limits, and how it compares with other parser tools |
| [Tutorial](docs/tutorial/README.md) | Ten lessons from a first grammar to a shipped, tested and debugged parser |
| [Grammar ASTs and JSON](docs/guide/grammar-asts.md) | Constructing and validating grammars, structured error paths and JSON compatibility |
| [Guides](docs/guide/README.md) | Trees and actions, expressions, errors and recovery, context-sensitive parsing, running parsers, using the ready-made parsers, compiled grammars, code generation (Go and TypeScript), streaming, incremental parsing, debugging and profiling, linting grammars, testing grammars, sampling inputs and fuzzing, editor support |
| [Cookbook](docs/cookbook/README.md) | Short, complete recipes for concrete tasks: a configuration file into Go structs, a filter language, errors with a caret, several errors at once, parsing every keystroke, shipping a generated parser, testing against a grammar |
| [Editor support](docs/guide/editor-support.md) | `sxir lsp` and the VS Code extension: errors as you type, formatting, navigation, hover with inferred types, rename, completion |
| [Playground](docs/guide/playground.md) | Try grammars in the browser, inspect parse results and use the WebAssembly API |
| [Website maintenance](docs/site.md) | Build, test and publish the documentation site and playground |
| [Language specification](spec/README.md) | The SyntaxIR grammar language |
| [Development guide](docs/development.md) | Repository layout, architecture, language support and tests |
| [Releasing](docs/releasing.md) | What a release promises, supported platforms and Go versions, and how a release is tagged |
| [Bytecode specification](spec/bytecode/README.md) | Instruction set and VM semantics, for porting the runtime |
| [Benchmarks](docs/benchmarks.md) · [Performance](docs/performance.md) · [Optimization catalog](docs/optimizations/README.md) | How fast, and how it got there |
| [Design](docs/design/README.md) · [Design records](docs/design/records/) | How it is built, and the decisions and their rationale |
| [Project strategy](docs/strategy.md) | Target users, competitive position, tracks and measurable targets |

## Contributing

Bug reports, ideas and pull requests are welcome. Run `go test ./...` before sending changes, and follow the
[commit message guidelines](docs/commit-messages.md). Report security vulnerabilities privately, as the
[security policy](SECURITY.md) describes.

## Sponsoring

If SyntaxIR saves you time, you can [buy the project a coffee through GitHub Sponsors](https://github.com/sponsors/syntaxir).

## Citing SyntaxIR

If you use SyntaxIR in research, cite it as below. GitHub's **Cite this repository** button gives the same entry from
[CITATION.cff](CITATION.cff) in other formats.

```bibtex
@software{syntaxir,
  author = {Furukawa, Arata},
  title  = {{SyntaxIR}},
  url    = {https://github.com/syntaxir/syntaxir},
  year   = {2026}
}
```

## Maintainer

SyntaxIR is maintained by Arata Furukawa ([@ornew](https://github.com/ornew), old.river.new@gmail.com). Report security
vulnerabilities through the [security policy](SECURITY.md), not by email.

## License

[Apache License 2.0](LICENCE)
