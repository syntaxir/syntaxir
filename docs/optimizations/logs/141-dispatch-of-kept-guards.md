# 141. Dispatch of the Kept Guards

- **Status**: Adopted
- **Date**: 2026-11-27
- **Mechanisms**: [M010](../mechanisms/M010-first-character-dispatch.md)
- **Flags**: `dispatch`
- **Decides**: `dispatch` adopted

## Problem and Mechanism

The pass `guards` gives each alternative of a choice the set of characters that can start it. Which alternatives keep
that guard, so that the parser skips them when the next character cannot start them, depends on what a skip must
leave: the effects of the calls it passes over, and in a Document lowering no skip over a call of a memoized rule.
The pass `dispatch` makes that choice on the Core IR of each table right before the program is planned and sets the
guarded alternatives in the lowering options, which the lowerings of the VMs, Go and TypeScript read. It keeps the
guard of an alternative only where skipping the alternative leaves the state its evaluation would leave and records
the same expectations. Off, no alternative keeps its guard and the parser tries every alternative, as with
`-no-opt guards`. The parser of the closure engine is the same with the flag off: it is built from the Core IR, which
keeps the guards of `guards`.

The choice was made inside the lowering before it became a pass; the commit that made it a pass left every generated
parser byte-identical. Its effect on time is the effect of the guards, which record 136 measured with `-no-opt
guards`: off, parse time is 1.091 times that of `speed` over the ten language grammars, every one of them slower.

## Reproduction

The generated sizes were taken on `771138ae6` (Go 1.27.1, Apple M3 Max). `sxir gen -lang go,ts -lib embed` generated the
ten language grammars of `testdata/grammars` with the options of `testdata/grammars/generate.txt` (with `-lib embed`
in place of `-lib import`, so that the embedded library is part of the output) once with the `speed` profile and once
with the flag off. Sizes are bytes: `parser.go`, the whole Go output (the parser and its embedded library), and the
whole TypeScript output. A ratio is the size with the flag off over the size with it on.

## Axes Measured

| Axis | Workloads | Result |
|:--|:--|:--|
| Time | The ten language grammars | Not measured for this flag: not measured: the flag postdates the last timed run of the generation flags (record 136); the next measurement window times it. Turning the guards off, which this flag off is the same as for the generated parsers, costs 9.1% of parse time (record 136) |
| Allocations, bytes allocated, retained memory | — | Not measured: a guard reads the next character and allocates nothing |
| Generated code size | The ten language grammars, Go and TypeScript | `parser.go` 0.945 and the TypeScript output 0.952 with the flag off (geometric means; tables below): the kept guards cost 5.5% of Go source |
| Compile time | — | Not measured |

| Grammar | parser.go, speed | parser.go, `-no-opt dispatch` | ratio | TypeScript output, speed | TypeScript output, `-no-opt dispatch` | ratio |
|:--|--:|--:|--:|--:|--:|--:|
| `cel` | 122,973 | 116,937 | 0.951 | 513,618 | 496,571 | 0.967 |
| `csv` | 29,732 | 28,825 | 0.969 | 335,693 | 334,484 | 0.996 |
| `cue` | 447,083 | 397,260 | 0.889 | 919,617 | 847,414 | 0.921 |
| `duckdb` | 1,844,978 | 1,733,741 | 0.940 | 3,018,081 | 2,703,930 | 0.896 |
| `golang` | 639,251 | 609,614 | 0.954 | 1,206,778 | 1,150,560 | 0.953 |
| `json` | 60,132 | 56,958 | 0.947 | 359,771 | 352,986 | 0.981 |
| `python` | 754,720 | 727,628 | 0.964 | 1,315,462 | 1,262,598 | 0.960 |
| `typescript` | 1,091,432 | 1,034,052 | 0.947 | 1,968,219 | 1,833,719 | 0.932 |
| `xml` | 321,223 | 297,410 | 0.926 | 620,220 | 592,576 | 0.955 |
| `yaml` | 595,121 | 574,393 | 0.965 | 933,265 | 895,592 | 0.960 |
| geometric mean | | | 0.945 | | | 0.952 |

## Decision

Adopted in `speed` and every profile, for parse time: the guards that it keeps are what makes the parse 9% faster,
and their price in source size is the one record 136 accepted for `guards`. The `size` profile keeps it, as it keeps
`guards`.
