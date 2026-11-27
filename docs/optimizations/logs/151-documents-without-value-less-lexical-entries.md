# 151. Documents Without Value-less Lexical Entries

- **Status**: Proposed
- **Date**: 2026-11-28
- **Mechanisms**: none (the [transient rule memo policy](../../design/implementation/transient-rule-memo-policy.md) of Documents)
- **Flags**: none

## Problem and Mechanism

A Document stores the result of every call of a memoized or transient rule, so that a later parse can reuse it. Many
of those results belong to rules that only match characters: the blanks between the elements of a list, the digits of
a number, the characters of a name, where the caller does not use the value. Each such result costs an entry, its
place in the position index and its shift after an edit, and a lookup before every call, while matching the
characters again costs a scan of a few of them. The results of the callers carry the reuse: a reused element contains
its blanks and its names.

A value-less lexical rule is the value-free twin of a transient rule whose body is made only of literals, character
classes, `.`, anchors, `_`, `_|_`, sequences, choices, options, repetitions, discards, lookaheads, `#atomic`, captures
and calls of such rules, with no action and on no cycle of calls. Documents no longer store or look up its results:
every parse matches its characters again, as a parse from scratch does. The term and the rule are normative
(documents specification, "Results that are stored"), so the Documents of every backend store the same results and
count the same compute units. A compiled module records the property of each rule, and the instruction set version
is 42.

## Applicability

| Execution profile | Status | Reason |
|:--|:--|:--|
| Node and typed Documents, every backend | Applied | Every call of a value-less lexical rule |
| Parses, recognitions, typed parses and streams | Not applicable | They never memoize transient rules |

## Bound

The linear-cost proof keeps its bound for Documents: a call of a value-less lexical rule looks nothing up and stores
nothing, so it adds its call and its body to the own cost of its caller as in a parse from scratch. Its caller is
either memoized, whose own cost is counted once per stored result, or transient, which the same argument applies to.
The constant of a Document reparse is the constant of the parse from scratch, which already counts these calls.

## Reproduction

Deterministic numbers come from `go run ./bench/metrics collect` and `compare` at both commits, and the retained memory
from `bench/document-retained` (`GOGC=off GOMAXPROCS=1`, 100,000 lines).

## Results

Retained memory of a Document over 100,000 lines, against its tree:

| Subject | Tree | Document, before | Document, after | Document over tree, before | after |
|:--|--:|--:|--:|--:|--:|
| CSV | 119.2 MB | 254.0 MB | 243.3 MB (−4.2%) | 2.13 | 2.04 |
| Minilang | 183.4 MB | 656.4 MB | 448.5 MB (−31.7%) | 3.58 | 2.45 |

The Documents retain 4.1% (CSV) and 27.1% (Minilang) fewer objects. The Settings Document of the engine retains the
same: its grammar has no value-less lexical rule that a Document stored.

The generated Go library is 1.2 KB smaller (0.2%), the generated Go and TypeScript sources 27 bytes smaller and the
playground's WebAssembly 786 bytes larger compressed (0.01%), for the new field of the compiled rules. The fact is
found by the walk that already collects the calls of each rule, so compiling the synthetic grammars takes only 2 to 4
more steps, of 54,534 to 437,300.

Matching the characters again shows in the Document statistics. On the JSON Document of the incremental-performance
guide, the first parse evaluates 325,008 rule bodies instead of 315,007 and records no memo hit instead of 10,001;
after the edit it evaluates 10,025 bodies instead of 11 and reuses 5,006 results instead of 15,020: the blanks between
the reused elements run again, and each costs a scan of its characters instead of a lookup.

Timing: pending a measurement window.

## Correctness and Tradeoffs

The Documents of the engine, the virtual machines, generated Go and generated TypeScript decide the same rules from
the same fact of the compiled module. `TestDocumentEvaluatesValuelessLexicalRules` checks that a Document evaluates
such a rule at each call and records no hit for it, and the TypeScript context-unit tests check that TypeScript
Documents count the units the engine counts, including scans and class runs.

The cost of the change is the matching it repeats: a reparse matches the value-less lexical calls inside the rules it
evaluates again, where it looked up their results before. These calls are bounded by the parse from scratch, and each
scans the characters a lookup would have skipped.

## Decision and Limits

Proposed, with no setting: which results a Document stores fixes its compute units on every backend, so the exception
is part of the Document reuse rule, not a separable optimization (record 080, criterion 2). Adoption waits for the
edit latency of the Document benchmarks, whose p99 must not be worse.
