# 144. Library of the Used Features

- **Status**: Adopted
- **Date**: 2026-11-27
- **Mechanisms**: [G250](mechanisms/G250-used-library-features.md)
- **Flags**: `used-features`
- **Decides**: `used-features` adopted

## Problem and Mechanism

A generated parser that embeds its library carries every feature of it unless the generator leaves the unused ones
out: the growth of left-recursion seeds, recovery, Operators Rules, streams, Documents and the limit controls. The
feature analysis decides once per generation which features a parser can use, from the options and from the rules its
entry points reach, and over-approximates. With `used-features` on, the embedded Go and TypeScript library keep the
code of those features only, and the declarations that the parser names. A build tag makes a generated parser panic
when a parse uses a feature outside its set; the generated-parser tests build with it.

## Reproduction

The generated sizes were taken on `771138ae6` (Go 1.27.1, Apple M3 Max). `sxir gen -lang go,ts -lib embed` generated the
ten language grammars of `testdata/grammars` with the options of `testdata/grammars/generate.txt` (with `-lib embed`
in place of `-lib import`, so that the embedded library is part of the output) once with the `speed` profile and once
with the flag off. Sizes are bytes: `parser.go`, the whole Go output (the parser and its embedded library), and the
whole TypeScript output. A ratio is the size with the flag off over the size with it on.

## Axes Measured

| Axis | Workloads | Result |
|:--|:--|:--|
| Time | — | Not measured: not measured: the flag postdates the last timed run of the generation flags (record 136); the next measurement window times it. A parse runs the same code either way; only the library around it differs |
| Allocations, bytes allocated, retained memory | — | Not measured: the code a parse runs is the same |
| Generated code size | The ten language grammars, Go and TypeScript, `-lib embed` | With the flag off, the Go output is 1.041 times larger and the TypeScript output 1.187 times (geometric means), the TypeScript output of `csv` and `json` about 1.45 times. `parser.go` itself is unchanged |
| Compile time | — | Not measured |

| Grammar | Go output, speed | Go output, `-no-opt used-features` | ratio | TypeScript output, speed | TypeScript output, `-no-opt used-features` | ratio |
|:--|--:|--:|--:|--:|--:|--:|
| `cel` | 708,046 | 756,892 | 1.069 | 513,618 | 645,402 | 1.257 |
| `csv` | 611,612 | 649,996 | 1.063 | 335,693 | 490,327 | 1.461 |
| `cue` | 1,050,364 | 1,097,548 | 1.045 | 919,617 | 1,046,125 | 1.138 |
| `duckdb` | 2,644,819 | 2,692,124 | 1.018 | 3,018,081 | 3,145,318 | 1.042 |
| `golang` | 1,267,928 | 1,316,649 | 1.038 | 1,206,778 | 1,336,588 | 1.108 |
| `json` | 645,253 | 682,432 | 1.058 | 359,771 | 517,703 | 1.439 |
| `python` | 1,427,138 | 1,474,485 | 1.033 | 1,315,462 | 1,441,567 | 1.096 |
| `typescript` | 1,896,231 | 1,943,699 | 1.025 | 1,968,219 | 2,096,434 | 1.065 |
| `xml` | 936,323 | 967,359 | 1.033 | 620,220 | 756,965 | 1.220 |
| `yaml` | 1,194,078 | 1,223,577 | 1.025 | 933,265 | 1,048,198 | 1.123 |
| geometric mean | | | 1.041 | | | 1.187 |

## Decision

Adopted in `speed` and every profile, for generated code size: a parser that embeds its library is 4% smaller in Go
and 19% smaller in TypeScript, most for the small grammars, and a parse runs the same code.
