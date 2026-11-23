# The `sxir` Command

`sxir` is the command-line tool of SyntaxIR: it parses input with a grammar, formats and converts grammars, saves compiled
grammars, generates parsers, tests, lints, samples, debugs and runs a language server. This guide shows the workflows
of the commands that work on one grammar and its input. The flags of every command are listed by `sxir <command> -h`
and in the generated command reference.

`sxir` with no arguments prints the usage and exits with status 1. Every command takes `-h` to list its flags.
Errors are printed to standard error as `sxir: ...` and give exit status 1. `fmt` and `convert` accept flags before or
after their file arguments.

A `<grammar>` argument is SyntaxIR source (`.sxir`), a grammar in JSON (`.json`) or, for `parse`, `trace`, `profile`,
`explain` and `compile`, a compiled grammar (`.sxirc`). The type is recognized by the contents of a `.sxirc` and by the
`.json` suffix; anything else is parsed as SyntaxIR source. A compiled grammar runs only the parses of the mode it was
saved in, and the commands that need the grammar itself (`convert`, `gen`, `lint`, `sample` and `schema`) reject it.

| Command | Use it to | Read more |
|:--|:--|:--|
| `parse` | Parse input and print the tree, or only check it | [Parse](#parse) |
| `fmt`, `convert` | Format grammar source, convert between source and JSON | [Fmt](#fmt) |
| `compile` | Save a compiled grammar of one mode | [Compiled Grammars](compiled-grammars.md) |
| `gen` | Generate a Go or TypeScript parser | [Generating Parsers](code-generation.md), [Generated TypeScript Parsers](typescript.md) |
| `test` | Run a fixture suite | [Testing Grammars](testing-grammars.md) |
| `lint` | Find likely mistakes in a grammar | [Linting Grammars](linting.md) |
| `explain`, `trace`, `profile` | Find out why an input fails or a parse is slow | [Debugging and Profiling](debugging.md) |
| `sample`, `edit-fuzz` | Generate inputs, check incremental parsing | [Sampling Inputs and Fuzzing](sampling-and-fuzzing.md), [Fuzzing Edits](sampling-and-fuzzing.md#fuzzing-edits) |
| `schema`, `schema diff` | Print the output schema of a grammar, compare two schemas | [Output Schemas](output-schemas-and-queries.md#the-command-line) |
| `lsp` | Run the language server | [Editor Support](editor-support.md) |

The examples use the grammar `pairs.sxir`:

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

## `parse`

```bash
sxir parse -g <grammar> [-s <rule>] [-i <input>] [-f json|sexpr] [-stream] [-recognize]
           [-unit codepoints|bytes] [-backend closure|bytecode|bytecode-iterative] [-source-info] [-default-value]
           [-optimize <profile>] [-opt <names>] [-no-opt <names>]
           [-max-compute <n|unlimited>] [-max-memory <n|unlimited>] [-max-input <n|unlimited>] [-max-depth <n|unlimited>]
```

| Flag | Default | Meaning |
|:--|:--|:--|
| `-g` | (required) | The grammar |
| `-s` | saved in a `.sxirc`, otherwise the default start rule | Start rule |
| `-i` | standard input | The input text. Without `-i`, input is read from standard input. |
| `-f` | `json` | Output format: `json` (indented, like `encoding/json`'s `MarshalIndent`) or `sexpr` (`Node.String()`) |
| `-recognize` | off | Recognition only: print `ok`, or the syntax error |
| `-unit` | `codepoints` | Position unit |
| `-backend` | chosen for the grammar | `closure`, `bytecode` or `bytecode-iterative` |
| `-source-info` | off | Record the rule of each node and the names of captures, and print them |
| `-default-value` | off | Drop value actions and declared rule types, so the parse returns the default node tree; predicates still run (see [Default Values](../../spec/language/actions.md#default-values)) |
| `-stream` | off | Print each `#stream` element as soon as it matches, one per line (see [streaming.md](streaming.md)) |
| `-optimize`, `-opt`, `-no-opt` | the `speed` profile | The [optimization settings](../../spec/runtime/optimization-settings.md) that the grammar is compiled under, as in `gen`. None changes what the parse returns. Not accepted with a compiled grammar (`.sxirc`), which holds the bytecode that its own settings built |
| `-max-compute` | `0` | `Limits.Compute`: the cost a parse may spend, in compute units. `0` selects the default and `unlimited` disables the limit |
| `-max-memory` | `0` | `Limits.Memory`: the bytes of storage a parse may take into use. `0` and `unlimited` disable the limit |
| `-max-input` | `0` | `Limits.Input`: the longest input accepted, in the position unit. `0` and `unlimited` accept any length |
| `-max-depth` | `0` | `Limits.Depth`: the deepest nesting of recursive rule calls. `0` selects the backend's default (100,000; 10,000,000 for `bytecode-iterative`), and `unlimited`, accepted only with `bytecode-iterative`, disables the limit |

`sxir parse`, like `explain`, `trace`, `profile`, `test` and `edit-fuzz`, records [expectations](error-messages.md#recording-expectations), so a syntax error lists what was expected.

A parse that reaches a limit prints the message of the `*runtime.LimitError` (for example `parse compute limit exceeded: limit 1048576, used 1048576, requested 1`) to standard error and exits with status 1, like any other failed parse. See [cancellation-and-limits.md](cancellation-and-limits.md).

```bash
sxir parse -g pairs.sxir -f sexpr -i 'abc=12; あい=3'
```

```text
[Pair{Key: "abc", Value: "12"} Pair{Key: "あい", Value: "3"}]
```

```bash
printf 'abc=12; def=3\n' | sxir parse -g pairs.sxir -f sexpr
```

```text
[Pair{Key: "abc", Value: "12"} Pair{Key: "def", Value: "3"}]
```

```bash
sxir parse -g pairs.sxir -s key -f sexpr -i 'abc'
```

```text
"abc"
```

```bash
sxir parse -g pairs.sxir -recognize -i 'abc=12'
```

```text
ok
```

```bash
sxir parse -g pairs.sxir -recognize -i 'abc=x'; echo "exit $?"
```

```text
sxir: 1:5: syntax error: expected [0-9]
exit 1
```

Without `-f sexpr` the value is printed as JSON, for `abc=12`:

```json
{
  "start": 0,
  "end": 6,
  "value": [
    {
      "type": "Pair",
      "start": 0,
      "end": 6,
      "fields": {
        "Key": {
          "start": 0,
          "end": 3,
          "value": "abc"
        },
        "Value": {
          "start": 4,
          "end": 6,
          "value": "12"
        }
      }
    }
  ]
}
```

A node is an object with `start`, `end` and `value`; a struct is an object with `type`, `start`, `end` and `fields`. The tree has no rule names
or capture names unless you ask for them with `-source-info`, which adds a `rule` to each node (and `captures` to a tuple that
has them) in JSON, and `@rule` suffixes and `name:` labels in S-expressions:

```bash
sxir parse -g pairs.sxir -source-info -f sexpr -i 'abc=12'
```

```text
[Pair{Key: "abc"@key, Value: "12"@value}]
```

When the parse recovered from errors with `#recover`, the tree is
printed first and then the errors, with exit status 1, so you can still inspect the tree.

## `fmt`

```bash
sxir fmt [-w] [-l] [-check] [<file.sxir> ...]
```

Formats SyntaxIR source and **keeps comments**. Without `-w` it prints the result; `-w` rewrites the files in place;
`-l` lists the files whose formatting differs. `-check` lists them too, without changing them, and exits with status 1
when there are any and 2 when a file cannot be read or parsed, so a CI job can run `sxir fmt -check *.sxir`; `-l` alone
exits with status 0 whatever it lists. Without file arguments it formats standard input (`-w` is then an error). It accepts only `.sxir` files; for JSON grammars it points you to `convert`.
Adjacent minus operators stay separated: `- -"a"` remains two discards, and `- -1` remains two unary negations.
The whitespace keeps either expression from becoming `--`, a reserved token that is an error.

A comma-separated list in brackets (call arguments, struct and list constructors, tuples, tuple types, type arguments, lambda parameters and
attribute arguments) stays on one line, like `f(a, b)`, unless it ends in a trailing comma or holds a comment that needs
its own line. Then each element gets a line and a comma, so a trailing comma is the way to ask for the lines:
`f(a, b,)` becomes `f(` with `a,` and `b,` on separate lines and `)` on the last. The comma of a one-element tuple,
`(x,)`, is part of the syntax and does not count.

With `-w`, a changed regular file is written completely to a temporary file in the target directory before replacement.
Permission bits are preserved, read-only files are rejected, and the directory must allow creating and replacing files.
Symlinks keep pointing to the same path and that target is replaced; formatting one hard-linked name leaves the other
names with their original content. Files whose formatting is unchanged are left in place.

```bash
cat messy.sxir
```

```text
// A messy grammar.
@start
rule main =    ws   "x"+   ws   $$    // trailing comment
rule ws =  [ \t\n]*
```

```bash
sxir fmt -l messy.sxir
```

```text
messy.sxir
```

```bash
sxir fmt -w messy.sxir && cat messy.sxir
```

```text
// A messy grammar.
@start
rule main = ws "x"+ ws $$ // trailing comment
rule ws = [ \t\n]*
```

```bash
sxir fmt -l messy.sxir     # nothing listed: already formatted
sxir fmt -check messy.sxir # exit status 0
```

## `convert`

```bash
sxir convert [-to sxir|json] [-o <file>] <grammar>
```

Converts between SyntaxIR source and the JSON form of the grammar AST. `-to` defaults to the other format (`.sxir` becomes
JSON, `.json` becomes SyntaxIR source, and a file with another extension is JSON when its first character other than
white space is `{`); a compiled grammar holds no source and is rejected. Without `-o`, the result goes to standard
output. JSON carries the comments that declarations, struct fields and operators rule lines own, and converting it
back to source writes them again, but it carries no layout and no other comments. Keep the
`.sxir` as the source of truth.

```bash
sxir convert -to json pairs.sxir -o pairs.json
sxir parse -g pairs.json -f sexpr -i 'abc=12'      # JSON grammars work everywhere a grammar is expected
```

## `compile`

```bash
sxir compile -g <grammar> [-s <rule>] [-mode parse|recognize|document] [-source-info] [-default-value]
             [-optimize <profile>] [-opt <names>] [-no-opt <names>] -o <file.sxirc>
```

Saves a compiled grammar; see [compiled grammars](compiled-grammars.md). `-s` sets the default start rule stored in
the file, `-mode` chooses the parses the file serves, `-source-info` makes the parser record the rule of each node and
the names of captures, and `-default-value` makes it return default values (the flag is stored in the file).
`-optimize`, `-opt` and `-no-opt` choose the optimization settings that the saved bytecode is built under; a compiled
grammar given as `-g` keeps its own. Both `-g` and `-o` are required.

| `-mode` | The file serves |
|:--|:--|
| `parse` (default) | `sxir parse` and `sxir parse -stream` |
| `recognize` | `sxir parse -recognize` |
| `document` | `sxir trace`, `sxir profile` and `sxir explain` |

Another command given the file reports the file's mode and the `-mode` it needs. When `<grammar>` is itself a `.sxirc`,
it is saved again only in its own mode, so pass that mode with `-mode`; otherwise the command reports the mismatch and
writes no output. To save another mode, compile the grammar source.

## `gen`

```bash
sxir gen -g <grammar> -module <import path> [-pkg <package>] [-lib embed|import|only] [-s <rule>] [-o <directory>] [-types] [-document] [-stream] [-recognize] [-typed-only] [-recognize-only] [-expectations] [-observability] [-no-limits] [-max-depth <n>] [-nodoc] [-source-info] [-default-value] [-optimize <profile>] [-opt <name[=count]>] [-no-opt <names>] [-print-before <passes>] [-print-after <passes>] [-print-changed] [-verify-each] [-stats] [-time-passes] [-opt-bisect-limit <n>]
sxir gen -lang ts -g <grammar> -o <directory> [-s <rule>] [-lib embed|import|only] [-lib-module <specifier>] [-types] [-typed-only] [-recognize] [-recognize-only] [-expectations] [-observability] [-no-limits] [-max-depth <n>] [-source-info] [-default-value] [-optimize <profile>] [-opt <name[=count]>] [-no-opt <names>] [-print-before <passes>] [-print-after <passes>] [-print-changed] [-verify-each] [-stats] [-time-passes] [-opt-bisect-limit <n>]
sxir gen -lang go,ts -g <grammar> -o <directory> [-go.o <directory>] [-ts.o <directory>] [-go.<flag> <value>] [-ts.<flag> <value>] [the flags above]
```

Generates a standalone Go parser (see [code-generation.md](code-generation.md)) or, with `-lang ts`, a TypeScript
parser (see [typescript.md](typescript.md)). `-g` is required (except with `-lib only`), and for Go `-o`, the output directory, and `-module`, the import path of the package (with `-lib import`, `-pkg` is enough). The Go output is `parser.go`, `api.go` and, with `-lib embed`, a copy of the library in `internal/libgo`; `sxir gen` refuses to replace a file in the directory that it did not generate. TypeScript needs `-o` as well and writes `parser.ts`, `lib.ts` and, with `-types`, `types.ts`; `-lib import` imports the library from `-lib-module` instead of writing `lib.ts`. `-source-info` makes the generated Go parser record the rule of each node and the names of captures.
`-default-value` makes the generated parser return default values; it cannot be combined with `-types`, because the
types come from the dropped actions.
The optimization settings are not specific to generation (see [Optimization Settings](../../spec/runtime/optimization-settings.md)): `parse`, `test`, `compile`, `trace`, `profile` and `explain` take the same three flags, and a compiled grammar (`.sxirc`) takes none, since it holds the bytecode that its own settings built.
`-optimize <profile>` selects an optimization profile (`speed`, the default, `memory` or `size`), `-opt <name>[=<count>][,...]`
turns on an optimization that the profile leaves off or sets a count, `-no-opt <name>[,<name>]`
turns named optimizations off. None of them changes what the generated parser returns; the names are in
[Optimizations](code-generation.md#optimizations). A required step (`elaborate`, `flowlower`, `flow-finish`,
`execlower`) always runs, and naming one in `-opt` or `-no-opt` is an error.
The debugging flags write to standard error and change no generated file: `-print-after <pass>[,<pass>]` writes the
Core IR, Code IR at the Flow or Exec stage, or the plan of the parser (`plan`) after the named steps and passes,
`-print-before <pass>[,<pass>]` before
the named passes, `-print-changed` after each pass that changed it, `-verify-each` verifies it after every pass, and
`-stats` and `-time-passes` write the statistics and the times of the passes after the build, and `-print-pipeline`
writes the pass pipeline of the build before it.
`-opt-bisect-limit <n>` runs only the first `n` runs of optional passes and writes each decision, to find the pass that
breaks a parser; it generates once, so its files can keep rules that a build leaves out (see
[Debugging the generator](code-generation.md#debugging-the-generator)). Their output is not stable and may change in
any version. None of these flags can be combined with `-lib only`.

`-lang` takes a list, such as `-lang go,ts`, to generate several languages from one compilation of the grammar; each
language appears at most once:

```bash
sxir gen -lang go,ts -g calc.sxir -module example.com/calc -o gen -go.types -ts.max-depth 500
```

With several languages, `-o` is a parent directory, and each language writes to the subdirectory named after it
(`gen/go` and `gen/ts` above) unless its qualified flag `-go.o` or `-ts.o` names another directory. `-g`, `-s`,
`-source-info`, `-default-value`, the optimization flags and the debugging flags belong to the whole generation. A flag
of the generated code, such as `-types` or `-lib`, applies to every listed language, and its qualified form
(`-go.types`, `-ts.lib`) applies to one language and wins over the unqualified flag. `-module`, `-pkg` and `-nodoc` apply
to Go and `-lib-module` to TypeScript. A flag that a listed language does not support is an error naming its qualified
form: `-stream` with `ts` listed must be written `-go.stream`. The files
of each language equal those of a run with that language alone, and when any target fails, for example because its
directory holds a file that `sxir gen` did not generate, no target writes a file. `-lib only` with several languages
writes the library of each.

A `.sxirc` compiled without `-default-value` cannot be used with `-default-value` in `parse`: the command
asks you to compile it again with `-default-value`.

## `schema`

```bash
sxir schema -g <grammar> [-s <rule>] [-all-rules] [-o <file>]
sxir schema diff [-s <rule>] [-all-rules] [-f text|json] <before> <after>
```

`schema` prints the [output schema](output-schemas-and-queries.md) of a grammar as JSON, without parsing an input. `-s`
selects the start rule as it does for the other commands, and an empty name selects the default. By default the schema
holds the start rule and the rules it calls, as `Parser.OutputSchema()` does; `-all-rules` adds the unreachable rules
and every declared type, as `runtime.WithAllSchemaRules()` does. `-o` writes the file.

`schema diff` compares two schemas and prints one line per added, removed or changed type and rule with its
compatibility class and the rules behind it, for example `changed type declared/Pair: breaking (field-removed at
$.fields.Old)`. Each operand is either a schema JSON file, as written by `schema -o`, or a grammar. A file is recognized
by its `"format": "sxir-output-schema"` member, so a grammar in JSON is never mistaken for a schema. `-s` and
`-all-rules` apply to the grammar operands only; a schema file already holds the rules it was exported with. With `-f
json`, the output lists the changes with their `class`, their `impacts` (`path`, `class` and `rule`) and the entries of
both sides. The command exits with status 0 whenever both operands are valid, even when they differ, so a script must
read the output to detect a change or a `breaking` class. A change is `breaking` when an existing reader may stop
compiling against the generated types or stop handling a value, and `compatible` when it only adds a type, rule, struct
field or capture; see [the compatibility criterion](../../spec/runtime/schemas-and-queries.md#comparing-schemas).

```bash
sxir schema -g pairs.sxir -o before.json
# edit pairs.sxir
sxir schema diff before.json pairs.sxir        # a schema file and a grammar
sxir schema diff -all-rules -f json before.json pairs.sxir
```

## `explain`, `trace` and `profile`

```bash
sxir explain -g <grammar> [-s <rule>] [-i <input>] [-n <calls>]
sxir trace -g <grammar> [-s <rule>] [-i <input>] [-show-depth <n>] [-rule <name>] [-failures] [-f text|json]
sxir profile -g <grammar> [-s <rule>] [-i <input>] [-sort <column>] [-n <rows>] [-f text|json]
```

Debugging tools; all three also take `-unit`, `-backend`, `-optimize`, `-opt`, `-no-opt`, `-max-compute`, `-max-memory`, `-max-input` and `-max-depth` (as in `parse`), and all read the input from standard input without `-i`. The `-show-depth` of `trace` selects the calls it shows; `-max-depth` is the depth limit of the parse.
`explain` shows which rule calls recorded what a syntax error says was expected, `trace` prints every rule call of a
parse as an indented tree, and `profile` prints the cost of each rule with hints. In Go, the same information comes
from `runtime.WithTrace` and `runtime.WithProfile`. A parse that reaches a limit ends the trace or profile at that point; `profile` prints the limit error as its `result`. See [debugging.md](debugging.md).

## `sample`

```bash
sxir sample -g <grammar> [-s <rule>] [-n 10] [-seed <n>] [-max-depth <d>] [-max-repeat <r>] [-max-len <bytes>] [-budget <steps>] [-coverage] [-invalid] [-f lines|json]
```

Prints distinct inputs that the grammar accepts, one Go-quoted string per line or as a JSON document; `-invalid`
prints near-miss inputs that it rejects instead. See [sampling-and-fuzzing.md](sampling-and-fuzzing.md).

## Related Topics

- [Running Parsers](running-parsers.md): the Go API behind `parse`.
- [Compiled Grammars](compiled-grammars.md), [Generating Parsers](code-generation.md) and
  [Generated TypeScript Parsers](typescript.md): the commands that ship a grammar.
- [Testing Grammars](testing-grammars.md) and [Debugging and Profiling](debugging.md): the commands that check one.
