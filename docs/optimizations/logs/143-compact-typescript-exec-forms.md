# 143. Compact TypeScript Exec Forms

- **Status**: Adopted
- **Date**: 2026-11-27
- **Mechanisms**: [G240](../mechanisms/G240-typescript-exec-forms.md)
- **Flags**: `ts-merged-records`, `ts-tested-inits`
- **Decides**: `ts-merged-records` adopted, `ts-tested-inits` adopted

## Problem and Mechanism

Two Exec passes of the TypeScript backend replace operations with a form that the TypeScript library runs more
compactly. `ts-merged-records` makes the end of an expectation record and the merge of its expectations into the
enclosing record one `unisolateMerge`, which merges in place instead of copying the expectations and holds no local
for them. `ts-tested-inits` makes an `if` that tests whether its first call matched test the call's result in its
condition, without the boolean local the call sets. The printer prints the replaced operations as well, so each flag
turns its pass off and still prints a parser that returns and charges what it does with the pass.

## Reproduction

The generated sizes were taken on `771138ae6` (Go 1.27.1, Apple M3 Max). `sxir gen -lang go,ts -lib embed` generated the
ten language grammars of `testdata/grammars` with the options of `testdata/grammars/generate.txt` (with `-lib embed`
in place of `-lib import`, so that the embedded library is part of the output) once with the `speed` profile and once
with the flag off. Sizes are bytes: `parser.go`, the whole Go output (the parser and its embedded library), and the
whole TypeScript output. A ratio is the size with the flag off over the size with it on.

## Axes Measured

| Axis | Workloads | Result |
|:--|:--|:--|
| Time | — | Not measured: not measured: the flag postdates the last timed run of the generation flags (record 136); the next measurement window times it; the TypeScript parse time is outside the harness of record 136 |
| Allocations, bytes allocated, retained memory | — | Not measured; `ts-merged-records` saves a copy of the expectations per merge |
| Generated code size | The ten language grammars, TypeScript | `ts-tested-inits` off: 1.036 times larger (geometric mean; every grammar, up to 1.086). `ts-merged-records` off: unchanged within 0.1% (four grammars change) |
| Compile time | — | Not measured |

`ts-tested-inits`:

| Grammar | TypeScript output, speed | TypeScript output, `-no-opt ts-tested-inits` | ratio |
|:--|--:|--:|--:|
| `cel` | 513,618 | 524,407 | 1.021 |
| `csv` | 335,693 | 336,455 | 1.002 |
| `cue` | 919,617 | 957,178 | 1.041 |
| `duckdb` | 3,018,081 | 3,276,173 | 1.086 |
| `golang` | 1,206,778 | 1,265,589 | 1.049 |
| `json` | 359,771 | 362,342 | 1.007 |
| `python` | 1,315,462 | 1,373,726 | 1.044 |
| `typescript` | 1,968,219 | 2,076,861 | 1.055 |
| `xml` | 620,220 | 635,217 | 1.024 |
| `yaml` | 933,265 | 961,086 | 1.030 |
| geometric mean | | | 1.036 |

`ts-merged-records`:

| Grammar | TypeScript output, speed | TypeScript output, `-no-opt ts-merged-records` | ratio |
|:--|--:|--:|--:|
| `cel` | 513,618 | 513,618 | 1.000 |
| `csv` | 335,693 | 335,693 | 1.000 |
| `cue` | 919,617 | 919,617 | 1.000 |
| `duckdb` | 3,018,081 | 3,018,081 | 1.000 |
| `golang` | 1,206,778 | 1,206,532 | 1.000 |
| `json` | 359,771 | 359,771 | 1.000 |
| `python` | 1,315,462 | 1,316,555 | 1.001 |
| `typescript` | 1,968,219 | 1,967,949 | 1.000 |
| `xml` | 620,220 | 620,200 | 1.000 |
| `yaml` | 933,265 | 933,265 | 1.000 |
| geometric mean | | | 1.000 |

## Decision

Both are adopted in `speed` and every profile. `ts-tested-inits` makes the TypeScript output 3.6% smaller at no cost:
the condition calls what the local held. `ts-merged-records` changes the size by less than 0.1%; it is kept for the
copy of the expectations that each merge saves, which the next timed window measures. The record that times it
reviews that.
