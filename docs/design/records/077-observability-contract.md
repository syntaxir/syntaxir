# 077. Observability Contract

- **Status**: Partially Implemented: the contract, its producers and the reference evaluator of the examined-end oracle
  have landed; the perturbation and stream high-water parts of the oracle have not
- **Author**: @ornew
- **Date**: 2026-11-21

## Summary

The engine, generated Go parsers and generated TypeScript parsers report parses through one contract: one trace event
and one profile, one set of observation options with one meaning, and one examined end per call. The examined end is
exact in every producer. This record supersedes the observability parts of
[014](014-tracing-and-profiling.md) and [043](043-generated-parser-observability.md): the observation API, the event and
profile shapes, and the examined end. Their command-line and measurement content stands. The normative text is
[Tracing and Profiling](../../../spec/runtime/observability.md).

## Motivation

Before this record, the engine and the generated parsers defined separate types. The engine's trace event carried
lookahead and reused elements and the generated one carried an examined-known flag and a skip reason. Profiles were
opaque in one producer and a snapshot in the other. TypeScript had a third shape, and its examined end was not measured
outside a Document parse. A tool that reads traces or profiles needed a code path per producer, and the same option
name could mean a different thing in each. The examined end is the input that a call's result depends on, which is the
quantity that memoization and incremental reuse rely on; a value that is sometimes unknown cannot be trusted by them.

## Decision

### Observation Options

The four options `WithTrace`, `WithProfile`, `WithTraceFilter` and `WithTraceMaxDepth` have the same meaning in the
engine and in generated Go parsers, and TypeScript takes them as fields of its parse options.

- Several trace functions and several profiles each receive the whole parse, in the order given. A nil (or `null`)
  function or profile is ignored.
- The trace filter and the depth limit select only what the trace functions receive. They never change the parse or
  what the profiles record. When one is given twice, the last applies.
- A `Document` keeps the options given at its creation as defaults. Trace functions and profiles given to one parse
  add to the defaults. A filter or depth limit given to one parse replaces the default for that parse only.

### Events and Profiles

Trace events and profiles are defined once in the shared package `instrument` (`syntaxir.dev/sxir/pkg/runtime/instrument`),
and every producer uses those types. The engine and generated Go re-export them; TypeScript exports the same fields
under their JSON keys in camelCase.

- An event is `enter`, `exit` or `skipped`. A skipped call carries a typed skip reason, which the engine never emits.
- A field that only some producers can fill is documented in the specification with the producer that fills it. The
  generated parsers report `reusedElements` as 0; lookahead is filled by every producer.
- A profile is opaque. Each parse records into its own recorder, and a snapshot is an owned copy with rules sorted by
  name. Profile time excludes the trace functions, and TypeScript reports it in milliseconds.

### Examined End

`examined` is the exact exclusive end of all input that the call examined: every unit below it can change the call's
result or its examined range, and nothing beyond it was looked at by the call or by a call it used. There is no
"known" flag and no unknown state in any producer.

The generated `Document` reuse frontier is a different quantity. It bounds what a reused result may depend on, is
kept for reuse, and is not reported in traces or profiles.

### Cost

Observation has a cost only when it is requested. An observed parse runs a separate traced path that records exact
examined ends; a plain parse, in any producer, records nothing extra. A parse with a profile but no trace function
pays for the profile's counts, not for the event delivery.

## Alternatives Considered

- **Keep a per-producer shape with an examined-known flag.** Rejected: two producers would mean two meanings for
  `examined`, and every consumer would need to handle an unknown state.
- **Report the generated Document's reuse frontier as `examined`.** Rejected: the frontier is conservative and is
  larger than the examined end, so the trace would report input the call did not look at.
- **Record exact examined ends in every parse.** Rejected: unobserved parses would pay for data no one reads. The
  separate traced path keeps plain parses at their previous cost.
- **Give TypeScript its own trace shape with fewer fields.** Rejected: one shape lets a tool read traces from any
  producer, and the fields a producer cannot fill are documented as such.

## Testing

- `TestTSObservationsAgreeWithEngine` compares TypeScript events and profiles with the engine's over parse, recognize,
  document and edit routes.
- `TestGeneratedTSObservationOptions` checks the meaning of the options in TypeScript, including the Document defaults.
- `TestEngineObservationMutants` checks that the agreement tests fail on deliberately broken observation code in the
  engine, generated Go and TypeScript.
- The examined oracle below checks the examined ends of every producer against definitions that run none of their
  code: `TestExaminedOracle` for the engine, `TestObservedExaminedEndsAreExact` for generated Go and
  `TestTSObservedExaminedEndsAreExact` for generated TypeScript compare each exit with the reference evaluator, on the
  generated corpus and on shapes chosen for the reference in the `codegentest` package.

## Examined Oracle

The examined end is a release gate, so it is checked against definitions that do not run the producers' code. The
oracle has three parts. Each applies to the observed parses of the engine, on every backend, and of the generated Go
and TypeScript parsers, on their routes (typed value, parse, recognize, stream and Document parses, as each producer
has them), in both position units.

