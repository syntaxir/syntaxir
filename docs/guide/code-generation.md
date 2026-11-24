# Generating Parsers With `sxir gen`

`sxir gen` turns a grammar into Go source files: a parser package you commit to your repository, build with your program, and
call like any other package. By default the generated Go package depends only on the packages under `syntaxir.dev/sxir/pkg/runtime` (`value`, `input`, `instrument`, `value/impl` and the runtime package itself), which define the
nodes and errors that the compiled parsers return too, and on the standard library: it carries a copy of the SyntaxIR library
of generated parsers in its `internal/libgo` directory, and it does not load or compile the grammar at run time.

This guide shows how to generate a parser, what the generator supports compared with the engine (the `runtime.Parser` of
[Running Parsers](running-parsers.md)), how to keep generated code up to date, and when it is the right choice. The API
of the generated Go package is in [The Generated Go API](generated-go-api.md), and the options that add typed values,
documents and streams have guides of their own, linked below. `sxir gen -lang ts` generates
TypeScript modules with the same behavior instead; it has a guide of its own, [TypeScript parsers](typescript.md).

- [Why generate](#why-generate)
- [Quick start](#quick-start)
- [The `sxir gen` command](#the-sxir-gen-command)
- [What is supported](#what-is-supported)
- [Keeping generated code up to date](#keeping-generated-code-up-to-date)
- [Trade-offs](#trade-offs)
- [Recipes](#recipes)

## Why Generate

| | Compiled parser (`compiler.Compile`) | Generated parser |
|:--|:--|:--|
| Speed | Bytecode VM by default | Generated Go takes <!-- fig:generated-vs-closure-codepoints -->0.44–0.61×<!-- /fig --> closure time in CodePoints and <!-- fig:generated-vs-closure-bytes -->0.50–0.66×<!-- /fig --> in Bytes; direct typed values take <!-- fig:typed-vs-parse-time -->0.69–0.98×<!-- /fig --> generated `Parse` time ([benchmarks](../benchmarks.md), [analysis](../performance.md#where-syntaxir-stands)) |
| Dependencies | `syntaxir.dev/sxir` | `syntaxir.dev/sxir/pkg/runtime` and its subpackages only (the library is copied into the package; `-lib import` imports it from `syntaxir.dev/sxir/pkg/runtime/libgo` instead) |
| Grammar at run time | Loaded and compiled (or loaded from `.sxirc`) | Gone: it is compiled into Go code |
| Grammar changes | Edit the grammar, restart | Regenerate and rebuild |
| Recognition (no tree) | `Recognize` | `Recognize`, with [`-recognize`](generated-go-api.md#recognition--recognize) |
| Streaming, incremental, backends | Yes | Go Node `ParseStream` with `-stream` (`-types` adds `ParseStreamValue`); Go Node `Document` with `-document`; `-types` adds `Document.ParseValue`, using direct incremental matching for eligible grammars and Node conversion otherwise. No backend choice (see [what is supported](#what-is-supported)) |

Generate a parser when the grammar is fixed, the parser is on a hot path or in a small binary, or you want no
dependency on SyntaxIR at run time. Use the engine when the grammar is data. For streams of Nodes, generate a Go
`ParseStream` with `-stream`; for incremental Node parsing, generate a Go `Document` with `-document`. The engine offers
the broader stream and Document APIs.
A good deal of the time you can use both: the engine in tools and tests, and the generated code in production
(see [Recipes](#recipes)).

The generated parser returns the **same tree and the same errors** as the engine, so switching between them does not
change what your code sees. The test suite checks this for every grammar in `examples/`
([design record 008](../design/records/008-code-generation.md)).

## Quick Start

A complete, runnable setup. The grammar, `pairs.sxir`, is the one used in the [Running Parsers](running-parsers.md):

```sxir
// pairs.sxir
type Pair = { Key node[string]; Value node[string] }

@start(default)
rule main = ws ps:pair+ $$ => $ps
rule pair Pair = k:key "=" v:value ";"? ws => Pair{Key: $k, Value: $v}
@start
rule key = [a-zあ-ん]+ #atomic
rule value = [0-9]+ #atomic
rule ws = [ \t\n]*
```

The layout is a module with the grammar next to the code that generates from it, and the generated parser in its own
package (see [why its own package](#one-grammar-one-package)):

```text
example.com/pairs
├── go.mod
├── pairs.sxir
├── generate.go
├── main.go
└── pairsparser/        # created by go generate
    ├── parser.go
    ├── api.go
    └── internal/libgo/   # and, with -types, types/types.go
```

Create the module and record `sxir` as a tool dependency of it, so that `go generate` runs the pinned version and
nobody needs a separate install:

```bash
go mod init example.com/pairs
go get -tool syntaxir.dev/sxir/cmd/sxir
```

This adds a `tool syntaxir.dev/sxir/cmd/sxir` line to `go.mod`. Then declare the generation step with a
`go:generate` directive; `sxir gen` creates the output directory:

```go
// generate.go
package main

//go:generate go tool sxir gen -g pairs.sxir -module example.com/pairs/pairsparser -o pairsparser
```

```bash
go generate ./...
```

`-module` is the import path of the generated package, which the copy of the library in `pairsparser/internal/libgo`
is imported by; `-o` is the output directory, which `sxir gen` creates. The result is `pairsparser/parser.go`, with the
code generated from the grammar, `pairsparser/api.go`, which re-exports the names the users of the parser need
(`Node`, `SyntaxError` and so on), the library copy and, with `-types`, `pairsparser/types/types.go`, the package of the Go types of the grammar (the `-module` path is then required). `parser.go` starts with:

```go
// Code generated by sxir v0.1.0. DO NOT EDIT.

// Package pairsparser is a parser generated from a SyntaxIR grammar.
package pairsparser
```

The first line of every generated file names the version of `sxir` that wrote it (`(devel)` for an `sxir` run from a
checkout with `go run`; `go build ./cmd/sxir` in a checkout stamps a pseudo-version of its commit instead), so that a parser can be checked against security advisories, which name affected versions the
same way. One version always writes the same header, so regeneration stays reproducible.

Now use it:

```go
// main.go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"example.com/pairs/pairsparser"
)

func main() {
	node, err := pairsparser.Parse("abc=12; あい=3")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(node)
	for _, pair := range node.Elems() {
		key := pair.Field("Key").(*pairsparser.Node)
		fmt.Printf("%s %q at %d-%d\n", pair.Type(), key.Text(), key.Start(), key.End())
	}

	// Positions in UTF-8 bytes.
	node, _ = pairsparser.Parse("あい=3", pairsparser.WithUnit(pairsparser.Bytes))
	fmt.Println(node.Elem(0).End())

	// Any rule marked @start can be the start rule.
	node, err = pairsparser.ParseRule("key", "abc")
	fmt.Println(node, err)
	_, err = pairsparser.ParseRule("nope", "abc")
	fmt.Println(err)

	// Errors have the same fields as the engine's, but are types of the generated package.
	_, err = pairsparser.Parse("abc=x")
	var se *pairsparser.SyntaxError
	fmt.Println(errors.As(err, &se), se.Line, se.Col, se.Expected, err)

	out, _ := json.Marshal(node)
	fmt.Println(string(out))
}
```

```bash
go run .
```

```text
[Pair{Key: "abc", Value: "12"} Pair{Key: "あい", Value: "3"}]
Pair "abc" at 0-3
Pair "あい" at 8-10
8
"abc" <nil>
rule nope is not defined
true 1 5 [] 1:5: syntax error
{"start":0,"end":3,"value":"abc"}
```

The output is what the engine prints for the same grammar and input, except that the syntax error lists no expected
items: a generated parser records them only when it is generated with `-expectations` and called with
`WithExpectations()` (see [recording expectations](error-messages.md#recording-expectations)).

## The `sxir gen` Command

```bash
sxir gen -g <grammar> -module <import path> -o <directory> [-lang go] [-pkg <package>] [-lib embed|import|only] [-s <rule>] [-types] [-document] [-stream] [-recognize] [-typed-only] [-recognize-only] [-expectations] [-observability] [-no-limits] [-source-info] [-default-value] [-nodoc] [-max-depth <n>] [-optimize speed|memory|size] [-opt <name[=count],...>] [-no-opt <name,...>] [-print-before <pass,...>] [-print-after <pass,...>] [-print-changed] [-verify-each] [-stats] [-time-passes] [-opt-bisect-limit <n>] [-print-pipeline]
sxir gen -lang ts -g <grammar> -o <directory> [-lib embed|import|only] [-lib-module <specifier>] [-s <rule>] [-types] [-recognize] [-expectations] [-observability] [-no-limits] [-source-info] [-default-value] [-max-depth <n>] [-optimize speed|memory|size] [-opt <name[=count],...>] [-no-opt <name,...>] [-print-before <pass,...>] [-print-after <pass,...>] [-print-changed] [-verify-each] [-stats] [-time-passes] [-opt-bisect-limit <n>] [-print-pipeline]
sxir gen -lib only -o <directory> [-lang go|ts]
```

| Flag | Default | Meaning |
|:--|:--|:--|
| `-lang` | `go` | The language: `go`, or `ts` for TypeScript (see [TypeScript parsers](typescript.md)) |
| `-g` | (required, except with `-lib only`) | The grammar: `.sxir` or `.json` |
| `-module` | (required for Go with `-lib embed`) | The import path of the generated package. The copy of the library is imported by `<module>/internal/libgo`. With `-lib import`, `-pkg` can stand in for it, since the package then needs no import path of its own. |
| `-pkg` | last element of `-module`, without a major version suffix (`example.com/m/v2` gives `m`) | The package name of the generated code. It must be a valid Go identifier; a module path whose last element is not one (`my-parser`) needs `-pkg`. |
| `-lib` | `embed` | How the generated code reaches the library: `embed` writes a copy (Go: `internal/libgo`; TypeScript: `lib.ts`), `import` imports it (Go: `syntaxir.dev/sxir/pkg/runtime/libgo`; TypeScript: the module of `-lib-module`), and `only` writes just the copy, to refresh it without regenerating the parser. `only` takes only `-o`; a grammar or any other generation flag is an error |
| `-lib-module` | (required for TypeScript with `-lib import`) | The module specifier that the generated TypeScript imports the library from, for example `../shared/lib.ts`. An error otherwise, and for Go |
| `-s` | the default start rule | The start rule of the generated `Parse` function |
| `-o` | (required) | The output directory (it is created). Generated files that the new run does not write again are removed, but `-lib only` never removes `parser.go`, `api.go`, `types/types.go`, `parser.ts` or `types.ts`, and no file without the `// Code generated by sxir <version>. DO NOT EDIT.` header is replaced or removed: `sxir gen` stops with an error instead. |
| `-types` | off | Also generate the Go types for the grammar's types, in the package `<module>/types`, and `ParseValue` (see [typed values](generated-typed-values.md)) |
| `-document` | off | Also generate the incremental Node `Document` API (`createDocument` in TypeScript); with `-types`, it also exposes `Document.ParseValue` (`parseValue` in TypeScript) (see [incremental documents](generated-documents.md)) |
| `-stream` | off | Also generate the Go Node `ParseStream`, or the TypeScript push stream `createStream` (see [streams](generated-streams.md)); the start rule must have a `#stream` repetition; in Go, with `-types` it also generates `ParseStreamValue`, and in TypeScript `createStreamValue` |
| `-typed-only` | off | Go and TypeScript; needs `-types`. Generate only the typed entry points: Go `ParseValue` and `ParseRuleValue`, TypeScript `parseValue`, `parseValueWithOptions`, `parseRuleValue` and `parseRuleValueWithOptions`, without `Parse` and `ParseRule` (and, in Go, without the code that builds untyped `*Node` values, which about halves the source of a large grammar). It can be combined with `-recognize` but not with `-document` or `-stream` |
| `-recognize-only` | off | Go and TypeScript. Generate only `Recognize`: no `Parse`, `ParseRule` or typed entry points. It cannot be combined with `-types`, `-typed-only`, `-document` or `-stream` |
| `-recognize` | off | Also generate `Recognize`, which checks input without building a tree (see [recognition](generated-go-api.md#recognition--recognize)) |
| `-no-limits` | off | Opt out of the limit-controlled form of the parser. By default the parser has limit controls: it supports `WithContext` and finite `Compute` and `Memory` limits, exports `WithContext` from the package, and a parse that sets no limit runs with the default `Compute` limit of `max(2^20, 1024 * n)`; in TypeScript it supports the `signal` option and the `compute` limit (see [TypeScript](typescript.md#cancellation-and-compute-limits)). With `-no-limits` the Go parser holds the ordinary form of its functions and nothing that spends budget, which makes `parser.go` 15% to 50% smaller (the plain `parser.go` is 0.50 to 0.85 of the controlled one for the grammars of `testdata/grammars/`; larger grammars save more), and a parse that passes a context or a finite `Compute` or `Memory` returns an error; `Input` and `Depth` work in both forms (see [cancellation and compute limits](generated-go-api.md#cancellation-and-compute-limits)). A parser has one form: without the option it is the controlled one, and a parse that sets no limit runs the same code (a few inline tests of the control take the unmetered path), so generate two parsers if you need the faster plain form as well. A TypeScript parser generated with the option emits no code that spends budget and sets no control, and its options type and exports have no cancellation and no compute limit. With `-document` and `-no-limits`, the Document form has no charges |
| `-expectations` | off | Let a parse record what a syntax error expects: the expected list and the messages of `@error` (see [recording expectations](error-messages.md#recording-expectations)). The parser gets the table of expectation descriptions and the parse option `WithExpectations()`; a parse records only when it passes the option, and otherwise a syntax error has the farthest position, an empty `Expected` list, no `Messages` and the message `syntax error`. Without the flag the parser has no table and no option. A `Document` fixes the choice when it is created. It covers every entry point, including typed values, `Document` and streaming, and TypeScript (`{ expectations: true }`) |
| `-observability` | off | Emit opt-in tracing and profiling for Go or TypeScript parsers; combines with `-types`, `-recognize`, `-document` and `-stream` (see [generated parser tracing and profiling](debugging.md#generated-go-node-and-typed-traces)) |
| `-max-depth` | `0` | Default nested recursive rule-call limit in the generated parser; zero selects 100,000, a positive value overrides it, and negatives fail generation |
| `-default-value` | off | Make the parser return the default node tree: value actions and declared rule types are dropped, predicates still run (see [Default Values](../../spec/language/actions.md#default-values)). Cannot be combined with `-types` |
| `-source-info` | off | Make the parser record the rule of each node and the names of captures, so that results can be printed with them (as `sxir parse -source-info` does) |
| `-nodoc` | off | Leave out the package comment (`// Package p is a parser generated from a SyntaxIR grammar.`), for a package that has its own documentation in another file |
| `-optimize` | `speed` | The optimization profile (see [Optimizations](#optimizations)). `speed`, `memory` or `size`; any other name is an error that lists the valid ones |
| `-opt` | none | Optimizations to give a value after the profile is applied and before `-no-opt`, separated by commas, as `name` or `name=count`: a name alone turns on an optimization that the profile leaves off (`-opt ts-literal-sharing`), a count sets an integer one (`-opt class-tables=3`), and `0` turns one off. An unknown name, a count that is not a number, or a value that the optimization does not take is an error, and so is a required step (`elaborate`, `flowlower`, `flow-finish`, `execlower`), which always runs |
| `-no-opt` | none | Optimizations to turn off after the profile is applied, separated by commas (names in [Optimizations](#optimizations)). A name that is unknown or a required step is an error; a known name that does not apply to the parser being generated does nothing |
| `-print-after` | none | Write the intermediate representation that the generator builds to standard error, as text, after the named steps and passes, separated by commas: the steps `elaborate`, `actionlower`, `go-struct-call`, `go-in-place`, `action-finish`, `plan`, `flowlower`, `flow-finish` and `execlower`, a pass name, or `all`, which selects every step but `plan` (see [Optimizations](#optimizations)); a pass that does not apply to the parser prints nothing (see [Debugging the generator](#debugging-the-generator)) |
| `-print-before` | none | Write the intermediate representation that the named passes receive to standard error, before each of their runs, as `-print-after` writes it: pass names separated by commas, or `all`. The required steps are not passes to print before; their input is the dump after the step before them |
| `-print-changed` | off | Write the intermediate representation to standard error after each pass that changed it, and, before the first such dump of a unit, the unit that the pass received |
| `-verify-each` | off | Verify the intermediate representation after every pass and step, so that an internal error names the first pass that broke it |
| `-stats` | off | Write the statistics of each pass that ran to standard error after the build |
| `-time-passes` | off | Write the time of each pass and step that ran to standard error after the build |
| `-print-pipeline` | off | Write the pass pipeline of the build to standard error before the build, as text: headers with the options of the build, then its steps in order, the required ones marked with `!` (see [Debugging the generator](#debugging-the-generator)) |
| `-opt-bisect-limit` | none | Run only the runs of optional passes, and the applications of their patterns, numbered up to the count in pipeline order, skip the later ones and write each decision to standard error; the required steps always run. The generator generates once, so the files can keep rules that it otherwise leaves out (see [Debugging the generator](#debugging-the-generator)). A negative count is an error |

Details:

- Both languages need `-o`, because the output is several files. TypeScript writes `parser.ts`, `lib.ts` and, with `-types`, `types.ts`.
- The output is formatted with `gofmt`, passes `go vet`, and is **deterministic**: generating twice gives identical
  files. That is what makes the [up-to-date check](#keeping-generated-code-up-to-date) possible.
- Grammar errors are reported with positions, before any file is written. A rule with no expression is a syntax
  error:

  <!-- example: error -->
  ```sxir
  // broken.sxir
  @start
  rule main =
  ```

  ```bash
  sxir gen -g broken.sxir -module example.com/pairs -o /dev/null
  ```

  ```text
  sxir: broken.sxir:4:1: expected an expression, found end of file
  ```

  An action that reads a capture the rule does not have is a compile error:

  <!-- example: error -->
  ```sxir
  // typeerr.sxir
  @start
  rule main = x:[a-z]+ => $y
  ```

  ```bash
  sxir gen -g typeerr.sxir -module example.com/pairs -o /dev/null
  ```

  ```text
  sxir: typeerr.sxir:3:25: undefined capture $y
  ```

  ```bash
  sxir gen -g pairs.sxir -module example.com/pairs -s nope -o /dev/null
  ```

  ```text
  sxir: pairs.sxir: rule nope is not defined
  ```

  ```bash
  sxir gen -g pairs.sxir -module example.com/pairs -pkg foo-bar -o /dev/null
  ```

  ```text
  sxir: pairs.sxir: invalid package name "foo-bar"
  ```

- A `.sxirc` cannot be used, because code generation needs the grammar and a compiled grammar holds only bytecode:
  `sxir: pairs.sxirc: a compiled grammar holds only the bytecode of one mode; gen needs the grammar source (.sxir or .json)`.
- Without `-s`, the grammar's default start rule is used.
- The module of the generated package requires `syntaxir.dev/sxir` (`go get syntaxir.dev/sxir@vX.Y.Z`, the version of
  `sxir`): the generated code imports the packages of `syntaxir.dev/sxir/pkg/runtime` with either `-lib` mode, and with `-lib import` also
  `syntaxir.dev/sxir/pkg/runtime/libgo`. Both refer to the library protocol they were generated for, so a mismatched
  version fails the build on that line instead of misbehaving. With `-lib import`, `parser.go` declares
  `const _ = rt.Protocol35`, and a library that does not implement the protocol fails with
  `undefined: rt.Protocol35`. The embedded copy declares `const _ = valueimpl.Protocol15`, and a `pkg/runtime/value/impl` package that does
  not serve the node representation of that protocol fails with `undefined: valueimpl.Protocol15`. Regenerate the
  parser with the `sxir` of the required version, or require a version of `syntaxir.dev/sxir` that implements the
  protocol.

### Choosing Entry Points

A Go parser has only the entry points you select. `Parse` and `ParseRule` build `*Node` trees and are there unless
`-typed-only` or `-recognize-only` leaves them out; `-types` adds the typed entry points; `-recognize` adds `Recognize`;
`-document` and `-stream` add the incremental and streaming ones. The typed-only and recognize-only forms leave out the
code that builds untyped trees, which is most of the source of a large grammar. Combinations that cannot work are
errors: `-typed-only` needs `-types` and has no `-document` or `-stream`, which build untyped trees, and `-recognize-only`
has nothing else. The sizes below are the kilobytes of the generated `parser.go` (with `-nodoc -lib import`) of
six language grammars. With `-lib embed`, the parser also carries a copy of the library that keeps only the code the
parser can run; [Generated Parser Size](../benchmarks.md#generated-parser-size) gives the size of that copy and of the
whole library for the grammars of the repository.

| Grammar | `-types` | `-types -recognize` | `-types -typed-only` | `-types -typed-only -recognize` | `-recognize-only` |
|:--|--:|--:|--:|--:|--:|
| cel | 213 | 293 | 115 | 195 | 89 |
| cue | 782 | 1,091 | 419 | 729 | 331 |
| duckdb | 3,749 | 5,370 | 1,747 | 3,369 | 1,719 |
| golang | 1,109 | 1,620 | 597 | 1,107 | 548 |
| python | 1,207 | 1,763 | 657 | 1,212 | 604 |
| typescript | 1,893 | 2,767 | 1,038 | 1,916 | 959 |

### Optimizations

The generator optimizes what it emits. The optimization settings belong to the compilation and are the same for every backend; the contract of the optimization settings (the order in which they apply, their values, and the guarantee that none changes what a parser returns) is in [Optimization Settings](../../spec/runtime/optimization-settings.md); this section lists the optimizations. Each optimization is a flag with a name. A profile is a preset: a name for fixed
values of flags, with no logic of its own. The `speed` profile, the default, has every flag on except the opt-ins named
below (`ts-literal-sharing` and `class-tables`). `-opt` sets a flag, and `-no-opt` turns flags off after the profile and `-opt` are applied, so `-no-opt` wins.
A flag keeps what the parser returns, the same results, errors and events, and changes the code, the speed and the size.
A flag that changes the generated API or the representation of the results instead is marked "changes output" in
the table, is always an opt-in, and is never part of a profile. Two uses of the flags:

- To isolate a miscompilation: generate with `-no-opt` and one name and see whether the output changes.
- To measure an optimization with the same generator on both sides.

The kind says what the optimization changes: a `pass` rewrites the parser before it is lowered, an `action pass`
chooses the form of the lowered action expressions, a `flow pass` replaces the form in which the lowered code matches a construct with
another where the target language prints it, an `exec pass` replaces operations of the lowered code with a form
that one target prints more compactly, a `code pass` rewrites the lowered code before and after it is converted for
the target, a `pattern` is one local rewrite of such a pass, a `printer` is a choice of what the Go and TypeScript printers emit, and a `storage`
optimization is a control of the storage the parser emits. The names are the following, by kind and by name within a kind, as the usage text lists them, and an unknown name in `-opt` or `-no-opt` is an error that lists them. No flag in this
table changes output yet:

| Name | Kind | Default | What it does and what it costs |
| --- | --- | --- | --- |
| `dispatch` | pass | on | Keep the guards of the alternatives whose skip leaves what their evaluation would. Off, the parser tries every alternative, as with `-no-opt guards` |
| `guards` | pass | on | Skip an alternative when the next character cannot start it. One read of the next character per choice against the attempts of alternatives that cannot match, and a little more code per choice |
| `outline` | pass | on | Large and repeated parts of a rule run as functions of their own (TypeScript). Smaller functions that the JavaScript engine compiles faster, and equal parts share one function, against a call per outlined part |
| `scans` | pass | on | A value-free repetition of a class or of any character runs as a scan. A loop against the general repetition |
| `field-compare` | action pass | on | Typed Go compares fields in the actions and predicates of typed rules without boxing string fields. Fewer allocations, no cost |
| `text-dispatch` | action pass | on | A predicate that compares the text of a capture with literals tests them at once when the parse runs without limit controls. One test of the literals against a chain of comparisons, and the text is computed once |
| `class-runs` | flow pass | on | Consecutive classes of a value-free sequence run as one (lean typed Go). One run against one test and call per class |
| `class-tables` | flow pass | off | Match a class of at least this many ranges through a membership table (lean typed Go); `-opt class-tables=5` is the measured choice, and 0 turns the tables off. Table data against code: about 3% smaller source, and about 1% slower parsing on the language grammars (3.7% on TypeScript). It is an opt-in |
| `guarded-classes` | flow pass | on | A leading class of a guarded alternative consumes the character its guard read (ordinary Go parsers). Saves a second read and test of that character |
| `inline-literals` | flow pass | 4 | Compare literals of at most four code points in place first; the number is the longest literal, and 0 turns it off. Code against calls: faster, with more code |
| `terminals` | flow pass | on | Value-free choices of many literals and classes run as one program (lean typed Go). Data against code: the choice becomes a table in the parser instead of nested alternatives |
| `ts-merged-records` | exec pass | on | Ending an expectation record and merging its expectations into the enclosing record are one call that merges in place (TypeScript). Saves a copy of the expectations and a local per merge |
| `ts-tested-inits` | exec pass | on | An `if` that tests whether its first call matched tests the call's result in its condition (TypeScript). Saves the boolean local that the call sets |
| `canonicalize` | code pass | on | Apply the local rewrite patterns below to each lowered function until none applies. Simpler code at no cost to the parse |
| `drop-after-exit` | pattern | on | The statements after an exit, break, continue or return in a block are removed. Less code |
| `drop-unused-labels` | pattern | on | The label of a block that no exit names is removed. Less code |
| `fold-branches` | pattern | on | An `if` or a loop with a constant condition becomes the code that runs. Less code and no test |
| `fold-constants` | pattern | on | An operator or a conversion on constants becomes its value. Less code |
| `splice-blocks` | pattern | on | A block without a label that declares no local is spliced into its parent. Less code |
| `literal-sharing` | printer | on | Emit one table for equal literals (Go; TypeScript also needs `ts-literal-sharing`). Smaller source against an indirection to the table |
| `method-sharing` | printer | on | Emit one helper for generated bodies that are exactly equal (Go and TypeScript). Code against calls: smaller source, with a call where a body was |
| `ts-literal-sharing` | printer | off | Share literal tables in TypeScript parsers. Smaller code against speed: a small cold parse measured about 1.5 times slower. It is an opt-in: turn it on with `-opt ts-literal-sharing` |
| `used-features` | printer | on | Leave out the library code of the features that the grammar and the options rule out, such as left recursion, recovery, Operators Rules, streams or Documents, and the declarations of the embedded library that the parser does not name (Go and TypeScript, with `-lib embed`; `-lib only` writes the whole library). Off, the parser carries every feature; what a parse returns and charges is the same either way. Smaller source and binary, at no cost on average; single TypeScript workloads move by about 3% either way, as the JavaScript engine inlines the smaller library differently |
| `sparse-memo` | storage | on | Keep the first-call table of large TypeScript parsers sparse. Memory in proportion to the positions visited against a page lookup per check |
| `typed-list-slabs` | storage | on | Allocate typed lists in slabs (typed Go). Fewer allocations against memory, as for objects |
| `typed-object-slabs` | storage | on | Allocate typed objects in slabs (typed Go). Fewer allocations against memory: an object that escapes keeps the unused objects of its chunk |
| `typed-output-bootstrap` | storage | on | Size the first slab of the typed output from the input (typed Go). A small parse keeps little memory, against a few allocations before the slabs start |

A profile is chosen with `-optimize` (`compiler.WithOptimize` with a `compiler.Optimize` for `compiler.GenerateGo` and `compiler.GenerateTypeScript`). Each sets flags of the table above to other values
than `speed` has and nothing else, so `-opt` and `-no-opt` still apply after it:

| Profile | Sets | For | Measured against `speed` (ratios to `speed`, geometric mean over ten language grammars) |
| --- | --- | --- | --- |
| `speed` | nothing | The default: the fastest parse that does not cost generated code or memory out of proportion | |
| `memory` | `typed-object-slabs` and `typed-list-slabs` off | Fewer bytes and less live heap in typed Go parsers, at the price of many more allocations | bytes allocated 0.886 and live heap 0.848 of `speed`'s, parse time 0.996; allocations rise 1.03 to 1.39 times for the typed grammars and 52 to 97 times for the JSON, CSV and XML parses; no grammar is more than 3.8% slower |
| `size` | `inline-literals` off, `class-tables` at 5 | Smaller generated Go source and binaries | `parser.go` 0.912, stripped binary 0.986 and parse time 1.022 of `speed`'s; no grammar is more than 5.5% slower |

No profile changes what a parser returns. The measurements are in [optimization record 136](../optimizations/136-measured-effect-of-the-generation-flags.md).

A flag never overrides soundness: whether an optimization applies to a parser is decided by the generator, and `-opt`
never turns on a pass for a parser that it is unsound for. `-no-opt guards` also leaves out the guarded classes, which need guards.

### Debugging the Generator

The debugging flags observe the passes that build a parser and write to standard error; none of them but
`-opt-bisect-limit` changes a generated file. What they write is a debugging aid: its form, the pass names and the counter names are not stable and
may change in any version.

`-print-after` shows the intermediate representation that the passes work on, for debugging the generator. A pass that
does not apply to the parser (the lean passes in a parser with limit controls, for example) prints nothing. Only the
generation that writes the output prints. Each dump starts with one header line that names the step and the unit,
`-- after <step> [<table>] --`, where `<table>` is `typed` for the typed rules, and `rules` or `recRules` for the
untyped and recognizer tables; a pass that runs on one function at a time names the function too,
`-- after <pass> [<table>/<function>] --`. The rules of the untyped and recognizer tables that the parser leaves out
are lowered as tables of their own, `rulesUnreachable` and `recRulesUnreachable`, so that the expectation table keeps
their rows. The dumps of each table are written together, in the order of its steps. `-print-after elaborate` shows the Core IR of each rule table as built, with the attributes of its nodes, and
each Core pass prints it under its own name. `-print-after actionlower` shows the action expressions of each table as
lowered, once per table, under `-- after actionlower [<table>] --`: a function for each action site of the table, in the
order of the sites, as a listing of Code IR at the Action stage. The Action passes (`text-dispatch`, and in Go
`field-compare`), the Go step `go-struct-call` and `action-finish`, which ends each function, print each function they
run on under their own names, `-- after <pass> [<table>/site<n>] --`. The Go step `go-in-place` prints the functions of
the table after it adds, for the action of each rule, the function `site<n>_result` that Go prints in place of the
action. `-print-after plan` shows the plan of the parser's program, under
`-- after plan [program] --`, before the dumps of the tables: the entry points, then for each table every rule by ID with the route of a call of it
(`call=slot` through the rule table, `call=wrapper` through the rule's own function) and the ways the entry points reach
it (`reach=slot`, `reach=wrapper`, both, or `reach=none` for a rule whose body the parser leaves out). The plan is
written only when `plan` is named, since `all` selects the forms of the intermediate representation. In a TypeScript
`-print-after dispatch` shows, under `-- after dispatch [program] --`, each table and, by alternative path, the
alternatives that keep their guard, with the rules whose calls the skip makes (`calls=`) and `outside` where a Document
parse evaluates the alternative instead. In a TypeScript
parser, `-print-after outline` shows, under `-- after outline [program] --`, each table and, by rule, the nodes that run
as functions of their own: the path of each, the indices of the children from the body of the rule, and the first line
of its Core IR. `-print-after flowlower` shows the next form: the module of each table that
the parser is printed from, each function as a listing of Code IR at the Flow stage, as lowered from the Core IR.
`flow-finish` prints the same form after the Flow stage ends, and `execlower` prints the form after that, a listing of
Code IR at the Exec stage; the Flow and Exec passes print each function they run on under their own names. A listing writes the statements and the calls of the operations of the stage in the notation of the Code IR text form, with the immediate operand of a call in brackets before its arguments.

`-print-before` dumps the unit that each run of the named passes receives, under `-- before <pass> [<unit>] --`,
before the dump after it if `-print-after` names the pass too. A pass that is off or does not apply prints nothing.

`-print-changed` dumps the unit after each pass that changed its text, under the same headers, and nothing after a pass
that left it as it was. Before the first change of a unit it dumps the unit as the pass received it, under
`-- before <pass> [<unit>] --`, so every change can be compared with what came before it.

`-verify-each` runs the verifier of the intermediate representation after every pass and step instead of only where a
stage ends, so that an internal error names the first pass that broke it. It costs generation time.

`-stats` writes a `-- stats --` block after the build: for each pass that ran, in the order it first ran, the units it
ran on (`runs`), those it reported changing (`changed`), the counters it declares, such as the alternatives that
`guards` gave a guard (`guarded`), and the applications of its patterns. The block is the same in every run.
`-time-passes` writes a `-- time-passes --` block after the build: the time of each pass and step, summed over its
units, with its share of the total, the time of the pass manager outside them, and the total.

```text
-- stats --
  elaborate        runs 1  changed 1
  guards           runs 1  changed 1  guarded 3
  scans            runs 1  changed 1
  plan             runs 1  changed 1
  flowlower        runs 1  changed 1
  ...
```

`-print-pipeline` writes, before the build, the pipeline that the generator runs: a line with the version of the text
format and one with the version of sxir, header lines with the options of the build (`options`, `settings`, `debug` and
`target`), then the steps from the program to the files, one per line, nested in the blocks that run them on each table
or function. A required step ends with `!`, a pass that applies patterns lists them in brackets, and a second instance
of a pass is numbered, as in `canonicalize#2`. The steps are those of the first run of the build; when the TypeScript
generator runs the pipeline again with the reach of its printed code, the steps are the same.

```text
pipeline program -> output {
  with target=go {
    with reach=none {
      each go-tables {
        shared core-config {
          elaborate!
          slot core-passes {
            guards
            scans
          }
        }
      }
      plan!
      ...
      print-go!
    }
  }
}
```

`-opt-bisect-limit <n>` finds the pass that breaks a parser. The generator numbers the runs of optional passes in the
order it makes them, one run being one pass on one unit, and each application of a pattern of a pass takes a number
of its own; it runs those numbered up to `n` and skips the others, and the required steps always run, so the output is
a parser whatever `n` is. Each decision is written as `opt-bisect: <number> run|skip <pass> [<unit>]`. If a parser
built with `-opt-bisect-limit 0` behaves and the default one does not, the smallest `n` that misbehaves names the
run that broke it. To keep the numbers the same from one `n` to the next, the generator generates once under a limit:
the Go generator keeps the rules that the printed code cannot reach, and the TypeScript generator keeps those that
only its unprinted calls reach.

### From Go

The same generator is available from Go, in package `syntaxir.dev/sxir/pkg/compiler`, for build tools that produce parsers programmatically:

```go
files, err := compiler.GenerateGo(grammar.FromString("grammar.sxir", src),
	compiler.GoConfig{Module: "example.com/pairs/pairsparser"}, compiler.WithStart("main")) // grammar, config, start rule
if err != nil {
	log.Fatal(err)
}
for _, f := range files { // parser.go, api.go, types/types.go with Types, and the files of internal/libgo with its subpackages
	path := filepath.Join("pairsparser", filepath.FromSlash(f.Path))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, f.Content, 0o644); err != nil {
		log.Fatal(err)
	}
}
```

The fields of `compiler.GoConfig` are the flags of `sxir gen`: `Module` is `-module`; `Types: true` is `-types`; `Document` is `-document`; `Stream` is `-stream`; `Recognize` is `-recognize`; `TypedOnly` is `-typed-only`; `RecognizeOnly` is `-recognize-only`; `NoLimitControls` is `-no-limits`; `Observability` is `-observability`; `OmitPackageDoc` is `-nodoc`; `Lib: compiler.LibImport` is `-lib import`; `PackageName: "p"` is `-pkg p`; and `MaxDepth: 600_000` is `-max-depth 600000`. `compiler.TSConfig` has the same fields except for `PackageName` and `OmitPackageDoc` (Go only: `-pkg` and `-nodoc`), and takes `LibModule` for `-lib-module`. The options of `compiler.GenerateGo` and `compiler.GenerateTypeScript` set the rest: `compiler.WithSourceInfo()` is `-source-info`, `compiler.WithDefaultValue()` is `-default-value`, and `compiler.WithOptimize(compiler.Optimize{Profile: "size", Values: map[string]int{"class-tables": 3}, Off: []string{"guards"}})` is `-optimize size -opt class-tables=3 -no-opt guards`. `compiler.WithDebug(compiler.Debug{...})` sets the debugging flags: `PrintAfter: []string{"guards"}` is `-print-after guards`, `PrintBefore` is `-print-before`, `PrintChanged`, `VerifyEach`, `Stats` and `TimePasses` are `-print-changed`, `-verify-each`, `-stats` and `-time-passes`, and `OptBisectLimit`, a pointer to the count that is nil without a limit, is `-opt-bisect-limit`; `Output` is the writer they write to, such as `os.Stderr`, which every field but `VerifyEach` needs. Omitted, zero and explicit 100,000 defaults generate identical source. Observability supports standalone Go Node and typed batch parsers, including `ParseValue` and `ParseRuleValue`, Go Documents and streams, and TypeScript batch parsers with or without `-types`, Documents and streams. Generated Go Node, typed and Document packages expose `WithProfile`; TypeScript packages expose `Profile` through per-call options. Typed direct execution and Node-conversion fallback report their actual rule calls; conversion itself has no separate event. Invalid defaults return an error before generated output is written.

`sxir gen` is a thin wrapper around `compiler.GenerateGo` (and `compiler.GenerateTypeScript` with `-lang ts`, which returns the files too, and `compiler.Generate` with a list such as `-lang go,ts`, which takes a `GoConfig` or `TSConfig` per target and returns the files of each from one check of the grammar). Because a grammar built with the `ast` package (see the
[Running Parsers](running-parsers.md#from-go-values-programmatic-grammars)) is just an AST, it can be generated from, too.

When `-observability` is enabled, the generated Go or TypeScript package
includes opt-in rule tracing and profiling. See [generated Go Node and typed
traces](debugging.md#generated-go-node-and-typed-traces), [generated
TypeScript traces](debugging.md#generated-typescript-traces), and [generated
TypeScript profiling](debugging.md#profiling-generated-typescript-batch-parsers)
for the contracts and examples.

The ordinary generated source is unchanged when this option is off. Enabling
it adds generated code and some parse overhead even when no callback is passed;
delivering events costs more. Focused Node measurements showed no-callback
parse ratios around 1.01–1.06× and recognition around 1.02–1.20×. Typed
no-callback ratios ranged from 0.995–1.075×; scalar-store callbacks ranged
from 1.169–3.164× depending on workload and execution path. These are synthetic
measurements, not workload-wide estimates. Focused `WithProfile` runs were
3.8–26.9× ordinary time and added about 45–395 KB/op and 25–50 allocations/op
across the measured cases; see
[design 043](../design/records/043-generated-parser-observability.md#performance-and-results)
for scope and generation costs. Focused TypeScript profiles measured 1.66–6.63×
ordinary time, depending on API and fixture; see the TypeScript profile
measurements in design 043 for conditions and limits.

## What Is Supported

The generator supports the whole grammar language: Rule Expressions, Operators Rules, left recursion, Predicates,
variables, actions, types, built-in functions, `@error` and `#recover`. What differs from the engine is the parsing
machinery, which the generated code only has when you ask for it:

- `-stream`, `-document` and `-recognize` add `ParseStream`, `Document` and `Recognize` (and `-types` their typed
  forms). Without `-stream`, `#stream` is an ordinary repetition. In TypeScript, `-stream` adds the push stream
  `createStream` (and with `-types` `createStreamValue`), and `-document` `createDocument`.
- `ParseRule` replaces `WithStart` for another start rule, and `WithUnit` and `Limits.Depth` work as in the engine,
  except that `Depth: Unlimited` is an error; `-max-depth` sets the generated default.
- Cancellation and the compute limit are `WithContext` and `Limits.Compute` in the engine and in generated Go unless generated with `-no-limits`,
  and the `signal` option and the `compute` limit in TypeScript unless generated with `-no-limits`; the input limit is `Limits.Input` and
  `limits.input` in every form. See
  [Cancellation and Compute Limits](cancellation-and-limits.md) and the
  [specification](../../spec/runtime/generated-parsers.md#compatibility-and-support).
- There is no choice of backend, because the generated code is a backend of its own, and no `Parser.Grammar()`, saving
  or loading, because the grammar is gone at run time.
- `#missing` works for whole-input parsing, recognition, streams and Documents in the runtime parser (the closure
  backend, both bytecode VMs and compiled grammars) and in generated Go parsers, which insert with the same results,
  diagnostics and compute units. The generator rejects
  grammars that use it with `-types` and with `-lang ts`.

The [specification](../../spec/runtime/generated-parsers.md#compatibility-and-support) has the complete table.

Notes:

- **Depth.** Generated code uses the Go stack for rule calls and fails with
  `nesting too deep: more than 100000 nested recursive rule calls` at the default limit. `Limits.Depth` changes the limit for one
  invocation, and `-max-depth` changes the generated default. With the nested-parentheses grammar
  `rule nested = "(" nested ")" / "x"`, 50,000 levels parse and 200,000 levels fail with that error. If you must accept
  deeper nesting beyond the host stack, the engine's `BytecodeIterative` backend with `Limits.Depth` is the tool
  ([Choosing a Backend](backends.md#deep-nesting-and-the-depth-limit)).
- **Memory.** Generated parsers memoize in the same way as the engine, so memory still grows with the input. For input
  larger than memory, generate `ParseStream` with `-stream` or use the engine's `ParseStream`.
- Using a stream grammar with the generator is fine: `Parse` treats the `#stream` pragma as an ordinary repetition, so
  the same grammar file serves both streaming and whole-input parsing.

## Keeping Generated Code Up to Date

Generated code is source: commit it, review its diffs, and regenerate it whenever the grammar changes **or you upgrade
SyntaxIR** (the embedded library comes from the version of `sxir` that generated it). Three habits keep it honest.

**1. Generate with `go generate`.** Put the directive next to the grammar, as in [Quick start](#quick-start), and run `go generate ./...`. Pin the generator with
`go get -tool` as shown above. (`go install syntaxir.dev/sxir/cmd/sxir@latest` and
`//go:generate sxir gen ...` works, too, but every developer then has to install a matching version.) With several grammars, add one directive per grammar.

**2. Test that it is up to date.** A test regenerates in memory and compares with the committed file; it fails with a
reminder when somebody changed the grammar and forgot to regenerate:

```go
//go:embed pairs.sxir
var grammarSrc string

// The files of pairsparser are up to date with pairs.sxir.
func TestGeneratedIsUpToDate(t *testing.T) {
	want, err := compiler.GenerateGo(grammar.FromString("grammar.sxir", grammarSrc),
		compiler.GoConfig{Module: "example.com/pairs/pairsparser"}, compiler.WithStart("main"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range want {
		got, err := os.ReadFile(filepath.Join("pairsparser", filepath.FromSlash(f.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(f.Content) {
			t.Errorf("pairsparser/%s is out of date; run go generate ./...", f.Path)
		}
	}
}
```

If you change `pairs.sxir` (here: `rule value =  [0-9a-f]+ #atomic `) and skip regeneration, the test fails. Among the output:

```text
--- FAIL: TestGeneratedIsUpToDate (0.00s)
    generated_test.go:49: pairsparser/parser.go is out of date; run go generate ./...
FAIL
```

Run `go generate ./...` in your project and check that the generated files have no diff (for example, with `git diff --exit-code`). This makes stale output fail in your own CI.

**3. Test that it behaves like the engine.** The engine is the reference implementation, and a one-time parity test
protects you from the two diverging (for example after an upgrade):

```go
func TestGeneratedMatchesEngine(t *testing.T) {
	p, err := compiler.Compile(grammar.FromString("grammar.sxir", grammarSrc), compiler.WithStart("main"))
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"abc=12", "abc=12; あい=3", "", "abc=", "abc=1 x"} {
		want, werr := p.Parse(in)
		got, gerr := pairsparser.Parse(in)
		wj, _ := json.Marshal(want)
		gj, _ := json.Marshal(got)
		if string(wj) != string(gj) || fmt.Sprint(werr) != fmt.Sprint(gerr) {
			t.Errorf("%q:\n engine    %s %v\n generated %s %v", in, wj, werr, gj, gerr)
		}
	}
}
```

Comparing JSON and error text covers the whole tree (with positions) and all error details. Include inputs that fail,
and inputs that exercise each feature of your grammar.

## Trade-Offs

- **Speed.** In the current [benchmark results](../benchmarks.md), generated Go takes <!-- fig:generated-vs-closure-codepoints -->0.44–0.61×<!-- /fig --> closure time in
  CodePoints and <!-- fig:generated-vs-closure-bytes -->0.50–0.66×<!-- /fig --> in Bytes across eight workloads. Direct `ParseValue` values take <!-- fig:typed-vs-parse-time -->0.69–0.98×<!-- /fig --> generated
  `Parse` time; conversion workloads that include generic nodes can take longer. Standard-library comparisons produce
  different outputs and remain faster where they apply. These measurements are from one machine and commit; run
  `go run ./bench/report` to measure yours.
- **Binary and repository size.** Each generated parser adds source that you commit and that compiles into your
  binary: `parser.go` and `api.go`, with `-types` `types/types.go`, and with `-lib embed` a copy of the library in `internal/libgo` (the same for
  every grammar; `-lib import` shares one copy through `syntaxir.dev/sxir/pkg/runtime/libgo` instead). It does not pull in
  SyntaxIR's compiler, analyzer or VMs.
- **Start-up.** There is none: no grammar to parse or compile. If start-up time is the only concern and a source file
  is acceptable, loading a [`.sxirc`](compiled-grammars.md) is the cheaper alternative.
- **Flexibility.** The grammar is frozen into the binary. Changing it means regenerating, rebuilding and redeploying;
  if users supply grammars at run time, use the engine.
- **Features.** No backend choice; streaming requires `-stream` and incremental Node parsing `-document` (see
  [what is supported](#what-is-supported)).
- **Review noise.** Large diffs on regeneration (particularly after upgrading SyntaxIR) are expected. Mark the files as
  generated for your review tool: the first line, `// Code generated by sxir <version>. DO NOT EDIT.`, follows the Go
  convention that tools recognize.
- **Typed values.** `Parse` returns generic `*Node` trees like the engine, which is what makes the two interchangeable.
  For Go types matching the grammar's types, generate with [`-types`](generated-typed-values.md) and call `ParseValue`;
  for types of your own, write a small conversion from `*Node` or from the typed values, as
  [alpha-parsers/json](https://github.com/syntaxir/alpha-parsers/blob/main/json/json.go) does (`ToAny`).

## Recipes

### One Grammar, One Package

Each generated package defines `Node`, `SyntaxError`, `Parse` and the rest at package level, so two generated parsers in
the same package collide:

```text
both/b.go:17:6: Node redeclared in this block
	both/a.go:17:6: other declaration of Node
```

Give every grammar its own package (and directory), as the ready-made parsers of `alpha-parsers` do. If you also
want several entry points of one grammar, use `ParseRule` rather than generating it twice.

### Engine in Development, Generated in Production

Keep one `.sxir` file. Use `sxir parse` and the compiled parser (`compiler.Compile`) in tools, experiments and tests, where start-up
and features matter more than speed; generate the production parser from the same file; and let the
[parity test](#keeping-generated-code-up-to-date) prove they agree.

### Choosing the Start Rule

Mark every rule where parsing may begin with `@start`, and the usual entry rule with `@start(default)`, so that
`Parse` is the common case; `-s` (for example `sxir gen -g lang.sxir -module example.com/lang -s program`) selects
another start rule for `Parse`. Use `ParseRule("expr", input)` for the other start rules, for example for an
expression evaluator embedded in a larger tool.

### Wrapping the Result in Your Own Types

If the grammar declares the types you want, [`-types`](generated-typed-values.md) generates them. Otherwise: the generated
`Node` is the same loose shape for every grammar. A thin layer keeps that out of the rest of your program:

```go
type Pair struct{ Key, Value string }

func Pairs(input string) ([]Pair, error) {
	node, err := pairsparser.Parse(input)
	if err != nil {
		return nil, err
	}
	var out []Pair
	for _, n := range node.Elems() {
		out = append(out, Pair{
			Key:   n.Field("Key").(*pairsparser.Node).Text(),
			Value: n.Field("Value").(*pairsparser.Node).Text(),
		})
	}
	return out, nil
}
```

Design the grammar's types and actions ([Declaring Types](types.md), [Actions](actions.md)) so that this layer is
thin, and unit-test it with the engine and the generated parser alike.

## Related Topics

- [The Generated Go API](generated-go-api.md): `Parse`, recognition, call options, cancellation and compute limits.
- [Typed Values in Generated Go](generated-typed-values.md), [Incremental Documents in Generated Parsers](generated-documents.md)
  and [Streams in Generated Parsers](generated-streams.md): the packages that `-types`, `-document` and `-stream` add.
- [Generated TypeScript Parsers](typescript.md): the same generator for TypeScript.
- [Compiled Grammars](compiled-grammars.md): the other way to skip compiling at run time.
- [Using the Ready-Made Parsers](ready-made-parsers.md): parsers made with this generator.
