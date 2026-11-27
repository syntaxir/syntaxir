<!-- generated:facts -->
# M130: Short Literal Comparisons

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Short literals of generated Go
- **An eligible subset, named under Scope, of**: Go Node, Go recognizer, typed Go

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `inline-literals` | setting (flow pass) | 4 | speed 4, memory 4, size 0 | adopted (speed, memory) | Go Node, Go recognizer, typed Go | [136](../logs/136-measured-effect-of-the-generation-flags.md) |
<!-- /generated:facts -->

Generated direct Go rules can match common punctuation without calling the
general literal helper on the successful path.

## Mechanism and Invariants

Literals of at most four code points compare against decoded input in place.
Failure or insufficient input goes through `matchLiteral`, preserving
matched-prefix position and expectation behavior. Bytes always uses the general
matcher. Unicode literals and one-to-five-point boundary tests prevent
accidentally treating byte length as character count.

## Scope

Eligible generated Go Node and typed direct bodies in CodePoints. Longer
literals, fallback bodies, Bytes and other backends retain their literal route.

## Controls and Measurements

The setting is the longest literal, in code points, that is compared in place; 0 generates the same grammar
with every literal going through the general matcher. With 0, parse time over the ten language grammars is
1.016 times that of the default and `parser.go` is 0.930 times its size (record 136), which is why the `size`
profile uses 0.

## Evidence and Implementation

Historical record 062 measures typed JSON 3.34 to 3.16 ms, XML 3.89 to 3.80 ms
and Outline 2.14 to 1.97 ms. These do not establish current Node, Bytes or
all-literal effects.

Evidence records:
[062](../logs/062-comparing-short-literals-in-place-in-direct-rules.md),
[136](../logs/136-measured-effect-of-the-generation-flags.md).

Source owners: [`the inline-literals Flow
pass`](../../../internal/middleend/flowpass/passes.go),
[`parser.matchLiteral`](../../../pkg/runtime/libgo/lib.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M080](M080-direct-code-point-access.md), [M030](M030-literal-matching.md).

[Optimization Catalog](../README.md)
