# 077. Observability Contract

- **Status**: Partially Implemented: the contract and its producers have landed; the independent examined-end oracle that gates it has not
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
- `TestObservedExaminedEndsAreExact` and `TestObservedExaminedEndsMutants` compare the examined ends of observed generated
  Go parses with the engine's, and check that a left-out record of an examined end is caught.

These tests compare the producers with the engine. They do not compare the engine with an independent definition of
the examined end, which is the open part of this record.

## Examined Oracle

The examined end is a release gate, so it is checked against a definition that does not run the engine's code. The
oracle has three parts, each applied to every exit of an observed parse on every route (value, parse, recognize,
stream) and on the engine:

1. **Perturbation.** For an exit with examined end `e`, the parse is repeated on the input with every unit at a
   position `>= e` replaced by another unit. The call at the same key must keep its matched flag, end and examined
   end. A change beyond `e` that reaches the result shows that `e` is too small.
2. **Stream high-water mark.** A reader wrapper records the greatest unit it delivered. For a stream parse the root exit's
   examined end is one past that unit (the end slot counts once the reader reported the end), and no exit examines
   beyond it. A difference shows that `e` is too large or that a read is not recorded.
3. **Unmemoized reference evaluator.** A small interpreter of the grammar that evaluates each rule by the
   definitions of the language specification, without memo entries, contexts, first-character dispatch, or
   generated code. It tracks the examined maximum of each call itself and reports the exits, which must equal the
   observed exits in matched flag, end and examined end. Left recursion is evaluated by the growth rules of the
   runtime specification: the leaders of the cycles of left calls are computed from the Rule Facts definitions of the
   core IR specification (nullability, the rules proven to succeed, left calls, cycles, and leaders by declaration
   order), only leaders grow, and the other rules of a cycle are evaluated again in each step, so that no memo decides a
   result.

Independence: the evaluator reads the grammar through the frontend only (the parsed grammar, not Core IR, Exec IR or any
lowering), and its matching, growth and examined rules are written from `spec/language` and `spec/runtime`. It shares no
code with the engine, the backends, the memo, or the generated runtimes, so an error in those cannot agree with the
evaluator by construction. The evaluator is not yet checked by mutants of its own rules, nor by an independent review of them
against the specification; both are open work. Limits of the claim: the frontend and the value of user actions are
outside it; predicates the evaluator cannot run exclude a grammar and are named in the test. Its facts are those of
parses without token insertion.

Status: the reference evaluator is in `internal/testing/examinedref`. It evaluates the matching core of the language,
including left recursion by the growth rules above, and the examined ends it computes are checked by hand on
representative grammars. Its comparison with the engine is a passing check in the long tier (`TestExaminedOracle`): on
the generated corpus and the shapes, none of the 612 inputs of grammars inside the core has an exit whose examined end
differs. Grammars outside the core (predicates, variables, actions with effects, recovery, cuts, anchors of lines,
binding levels, Operators Rules, attributes) are not covered: 222 corpus grammars, which the test counts and does not
compare. Perturbation and the stream high-water mark are not yet implemented.

## Limitations and Open Work

- **Partially Implemented:** the oracle covers the matching core only, and its perturbation and stream parts are not yet written.
  Until they are, the exactness of the examined end rests on the agreement tests in Testing.
- The specification's limitations list the remaining differences between the backends, such as first-character skips
  of calls that fail on the next character.
