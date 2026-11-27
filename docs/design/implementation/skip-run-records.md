# Skip-Run Records

<!-- former-mechanism R180: The specification defines skip-run sites, their records and the units of their walks, the same on every backend; a walk that takes a record neither performs nor counts what it skips -->

The walks of unbounded repetitions in `#recover` skip expressions share the rest of their walks, so skips that start at
many positions of one input walk each position a bounded number of times. The same records serve the run sites and the
scan sites outside skip expressions.

## Design and Invariants

A skip-run site is an unbounded repetition in a skip expression whose rest of the walk from a position depends only on
the position and the input: no predicate that reads or binds a variable, no `#cut` that applies to the repetition, and
no called rule that reads a variable, contains `#recover` or belongs to a left-recursion cycle. For each site and
lookahead state, each position has a slot that is fresh, visited or recorded: a bit marks the slots that are not fresh,
and a map holds the records. A walk marks the position of each iteration it moves past until it reaches one that is
not fresh, and records from there on, so a walk that fails at once touches no slot. When a recording walk ends, it
stores the rest of the walk (end, farthest failure and expectations, examined range) for each position it visited while
recording, backward from the last. A recording walk that has matched its minimum reuses a recorded slot as a memo hit.
Records are stored only when a walk ends, so a stopped parse leaves none. Each reused record equals the rest of the
walk it replaces, and each position is evaluated at most twice, plus the minimum and one more for each walk
([proof](../../../spec/proofs/skip-runs.md)).

A repetition in a rule that a skip expression calls is not a skip-run site. Outside skip expressions, the records
extend to [run sites](../../../spec/runtime/parsing.md#run-sites), repetitions that a failed alternative walks again,
whose records also hold the elements of the rest of the walk and may be keyed by variables, and to
[scan sites](../../../spec/runtime/parsing.md#scan-sites), repetitions of one character class or any character in a
rule that calls no rule.

## Scope

All backends, Documents and streams. Slots last for one parse; streams drop those before a commit. A parse with
missing-token insertion keeps no slots. A generated Document resumes no run of a run site: each walk walks as in a
parse of the text alone.

## Why It Is a Design

The [parsing specification](../../../spec/runtime/parsing.md#skip-runs) defines the sites, the slots and the records,
and a walk that takes the rest of the walk from a record neither performs nor counts the iterations and the input that
it skips ([Compute Units](../../../spec/runtime/parsing.md#compute-units)). A parser without records would count
differently, so it is not a maintained form of the same contract. The tests compare against parses with records
disabled, a setting of each parse that only the tests use, and mutate the runtimes to never record or to store records
early.

## Evidence

Evidence record: [132](../../optimizations/logs/132-records-of-recovery-skip-walks.md).

Packages: `internal/middleend/corepass` (the site analysis), `pkg/runtime/libgo` (the core of skip walks, shared by the
engine and the generated Go runtime), `internal/runtime/state` (the hooks of the engine), `internal/ir/code/flow` and
`internal/middleend/flowlower` (the Flow IR operations and their lowering) and the TypeScript library.

Related: [left-recursion member memoization](left-recursion-member-memo.md),
[Document repetition records](document-repetition-records.md).
