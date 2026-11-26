# 140. Compute Units Counted Down as Fuel

- **Status**: Rejected
- **Date**: 2026-11-26

## Problem and Mechanism

A generated parser that counts [compute units](../../spec/runtime/parsing.md#compute-units) keeps the count in
`Parser.Units` and compares it with `Parser.UnitsCheck` after every charge: each count site loads both fields, adds,
compares and stores. The count sites are among the hottest instructions of a parse under the default compute limit.

The candidate held `Fuel`, what the count lacked to the next check, instead: a charge took from `Fuel`, and a check
was due exactly when `Fuel` was negative, so a count site touched one field and tested its sign. The count was derived
where the limit, the observation interval, the compute report and the limit error needed it. The field layout is part
of the library protocol, so the candidate moved the protocol from 36 to 37 and removed the exported `Units` and
`UnitsCheck` fields of `Parser`.

## Applicability

| Execution profile | Status | Reason |
|:--|:--|:--|
| Generated Go with limit controls (the default) | Applied | Every count site |
| Generated Go with `-no-limits` | Not applicable | No count sites |
| Engine backends, generated TypeScript | Not applied | Their own counters |

## Reproduction

Base: `361b3fbe5`; candidate: `4ad68de00`, the change as one commit on that base. Main had moved 69 commits past the
base, including changes to the Go library and generator; the candidate was compared with its own parent so that the
result holds only the change. Go 1.27.1, Apple M3 Max on mains power, `GOMAXPROCS=16`, `GOGC=100`.

- Time: `go run ./cmd/timed` of syntaxir/bench `8ea663e` with `-base`, 10 rounds over `BenchmarkParse` (generated
  in both units and `generated_ast`, that is `Parse` and `ParseValue` with the default compute limit) of JSON, XML and
  both arithmetic grammars, in an exclusive measurement window (load average 8.9 before, 3.6 after). The A/A
  control was accepted: geomean +0.71%, two of twelve rows outside their spread, which make no claim.
- Allocations and bytes: the medians of `allocs/op` and `B/op` of the same run.
- Sizes: the generated parsers that the candidate commits, against the base's, and the non-test sources of the Go
  library. Binary sizes and compilation were not measured: the deterministic metrics did not exist at the base.

## Results

| Axis | Change | Uncertainty |
|:--|:--|:--|
| `Parse` and `ParseValue` time | No difference on the ten claimable rows; candidate ÷ base from 0.9906 to 1.0097, every 95% interval containing 1.0 | 10 rounds, A/A accepted |
| Allocations | Unchanged | Exact counts |
| Bytes allocated | −0.06% to +0.04% | Medians of 10 rounds |
| Generated parser source | JSON −558 bytes (−0.9%), XML −2,193 (−0.7%), calculator −166 (−0.5%), left-recursive calculator −361 (−0.9%); the `-no-limits` JSON parser unchanged | Exact |
| Go library source | +696 bytes | Exact |

## Correctness and Tradeoffs

The candidate passed its own tests, including a unit test of the equivalence of the fuel and the count at the check
boundary. It changed the library protocol, so every checkout of generated parsers had to be regenerated, and it
removed two exported fields of `Parser`.

## Decision and Limits

Rejected. It gave no measurable gain in time, allocations or bytes on any workload, so its protocol change and the
removal of `Parser.Units` and `Parser.UnitsCheck` buy nothing. The default compute limit costs 1.03–1.09× the time of
the parsers generated without limit controls, which bounds what any change to the count sites can gain.

Revisit when a later mechanism leaves count sites that are both hot and many, for example after one charge per
straight-line block, or when a workload with heavy counting shows more than 5% overhead of the limits on over off.
