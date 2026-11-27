<!-- generated:facts -->
# M160: Class Tables

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Lean typed Go; classes of at least the configured number of ranges
- **An eligible subset, named under Scope, of**: typed Go

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `class-tables` | setting (flow pass) | off | speed 0, memory 0, size 5 | adopted (size) | typed Go | [136](../logs/136-measured-effect-of-the-generation-flags.md) |
<!-- /generated:facts -->

A class with many ranges spelled out in place makes a large test in every place it is used. A
shared membership table makes the test a lookup.

## Mechanism and Invariants

The generator declares one immutable class value per distinct class, with a 128-bit ASCII
membership set, the ranges of the wider characters and the negation flag. A class of at least
the configured number of ranges is matched through it: an inlined ASCII bit test, and a call
for every other character that also records the expectation of the failing class at the failing
position. Values, errors and expected sets do not change.

## Scope

Typed Go parsers that have no budget controls and no Documents, because a table match
charges no budget for the input, and it never applies elsewhere, whatever the flag says. The
engine backends, generated Go Node parsers and TypeScript do not use it.

## Controls and Measurements

The setting is the least number of ranges for which a class is matched through a table, and 0 generates the
classes in place, from the same generator. Over the six typed grammars, 5 makes `parser.go` 0.966 times the
size of the default and the parse about 1.1% slower; that cost is why only the `size` profile sets it
(record 136).

## Evidence and Implementation

Record 105 measures generated sources 6% to 14% smaller together with class runs, and parse
time between -0.7% and +1.2% on the three grammars it timed. Record 136 repeated the measurement
for the tables alone and made them an opt-in.

Evidence records: [105](../logs/105-character-class-tables.md),
[136](../logs/136-measured-effect-of-the-generation-flags.md).

Source owners: [`the class-tables Flow pass`](../../../internal/middleend/flowpass/passes.go),
[`Class`](../../../pkg/runtime/libgo/class.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M020](M020-character-class-tests.md), [M150](M150-class-runs.md).

[Optimization Catalog](../README.md)
