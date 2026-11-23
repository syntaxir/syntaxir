# 079. Derived Compute Budget

- **Status**: Partially Implemented: the derivation and its report have landed, and the derived value is not the default until the linear-cost proof confirms its constants
- **Author**: @ornew
- **Date**: 2026-11-22

## Summary

For a start rule that the classifier classifies as Linear, the classifier derives a bound on the compute units of a
parse, `c * n + k`, from the structure of the grammar, in the logical events of [record 076](076-logical-compute-units.md).
`sxir lint -classify` and `lint.Classify` report it. The default of `Limits.Compute` stays `max(2^20, 1024 * n)`
until the linear-cost proof confirms the constants of the derivation; then the derived bound, with a stated safety
factor, replaces the default for a Linear grammar. A parse with missing-token insertion is outside the classes, has no
such bound and keeps the default. A parse that exceeds its compute limit names its hotspot
([record 078](078-limit-error-hotspot.md)); it does not name the rule that was being evaluated when the limit was found
exceeded, which record 078 plans as `LimitError.Rule`.

## Motivation

The default `max(2^20, 1024 * n)` is a guess. It is too small for a grammar whose single evaluation costs more than it
(a grammar with 280 variables needs about 6.57 million units on a one-character input, because the memo key of each
call reads every variable), and too loose to stop an attack on a small grammar. [Record 064](064-grammar-complexity.md)
planned a constant `c_G` from the classifier, and [record 066](066-limits-by-purpose.md) reserved the place for it in the
defaults of `Compute`. Neither fixed the units or the formula. Record 076 fixes the units: the logical events, the same
on every backend. A default tighter than the true bound would reject valid inputs, so the bound must come from the
proof and not from measurements.

## Goals

- A bound that is a function of the grammar alone, in the units of record 076, the same on every backend.
- A bound that is reported before it is applied, so that its soundness can be tested against measured units.
- No effect on a parse until the proof confirms the constants: the default stays as it is and `Unlimited` stays
  explicit.
- An exceeded limit tells the user where to look, at no cost to a parse that stays within its limits.

## Non-Goals

- A derived bound for a grammar that is not Linear. A class of linear given an environment bound depends on the bound
  of the environment, and the other classes have no bound that a constant of the grammar gives. The caller sets
  `Compute` for them.
- Naming the rule that repeats its work when a limit is exceeded. [Record 078](078-limit-error-hotspot.md) does.
- Changing the proof. The constants below are the classifier's reading of the lemmas, to be confirmed.

## Design

### The Bound

For a grammar of the class Linear, with `K` the bound that the classification reports on the combinations of the values
of variables with finite values, the units of a parse of `n` position units are at most

`c * n + k`, with `c = K * sum over rules r of E(r) * B(r)` and `k = c + 2`.

`n` is the length of the input in the position unit of the parse, as it is for the default of `Compute`. The constants
count a literal in bytes and a character, class or `.` as four, the widest UTF-8 encoding, which is at least what a
code-point position unit advances, so one bound holds for both units.

`k` has two terms. The first is `c`: the proof bounds the cost by `K * (n + 1)` times a constant, and the `+ 1` is the
position at the start (and after the last unit) where the cost of a parse does not depend on the input. This is the
grammar-size term. `c` sums `E(r) * B(r)` over every rule that the start rule reaches, and `E(r)` is at least 1 for
each of them, so `c` contains one evaluation of every reached rule with the memo keys of its calls: a grammar whose
memo keys read 280 variables has a large `c` and therefore a large `k`, which is why the default of 2^20 rejects it on a
one-character input. The second term, 2, is the two events that no position carries: the entry call of the start rule
and the final syntax error of a failed parse (the first and the eighth rows of the table of record 076).

`B(r)` is the events of one evaluation of the body of `r`:

| Part of the body | Events |
| --- | --- |
| A call of a rule | 1, and 1 more for each variable of the callee's memo key if the callee is memoized |
| A literal | its length in bytes, and 1 for the syntax error that a failed match can build |
| A character, a class or `.` | 4, and 1 for the error |
| A capture, a predicate, an inline action | 1 |
| A recovery | 2: the syntax error that it builds, and the same error as one of the `P` errors that Lemma 14 charges once more at a take. Its skip expression is part of the body and counts as any other part |
| In a grammar with a recovery: a call | 1 more, for the list of recovered errors that the call may record when it reuses a result (the `A` of Lemma 14) |
| In a grammar with a recovery: the body of a rule | 1 more for the take at the store that ends its evaluation, and for an Operators Rule 1 more for each line (the `S` of Lemma 14) |
| The action of the rule | 1 |
| Inside a repetition | the events of its body count once more, for the iterations cover each position twice, and once more for each repetition that encloses it |

`E(r)` is the evaluations of the body of `r` at one position and level, for each combination of the variables:

| Rule | Evaluations |
| --- | --- |
| Memoized, or the start rule | 2 |
| In a left-recursion cycle of `m` rules | `2 * (m + 1)` |
| Any other | the sum, over its call sites, of the evaluations of the rule that holds the site |

