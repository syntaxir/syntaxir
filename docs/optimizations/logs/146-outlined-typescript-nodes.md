# 146. Outlined TypeScript Nodes

- **Status**: Adopted
- **Date**: 2026-11-27
- **Mechanisms**: [G220](../mechanisms/G220-outlined-typescript-nodes.md)
- **Flags**: `outline`
- **Decides**: `outline` adopted

## Problem and Mechanism

A TypeScript function printed in one piece grows with the grammar, and a function of thousands of lines is neither
compact nor fast for a JavaScript engine to compile. The pass `outline` chooses the nodes of each Node and recognizer
table that run as functions of their own: the nodes whose structure occurs in more than one place of the module, which
then share one function, and the large ones, so that no function grows beyond a bound. It runs in the TypeScript
pipeline only. The choice was made inside the planning of the tables before it became a pass, with byte-identical
output; this record decides its state as a flag.

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
| Allocations, bytes allocated, retained memory | — | Not measured |
| Generated code size | The ten language grammars, TypeScript | The TypeScript output is 1.055 times larger with the flag off (geometric mean), `duckdb` 1.410 times; three grammars are up to 1.4% smaller |
| Compile time | — | Not measured |

| Grammar | TypeScript output, speed | TypeScript output, `-no-opt outline` | ratio |
|:--|--:|--:|--:|
| `cel` | 513,618 | 518,584 | 1.010 |
| `csv` | 335,693 | 333,990 | 0.995 |
| `cue` | 919,617 | 957,921 | 1.042 |
| `duckdb` | 3,018,081 | 4,256,679 | 1.410 |
| `golang` | 1,206,778 | 1,311,080 | 1.086 |
| `json` | 359,771 | 358,574 | 0.997 |
| `python` | 1,315,462 | 1,390,582 | 1.057 |
| `typescript` | 1,968,219 | 2,024,640 | 1.029 |
| `xml` | 620,220 | 619,317 | 0.999 |
| `yaml` | 933,265 | 920,361 | 0.986 |
| geometric mean | | | 1.055 |

## Decision

Adopted in `speed` and every profile, for generated code size and for the size of the functions the JavaScript engine
compiles: shared outlined functions make the TypeScript output 5.5% smaller in the geometric mean, and much smaller for
the grammars with many repeated parts.
