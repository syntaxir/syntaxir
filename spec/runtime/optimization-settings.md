# Optimization Settings

The **optimization setting** of a compilation is a profile and the values of named optimizations. It belongs to
the compilation of a grammar, not to one backend: the setting is the same for every backend, and each backend
applies the optimizations that concern it. This chapter defines the setting, its profiles and optimizations, the
order in which they combine, the errors, and the entry points that take it.

## Scope and Terminology

A **backend** is what a compilation builds a parser for: the Go and TypeScript generators
([Generated Parsers](generated-parsers.md)), the closure engine, which runs the reference parser, and the bytecode
VMs. An **optimization** is a named setting of a compilation that is either on or off, or has a count where `0` is
off. A **profile** is a fixed set of values of the optimizations.

## Contract

### The Setting of Each Backend

The closure engine and the bytecode VMs build their parsers under the Core passes (kind `pass`) of the setting, so
turning one off changes how they parse, and the bytecode VMs also run the Flow passes for the forms of their
bytecode. The recognizer and the missing-token insertion program that a parser derives build under its setting.
The generators apply every kind. A [compiled grammar](compiled-grammars.md) holds the bytecode that the setting of
its compilation built; loading it builds nothing, so it takes no setting.

### Kinds

Each optimization has a kind: `pass` (a transformation of the Core IR, the first intermediate representation built
from the grammar), `action pass` (a transformation of the lowered action expressions of a table), `flow pass` and
`exec pass` (a transformation of Code IR at the Flow stage and at the Exec stage that is converted from it),
`code pass` (a transformation of Code IR at both stages), `pattern` (a local rewrite that a code pass applies, each
application on its own), `printer` (a choice of what the Go and TypeScript printers emit) or `runtime` (a control
of the runtime structures that the printers emit). `sxir gen -h` lists every optimization with its kind, its
default and whether it takes a count.

### Rules

- **No setting changes what a parser returns.** The setting is not part of the grammar's meaning. For every
  accepted setting, a parser accepts and rejects the same inputs and returns the same trees, typed values, syntax
  errors and recovered errors, the same compute units, and the same trace and profile events, as one built with
  the default setting. A setting changes only the code a parser is built or generated as, its size and the time
  and memory of a parse.
- **Profiles.** A profile has no behavior of its own. `speed` is the default, also for the empty name, and sets
  every optimization to its default value. `memory` allocates typed values one by one instead of in slabs (fewer
  bytes and less live heap, more allocations). `size` produces smaller generated Go source and binaries for a
  little parse time.
- **Order.** The values are the profile's, then the values of `-opt` replace them, then `-no-opt` turns
  optimizations off. `-no-opt` therefore wins over `-opt` for the same name.
- **Values.** `-opt name` is `name=1`. An optimization without a count takes `0` or `1`; one with a count takes `0`
  or a positive number.
- **Backends and languages.** An optimization that concerns only one backend or language, such as the slabs of
  typed Go values, has no effect on another, and naming it is not an error.

### Entry Points

| Entry point | Setting |
|:--|:--|
| `compiler.WithOptimize(o)` ([Go Packages](go-packages.md)) | The profile `o.Profile`, then the values `o.Values`, then the optimizations `o.Off` turned off. Every compile function takes it: the parsers of `Compile` and `NewReferenceParser`, the files of `Marshal` and the generators |
| `-optimize <profile>` | The profile |
| `-opt <name>[=<count>][,...]` | The values of single optimizations |
| `-no-opt <name>[,...]` | The optimizations turned off |

The commands that compile a grammar take the three flags with the same names and meaning: `sxir gen`, and
`sxir parse`, `test`, `compile`, `trace`, `profile` and `explain`, which build a parser for the engine.

## Validation and Errors

An unknown profile, a name that is not an optimization, a value that the optimization does not take, a count that
is not a decimal number, an empty item and a name given twice in `-opt` are errors. A command reports the error
with the name of its flag, as in `-no-opt: unknown optimization "x"`, and the list of valid names, before it
compiles or writes anything; `compiler.WithOptimize` makes it an error of every compile function. A command given
a compiled grammar (`.sxirc`) and any of the three flags fails: the file holds the bytecode that its own setting
built, so the setting is chosen when `sxir compile` saves it.

## Examples

```bash
sxir parse -g json.sxir -no-opt guards -i '[1]'
sxir compile -g json.sxir -optimize size -o json.sxirc
sxir gen -g json.sxir -module example.com/json -o out -opt class-tables=3
```

The first parse returns what it returns without the flag. `sxir parse -g json.sxirc -optimize speed` fails, because
`json.sxirc` is a compiled grammar.

## Compatibility and Support

The names of the profiles and optimizations are part of the command line and of `compiler.Optimize`. An
optimization may be added in any version; removing or renaming one is a breaking change. The debugging flags of
`sxir gen` that observe the pass pipeline are not part of this contract
([Generated Parsers](generated-parsers.md#optimization-and-debugging-flags)).
