# 138. Typed-Only Output of Generated TypeScript

- **Status**: Implemented
- **Date**: 2026-11-24

## Problem and Mechanism

A Go parser generated with `-types -typed-only` leaves out `Parse`, `ParseRule` and the code that builds untyped
`*Node` values, which about halves the source of a large grammar. TypeScript has the same flag. This record states what
it leaves out and measures it.

`-typed-only` (`compiler.TSConfig.TypedOnly`) needs `-types`. It generates `parseValue`, `parseValueWithOptions`,
`parseRuleValue` and `parseRuleValueWithOptions`, and no `parse`, `parseRule`, `parseWithOptions` or
`parseRuleWithOptions`; `-recognize` still adds `recognize`. It is refused with `-document`, `-stream` and
`-recognize-only`, which build or are the untyped tree, with `-default-value`, which `-types` already refuses, and for
a grammar that uses `#missing`, which generated parsers do not support. The flag is off by default, as it is for Go:
a typed grammar keeps its tree entry points unless the flag is given.

A generated TypeScript parser builds a typed value by converting the Node that the Node rules of the rule build.
Those rules are what the typed entry points run, so `-typed-only` keeps them: it removes the entry points and
nothing else. The saving is therefore a constant, not a fraction of the grammar.

## Measurement

`parser.ts` of the ten language grammars of `testdata/grammars`, generated with the options of
`testdata/grammars/generate.txt` for TypeScript (`-types`, and `-recognize`, `-expectations` or `-no-limits` where the
line has them), with and without `-typed-only`. `lib.ts` and `types.ts` are byte-identical in the two builds. Sizes are
bytes, raw and compressed with `gzip -9`. Taken on `7747f7168`.

| Grammar | raw | raw, typed-only | saved | gzip | gzip, typed-only | saved |
|:--|--:|--:|--:|--:|--:|--:|
| `json` | 62,017 | 60,203 | 1,814 (2.93%) | 9,233 | 8,983 | 250 (2.71%) |
| `csv` | 36,258 | 34,444 | 1,814 (5.00%) | 7,277 | 7,026 | 251 (3.45%) |
| `xml` | 291,850 | 290,036 | 1,814 (0.62%) | 32,470 | 32,222 | 248 (0.76%) |
| `yaml` | 586,828 | 585,014 | 1,814 (0.31%) | 57,982 | 57,738 | 244 (0.42%) |
| `cel` | 185,630 | 183,737 | 1,893 (1.02%) | 21,435 | 21,158 | 277 (1.29%) |
| `cue` | 578,064 | 576,171 | 1,893 (0.33%) | 62,573 | 62,305 | 268 (0.43%) |
| `golang` | 851,359 | 849,466 | 1,893 (0.22%) | 81,705 | 81,443 | 262 (0.32%) |
| `python` | 934,203 | 932,310 | 1,893 (0.20%) | 102,861 | 102,574 | 287 (0.28%) |
| `typescript` | 1,489,860 | 1,487,967 | 1,893 (0.13%) | 141,707 | 141,413 | 294 (0.21%) |
| `duckdb` | 2,565,228 | 2,563,331 | 1,897 (0.07%) | 193,291 | 193,004 | 287 (0.15%) |

The saving is 1.8 to 1.9 KB raw and about 250 to 290 bytes compressed for every grammar, 0.07% to 5% of `parser.ts`
and 0.15% to 3.5% compressed.

## Reproduction

```sh
go build -o /tmp/sxir ./cmd/sxir
for g in json csv xml yaml cel cue golang python typescript duckdb; do
  opts=$(awk -v g=$g '$1==g {$1=""; print}' testdata/grammars/generate.txt | sed 's/-nodoc//; s/-typed-only//; s/-lib import//')
  for extra in "" "-typed-only"; do
    /tmp/sxir gen -lang ts -g testdata/grammars/$g/$g.sxir $opts $extra -o /tmp/ts-$g$extra
    echo "$g $extra $(wc -c < /tmp/ts-$g$extra/parser.ts) $(gzip -9 -c /tmp/ts-$g$extra/parser.ts | wc -c)"
  done
done
```

## Tradeoffs

The flag is worth using to keep `parse` and `parseRule` out of an API, not to shrink a bundle. A saving like Go's would
need a typed TypeScript parser that builds its values directly, without the Node rules; this record does not do that.
