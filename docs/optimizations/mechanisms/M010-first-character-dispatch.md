<!-- generated:facts -->
# M010: First-Character Dispatch

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Choices whose alternatives have known first characters
- **An eligible subset, named under Scope, of**: closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `dispatch` | setting (pass) | on | speed on, memory on, size on | adopted (speed, memory, size) | recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript | [141](../logs/141-dispatch-of-kept-guards.md) |
| `guards` | setting (pass) | on | speed on, memory on, size on | adopted (speed, memory, size) | closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript | [136](../logs/136-measured-effect-of-the-generation-flags.md) |
| `guarded-classes` | setting (flow pass) | on | speed on, memory on, size on | adopted (speed, memory, size) | Go Node, Go recognizer, typed Go | [136](../logs/136-measured-effect-of-the-generation-flags.md) |
<!-- /generated:facts -->

An ordered choice may call several rules that cannot begin with the next input
character. A conservative first-terminal guard rejects those alternatives before
constructing their call and memo state.

## Mechanism and Invariants

The Core IR analysis finds a required starting literal or class through safe
expression wrappers and calls, and the Guards pass sets it as the guard of the
alternative. Unknown, left-recursive and Operators Rule starts
retain ordinary matching. A rejected alternative records the expectation its
first terminal would have produced. The next character is examined, so a
Document edit can invalidate the result. Depth-limit behavior also constrains
skipped calls. The VMs encode guards in `GUARD` instructions; generated parsers
emit equivalent tests. A guard examines the next character without advancing, so
it counts no compute unit. A skip must leave the compute units as the
evaluation would, so under limit controls an alternative whose evaluation calls
rules before its first terminal is skipped only where the skip can count those
calls: the closure backend counts them for unmemoized rules, and the Flow
bodies of the VMs keep only the guards whose skip makes no call. Alternatives
that pass still execute in their original order.

A case-insensitive literal (`"x"i`, or a literal under `#ignorecase`) is guarded by
both cases of its first ASCII letter in the VMs and in the generated Go and TypeScript
parsers, and in the typed Go path through the Core first set (a lower-case letter with a
fold flag). Record 119 has the measurement.

## Scope

Eligible choices in closure, both VMs, generated Go
Node/recognition, typed Go and TypeScript. It is not a reordering of PEG
alternatives or a general lookahead prediction.

## Controls and Measurements

`guards` builds the same grammar without first-character guards on every backend. `dispatch` keeps only the
guards whose skip leaves what the alternative's evaluation would (record 141); off, the parser tries every
alternative, as with `guards` off. `guarded-classes` keeps the guards but leaves the leading class of a guarded
alternative to test the character again. Each is a same-source
reference. Old bytecode without `GUARD` is compatibility input, not a current-source reference compiler.

## Evidence and Implementation

A leading class of a guarded alternative consumes the character its guard read in
ordinary generated Go parsers (record 112). The flags are measured in record 136:
with guards off, parse time is 1.091 times that of the default over the ten
language grammars and `parser.go` is 0.917 times its size.

Historical records 049–051 report JSON parsing improvements of 13% in generated
Node, 7% in closure, and 13%/15% in the VMs on their recorded runs. Record 050
also measures incremental reparse benefits. The revisions and operations differ;
the percentages cannot be combined or generalized to unknown starts or current
TypeScript workloads.

Evidence records:
[049](../logs/049-first-character-dispatch-in-generated-choices.md),
[050](../logs/050-first-character-dispatch-in-the-closure-backend.md),
[051](../logs/051-first-character-dispatch-in-the-vms-instruction-set-3.md),
[112](../logs/112-guarded-first-classes.md),
[136](../logs/136-measured-effect-of-the-generation-flags.md).

Source owners: [`Guards pass`](../../../internal/middleend/corepass/passes.go), the
`guarded-classes` Flow pass,
[`closure.Builder.Choice and Guard`](../../../internal/backend/closure/builder.go), the GUARD
emission of the bytecode compiler, [`generated choice
guards`](../../../internal/backend/codegen/golang/print/generator.go), [`TypeScript choice
guards`](../../../internal/backend/codegen/typescript/print/generator.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M020](M020-character-class-tests.md), [M080](M080-direct-code-point-access.md).

[Optimization Catalog](../README.md)
