# 078. Hotspot of a Limit Error

- **Status**: Implemented on the engine, generated Go and generated TypeScript; the rule at the check (`LimitError.Rule`) is planned and not built
- **Author**: @ornew
- **Date**: 2026-11-22

## Summary

A parse that exhausts its compute limit reports the totals only, so the author of a grammar cannot tell which rule repeats
its work. A `LimitError` of the kind `LimitCompute` names `Hotspot`: the rule that the profile of the parse ranks first.
Every backend that has a profile does the same, and the error text names the rule.

## Design

The hotspot is the rule of the profile with the most repeated evaluations at one position and level (`MaxEvals`, at least
2), ties going to more `Repeats` and then to the smaller name ([record 077](077-observability-contract.md) defines the
columns). It is empty when the parse has no profile, or no rule was evaluated twice at one position and level. A profile
that earlier parses also filled ranks all of them.

The ranking is computed when a check finds the limit exceeded, on the cold path, so a parse that stays within its limits
pays nothing for it. Per-rule counters on every path would name the hotspot without a profile but cost an addition on each
charge, against the target of [record 076](076-logical-compute-units.md) that limit controls cost at most 1.10 times.
Re-running the failed parse with a profile is not possible for a generated parser, which has no interpreter to re-run with.

| Backend | Where it reads the profile |
| --- | --- |
| Engine | `Parser.Hotspot`, the first profile of the parse, through the control that every parse attaches |
| Generated Go | the first profile of the observation of the parse (`BudgetControl.observe`) |
| Generated TypeScript | `hotspotOf` on the first profile of the invocation; `ParseInterruptionError.hotspot` |

The control keeps the parser or observation whatever the memory limit; the memory attachment alone would name the hotspot
only for a parse with a memory limit.

`Rule`, the rule that was being evaluated when a check found the limit exceeded, is planned. The checks sit at recursive
calls and loop back-edges, so which check finds the excess can differ between backends, like `Used`; only the total and
the decision agree. The charge sites do not know their rule today, so `Rule` waits for step 4 of record 076 and is not in
the specification until then.

## Validation

Each backend has a test that fails the parse one unit below what it needs, so that the failing check comes after every
evaluation, and checks the profiled parse (it names the rule) and the unprofiled one (empty). The ranking has a unit test
with its tie-breaks.