Every constant comes from a lemma of [the proof](../../../spec/proofs/linear-cost.md) and is to be confirmed by it:

| Constant | Value | Lemma |
| --- | --- | --- |
| Evaluations of a memoized rule | 2 per combination, so `2K` in all | Lemma 3 |
| Cover of a repetition | 2: the spans of one scanning construct overlap only at their ends | Lemmas 4 and 13 |
| Growth steps of a cycle | one for each rule of the cycle, plus one | Lemma 5 |
| Evaluations of a rule that is not memoized | as often as its call sites in its callers | Lemma 10 |
| The memo key | 1 per variable, a constant for a grammar with no string variable | Lemma 2 and record 076 |
| A value that an action builds | at most linear in the spans of the captures it reads, counted 1 per node | Lemma 6 |
| The width of a class | 4 | the UTF-8 encoding |
| Units of the recovered errors | at most `P + A + S`: the errors built, the lists recorded and the takes | Lemma 14 |

The derivation applies to the class Linear only, where there is no string variable and `L = 0` in the theorem. A count
that passes what a counter holds makes the bound absent, not wrong.

### The Report

`sxir lint -classify` prints a line `budget: at most c * n + k compute units for an input of n position units`, and its
JSON output has a member `budget` with `perUnit` and `fixed`. `lint.Classification.Budget` carries the same two numbers.
All three are absent for a class other than Linear. They state that the bound is derived and is not the default.

### The Default

The default stays `max(2^20, 1024 * n)`. When the proof confirms the constants, the default of a parse of a grammar
that is Linear becomes the derived bound times a documented safety factor, and a grammar that is not Linear keeps
`max(2^20, 1024 * n)` with the class reported by `sxir lint -classify`. `Unlimited` disables the bound as it does today.

A parse with [missing-token insertion](../../../spec/language/pragmas.md#missing) keeps `max(2^20, 1024 * n)` whatever
the class of its grammar. Such a parse evaluates again, at every call, each rule that contains `#missing` directly or
through the rules it calls, and is outside the classes ([Classification](../../../spec/runtime/parsing.md#classification)),
so no bound of the form `c * n + k` holds for it. In the Linear grammar

```text
rule main = e $$
rule e = t "+" e / t
rule t = "(" e ")" #missing / "x"
```

whose derived bound is `70 * n + 72`, a parse of `d` nested parentheses around `x` with an insertion limit of 1 inserts
no token, and its units double with each level of `d`:

| d | Bound | Units without insertion | Units with insertion |
| --- | --- | --- | --- |
| 4 | 702 | 46 | 186 |
| 8 | 1,262 | 86 | 3,066 |
| 12 | 1,822 | 126 | 49,146 |
| 16 | 2,382 | 166 | 786,426 |

Without insertion a `#missing` literal is an ordinary literal, and the bound covers it.
The measured soundness of the bound (below) stays as a test after the switch.

## Alternatives

- **Calibrating `c` from measured units.** It would be tight on the corpus and unsound for any input that the corpus
  does not hold. Rejected: a default tighter than the true bound rejects valid inputs.
- **A bound for every class.** For the other classes no constant of the grammar bounds the cost.

## Validation

- The derivation has unit tests for each part of the table, with the memo key term checked against a grammar with and
  without a variable.
- The bound is at least the measured units of the Linear grammars of the logical-unit corpus on generated Go, in the
  units of record 076 (the benchmark package's soundness test): valid, truncated and damaged inputs, at several sizes, in both
  position units, for `Parse` and `ParseValue`. XML (unknown), chain (polynomial) and keyed (linear-env) have no budget.
  The test logs the largest ratio of measured units to the bound. The recover grammar recovers errors that the stores
  of the memo take and that reused results record; its units are those of the per-event reference and of the engine's
  backends on the same inputs (the logical-unit exactness tests):

  | Grammar | Bound | Inputs | Largest measured / bound |
  | --- | --- | --- | --- |
  | JSON | `634 * n + 636` | 68 | 0.019 |
  | policy | `54 * n + 56` | 5 | 0.055 |
  | recover | `136 * n + 138` | 53 | 0.058 |

  The bound is 17 to 53 times the largest count. A bound with half the constants still holds on this corpus, so the
  mutant divides them by 64 and must be exceeded by some parse of each grammar; this also fails a derivation that grows
  looser than that.
- The lint package's test of missing-token insertion classifies the grammar above as Linear, checks that the units
  without insertion stay within the bound and that those with insertion grow by at least 15 times for four more levels
  and pass the bound.
- The proof's confirmation of each constant is the gate of the default.

## Limitations

- The bound is on the units of record 076 and of the [Cost](../../../spec/runtime/parsing.md#cost) section that adopts
  them. A backend that still charges the earlier per-operation units can report `Used` above it; the report is true of
  a backend once it has adopted them, and the measured-soundness test (Validation) is its check on generated Go.
- The bound is loose: it adds every part of every rule, and takes `2K` for each rule.
- It holds only if the constants of the table hold. Until the proof confirms them it is a report.