1. **Unmemoized reference evaluator.** A small interpreter of the grammar, in `internal/testing/examinedref`, evaluates
   each rule by the definitions of the language specification, without memo entries, contexts, first-character
   dispatch or generated code, tracks the examined end of each call itself and reports every exit. Every exit that a
   producer reports must be an exit of the reference: a call of the same rule at the same position with the same
   binding level that returned the same match, end and examined end. A key may have several reference outcomes: the
   steps of a growth of left recursion, and calls made with different variable values or token insertions on their
   paths. Every skipped call must fail in the reference, and the start call must return exactly the reference's
   result. A producer may make fewer calls than the reference (memo hits, resumed repetitions of a Document, calls
   skipped by their first character) but none that the reference does not make.

   The reference evaluates the whole language: terminals in both units, with the slots that decoding a byte sequence
   examines; anchors; sequences, choices, repetitions, options and lookaheads; cuts; `#recover`; `#missing`, with and
   without token insertion; `#ignorecase`; attributes; inline actions, as calls of their hidden rules; Operators Rules
   and level-restricted calls; predicates and variables; and the action language: Value Actions of rules, inline
   actions, Operators Rule operands and operators (`$lhs`, `$op`, `$rhs`) and whole-rule actions (`$value`), struct and
   named node constructors, zero values of unset fields, casts to every Type, including structs and joins named by
   aliases, and the built-in functions. A runtime error of a Value Action makes the parse fail; the reference records it
   and goes on, so a producer that aborts reports a prefix of the reference's exits, and a producer may abort only where
   an action of the reference fails. Left recursion
   grows by the rules of the runtime specification: the leaders of the cycles of left calls come from the Rule Facts
   definitions of the core IR specification, computed for the parse with or without token insertion, only leaders
   grow, and the other rules of a cycle are evaluated again in each step.

2. **Perturbation.** For an observed parse of an input x, each distinct examined end c below the length of x is a
   change point. A perturbation of x at c is an input whose first c units are those of x (in code points, whose
   decoding of its first c code points is that of x) and that differs from x in every slot at or beyond c: every unit
   from c replaced by another, x cut at c, or x extended at its end. The parse of x is run twice, which must give the
   same events, and the parse of the perturbation once; the events are compared in order. Up to their first
   difference the two parses are the same computation, so the read of a changed slot that caused it happened inside
   every call running at that point, in both parses: each such call's exit, in each parse that has it, must have an
   examined end greater than c. A difference with no running call, such as the whole-input check after the start
   call returned, constrains nothing. In a grammar whose predicates read variables, a call is checked only when c is
   at least the largest end of a matched exit reported before the call's enter event, since a variable can carry
   input read before the call into it. Perturbation shows examined ends that are too small; an end that is too large
   is the stream high-water mark's to show.

3. **Stream high-water mark.** A stream parse reads from a reader that delivers one byte per read and returns the end
   of the input only on a read after the last byte. Let `Needed(e)` be the input a parse must have read to have
   examined the slots below `e`, as [Tracing and Profiling](../../../spec/runtime/observability.md#engine-tracing)
   defines it. When an exit is reported, the parse has read `Needed(Examined)` of the call; the parse reports exactly
   one exit of the start call; and when it returns, it has read exactly `Needed(H)`, where `H` is the greater of the
   start call's examined end and, when the start call matched and the parse requires the whole input, its end plus
   one. The whole-input check belongs to the parse, not to the start call, so the start call's examined end does not
   include the slot it examines.

Independence: the reference reads the grammar through the frontend only (the parsed grammar, not Core IR, Exec IR or
any lowering), and its matching, growth and examined rules are written from `spec/language`, `spec/runtime` and
`spec/core-ir`. Perturbation and the high-water mark use only the producers' events and the input. None of them shares
code with the engine, the backends, the memo or the generated runtimes, so an error in those cannot agree with them by
construction. Each part is checked by mutants of its own rules and of its comparison (`TestReferenceMutants` and the
mutants of perturbation and the high-water mark), which must make the oracle fail. A mutant of the reference must fail
a shape that the oracle passes without it, so that a shape on which a producer differs cannot hide it. A rule whose
breaking changes only the shape of a value, such as the associativity of an operator, has no mutant: no exit carries
that shape.

The reference evaluates the action language itself, from `spec/language/actions.md` and `spec/language/types.md`.
The producers' evaluator of actions runs on the state of a parser: it needs the capture frames of a compiled program, the
parser's variables, node arena and budget, and terms lowered through the middle end. Reusing it would tie the
reference to that state through an adapter that is itself a second interpreter boundary. Action values are not the
property under test, but predicates read them and so decide matches and examined ends: a grammar whose predicates read
action values has exact examined ends like any other, and the oracle checks every grammar of the corpus. A construct
that the reference cannot evaluate fails the test.

Limits of the claim: the frontend is outside it.

## Limitations and Open Work

- **Partially Implemented:** the perturbation and stream high-water parts of the oracle are not yet written. Until they
  are, the examined ends rest on the reference evaluator alone, and the input that a stream parse reads on the agreement
  tests in Testing.
- The specification's limitations list the remaining differences between the backends, such as first-character skips
  of calls that fail on the next character.
