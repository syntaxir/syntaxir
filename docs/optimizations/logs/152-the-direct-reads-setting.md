# 152. The Direct-Reads Setting

- **Status**: Adopted
- **Date**: 2026-11-28
- **Mechanisms**: [M080](../mechanisms/M080-direct-code-point-access.md)
- **Flags**: `direct-reads`
- **Decides**: `direct-reads` adopted

## Problem and Mechanism

A direct generated Go body reads the next character in many places: character classes, `.`, scan loops and the
first-character tests of choices. Record 061 made those reads cheap. A body that only ordinary parses run calls
`NextOrdinary`, a library read small enough for the compiler to inline. A body that a Document may also run reads
`p.In[p.Pos]` in place when the parse has no Document and the position is inside the decoded input, and calls `Peek`
otherwise. The read had no control of its own: the only way to turn it off was a body-lowering control, which removes
the whole direct route.

The printer setting `direct-reads` is that control. When it is off, every read is the library's plain read:
`PeekOrdinary` in an ordinary parse and `Peek` where a Document may run the body. Everything else about the body is
unchanged, so a comparison isolates the read. The in-place literal comparisons have a separate control,
`inline-literals`. Each printed read is a site of the setting, numbered in the order the printer prints them, so
bisection can turn off a single read. A traced body already reads through the observed `Peek`, and the setting leaves
it alone.

## Reproduction

The deterministic figures come from the settings part of the metrics on `02783a9d3` plus this change (Go 1.27.1,
Apple M3 Max):

```sh
go run ./bench/metrics collect -parts settings -o settings.txt
```

For each profile, the part generates the ten language grammars of `testdata/grammars` and the witness `text-compare`
twice: once with the profile's own values and once with `-no-opt direct-reads`. It records the variant's value minus
the profile's for steps, source, stripped binary, allocations and retained memory.

## Axes Measured

| Axis | Workloads | Result |
|:--|:--|:--|
| Time | — | Not measured. Parse time needs a measurement window, and the next window times the setting. Record 061 measured the mechanism, before it had a control, at −12% for the JSON `ParseValue` and −10% for the XML one |
| Allocations, bytes allocated | The inputs of the language-grammar matrix, every API | No difference in any profile |
| Retained memory | `ParseValue` | No difference beyond the part's noise (at most ±192 bytes and ±4 objects, in both directions) |
| Generated source | All eleven grammars | Byte count unchanged in every profile, since `PeekOrdinary` and `NextOrdinary` have the same length. The setting changes the parser of every grammar (`setting-identical-grammars … go 0`) |
| Stripped binary | All eleven grammars | 0 to 181,632 bytes smaller with the setting off (table below) |
| Compile steps | All eleven grammars | No difference |

Stripped binary, setting off minus the profile, in bytes:

| Grammar | `speed` | `memory` | `size` |
|:--|--:|--:|--:|
| `cel` | −16,512 | −16,512 | 0 |
| `csv` | 0 | 0 | 0 |
| `cue` | −16,512 | −16,512 | 0 |
| `duckdb` | −181,632 | −181,632 | −115,584 |
| `golang` | −16,512 | −16,512 | −16,512 |
| `json` | 0 | 0 | 0 |
| `python` | −16,512 | −16,512 | −16,512 |
| `text-compare` | 0 | 0 | 0 |
| `typescript` | −33,024 | −33,024 | −33,024 |
| `xml` | −16,512 | −16,512 | −16,512 |
| `yaml` | −16,512 | −16,512 | −16,512 |

## Decision

Adopted in every profile, where it stays on. It is the read that record 061 measured at a 10% to 12% faster
`ParseValue`, and its only measured cost is the binary: one or two 16 KiB steps for most grammars, and at most 182 KB
for `duckdb`. The `size` profile trades a little parse time for size. Whether this binary saving is worth the read's
cost in that profile depends on the parse time, which no measurement window has timed yet. Until a window times it,
the profile keeps the mechanism as it was.
