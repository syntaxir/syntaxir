# Decisions and Recommendations

## Recommending

- Recommend the end state that is essentially right for the product's purpose, not the
  smallest or quickest change. Name that end state first, then propose stages toward it.
  Implementation time decides only order and staging. Unacceptable run-time cost is a real
  constraint: state it with measurements.
- Revisit an existing spec rule or earlier decision that looks accidental, instead of
  building around it.
- Each concept has one meaning. Reject options where a field or name means different things
  per producer or backend, or where "unknown" papers over a missing implementation. Vet
  options that members draft before passing them on as recommended.
- Before you present a revised design, diff it against the owner's own draft, and justify
  every departure.
- Judge an IR layer or mechanism by the design it enables, not by its current users.

## Asking the owner

- Ask the owner about unspecified language semantics with alternatives, trade-offs and a
  recommendation. Wait for the answer before dependent work. Members ask the leader.
- Before you ask anything, search the issue comments and design records for an earlier
  decision. Re-asking a settled question wastes the owner's attention.
- Record every decision durably at once: a "Decided: ..." comment on the issue, and the
  issue body or a design record. Chat is lost to compaction.
- Package or directory renames need the owner's OK before they land. Give the count of
  affected files and of files that actually import both colliding packages. Go allows
  repeated package names, and an import alias in the few files that import both is cheap.
  Do not rename to avoid a collision alone. If a clash matters, rename the less user-facing
  side.

## Priorities

- Core first-release work (language features, structure, bugs, docs, then optimization)
  comes before ecosystem work (language server, editors, playground), ready-made parsers and
  compatibility extensions (32-bit, other target languages).
- Within the core, package structure and features land first, so that only proofs and
  optimization remain. Infrastructure that optimizations run on is foundation, not
  optimization: pass pipelines, registries, verifiers, dumps, profiles, flags and analysis
  frameworks. Proof lanes run in parallel.
- File package-structure problems as they are found and fix them before the release. Never
  defer structure or naming work to "after the next train".
- Release scope is not cut to gain speed. Compress the schedule by parallel and stacked
  work.
- Before the first release, the formats (`.zpc`, JSON, bytecode) and APIs may change
  incompatibly. Add no compatibility shims or version bumps for compatibility alone, and
  remove such shims when you touch them.
- Code left unreachable by a change is deleted with the tests that exercise only it, in a
  separate commit whose body says why it is unreachable. Keep tests of live behavior.
- A failing alpha-parsers check is never deferred with the ready-made parser work.
  Regenerate and fix it at once; only new ready-made parser features wait.
- Do not propose paid test machines. Make tests faster and select affected tests as a
  standing effort. New slow tests go to the long tier; never weaken tests to save time.
