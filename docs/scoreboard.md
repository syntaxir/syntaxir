# Performance Scoreboard

Each row of the target table in [the strategy](strategy.md#performance-leadership) against the saved results of
[the benchmarks](benchmarks.md), measured at commit `13b3d186` on 2026-10-29. A row is **met** or **not met** when the results
decide its Credibility target, and **open** when no benchmark measures it yet or the target is not a number; an open row
says what is missing. Where a target gives a stretch value, the standing is judged against the commitment.

This file is generated: `go run ./bench/report -scoreboard` rewrites it from `bench/results*.txt` without running
any benchmark, and a test checks that it matches the results and the strategy table. Do not edit it by hand.

| Measure (output class) | Credibility target | Measured | Standing |
|:--|:--|:--|:--|
| Compute limit overhead, generated Go `Parse` and `ParseValue`, limits on ÷ off | ≤ 1.15×; ≤ 1.05× (stretch) | 1.11–1.61× | not met |
| Typed `ParseValue` ÷ standard library, JSON and XML (typed values with spans) | < 1.0× | 1.11–1.33× | not met |
| Typed `ParseValue` ÷ standard library, JSON and XML (typed values without spans, byte positions) | ≤ 1.0× | not measured: no benchmark generates values without spans | open |
| Typed `ParseValue` ÷ standard library, CSV (without spans) and arithmetic (with spans, against `go/parser`'s positioned AST) | ≤ 2.0× | 3.10–3.36× | not met |
| Generated `Parse` ÷ standard library, comparable workloads (a positioned tree with rule names, a richer output than the baselines) | ≤ 2.0× | 1.62–6.76× | not met |
| Generated `Recognize` ÷ `json.Valid` (acceptance and error offset, no expected sets) | ≤ 3.0× | 8.05× | not met |
| Allocations per generated `Recognize`, pooled parser in steady state | 0 on JSON, CSV and arithmetic (met today) | JSON 0, CSV 0, Arith_OperatorsRule 0 allocs/op | met |
| Allocations per typed `ParseValue` | Count grows no faster than the logarithm of the input (slab growth only) | not measured: each workload has one input size, so growth with the input is not measured | open |
| Typed allocated bytes ÷ standard library, JSON and XML | ≤ 1.0× (with spans) | 1.18–1.25× | not met |
| Generated TypeScript `parse` ÷ generated Go `Parse` | ≤ 2.0× | 2.53–4.19× | not met |
| Generated Go streaming ÷ `encoding/csv` read loop (benchmark to be added) | ≤ 2.0× (stretch) | not measured for generated Go; closure `ParseStream` is 11.5× | open |
| `Document` edit plus reparse at 100,000 lines, p99 | Same-length edits ≤ 1 ms; length-changing edits ≤ 2 ms (stretch) | not measured: the incremental benchmarks report means, not a p99 at 100,000 lines | open |
| `Document` memory ÷ tree memory | ≤ 3× | not measured: no benchmark compares the retained memory of a `Document` with the tree | open |
| Short-input latency (≤ 1 KB) ÷ the faster of pigeon and participle, same grammar and output class | ≤ 1.0×; ≤ 0.7× (stretch) | not measured: pigeon and participle are not in the harness | open |
| Minimal generated package and binary-size delta | Measured and published against pigeon, participle and ANTLR Go | not measured: sizes are measured for SyntaxIR only, not against pigeon, participle or ANTLR Go | open |
| Size profile, DuckDB `parser.go` | ≥ 15% smaller than the speed profile | not measured: there is no size profile and no DuckDB grammar benchmark | open |
| Cross-tool batch parsing (harness below), same output class | Published, with losses reported | not measured: the cross-tool harness is not built | open |
| Cross-tool incremental edit latency, same traces and output class | Published against gotreesitter and gpeg | not measured: the cross-tool harness is not built | open |

1 met, 7 not met, 10 open.
