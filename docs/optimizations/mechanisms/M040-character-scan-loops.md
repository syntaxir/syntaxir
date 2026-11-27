<!-- generated:facts -->
# M040: Character Scan Loops

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Value-free repetitions of a class or of any character
- **An eligible subset, named under Scope, of**: closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `scans` | setting (pass) | on | speed on, memory on, size on | adopted (speed, memory, size) | closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript | [136](../logs/136-measured-effect-of-the-generation-flags.md) |
<!-- /generated:facts -->

A value-free repetition of one character need not build an element value or
create a full backtracking mark for every iteration. A scan loop performs the
repeated membership or any-character test directly.

## Mechanism and Invariants

The compiler recognizes eligible single-character class or dot repetitions only
when their result is unused. It preserves minimum/maximum repetition bounds,
input positions, terminal failure expectations and examined input. Captures,
actions or compound expressions that require per-element state remain on the
general repetition path. The bytecode compiler emits `SCAN`; generated emitters
produce an equivalent loop. Stream scans must retain reader loading behavior.

## Scope

Eligible value-free repetitions in closure, both VMs,
generated Go Node/recognition, typed Go and TypeScript. Value-building
repetitions are outside this specialization.

## Controls and Measurements

With the setting off, every backend runs these repetitions as ordinary loops. Over the ten language grammars,
parse time with it off is 1.029 times that of the default (record 136). A value-building parse changes the
operation and is not an isolated reference.

## Evidence and Implementation

Record 008 combines scan lowering with unmemoized-call changes and reports
recognition reductions such as CSV 24 to 18 ms. Record 011 carries scan loops
into generated Go. Those historical combined measurements do not assign the
whole improvement to scanning or establish current performance in all six
targets.

Evidence records:
[008](../logs/008-fast-path-for-unmemoized-calls-scan-nesting-limit-d2db87d.md),
[136](../logs/136-measured-effect-of-the-generation-flags.md).

Source owners: [`closure.Builder.ScanRepeat`](../../../internal/backend/closure/builder.go),
the SCAN emission of the bytecode compiler,
[`Scans pass`](../../../internal/middleend/corepass/passes.go),
[`Flow scan lowering`](../../../internal/middleend/flowlower/lower.go), which lowers the
scans of generated Go and TypeScript.

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [E010](E010-value-free-rule-bodies.md), [M020](M020-character-class-tests.md).

[Optimization Catalog](../README.md)
