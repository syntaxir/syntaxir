package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The scoreboard states, for each row of the target table in docs/strategy.md, whether the saved
// results meet the Credibility target. Each row of the table has one entry below, keyed by its
// measure and target text; TestScoreboardFollowsStrategy parses the table of the document and
// fails when a row has no entry, an entry has no row, or the texts differ. A row that no benchmark
// measures says so and why, so a gap shows as a gap instead of as a missing row.

// state is the standing of a row against its target.
type state string

const (
	met    state = "met"
	missed state = "not met"
	open   state = "open" // not measured, or the target is not a number the results can decide
)

// A scoreEntry is one row of the target table: the text of its first two cells and the evaluation.
type scoreEntry struct {
	measure, target string
	eval            func(c *figureCalc) (value string, st state)
}

// notMeasured is the standing of a row without a benchmark.
func notMeasured(reason string) func(c *figureCalc) (string, state) {
	return func(*figureCalc) (string, state) { return "not measured: " + reason, open }
}

// atMost is the standing of the largest of xs against a limit; strict makes the limit exclusive.
func atMost(limit float64, strict bool, xs []float64, format func([]float64) string) (string, state) {
	hi := slices.Max(xs)
	st := met
	if hi > limit || (strict && hi >= limit) {
		st = missed
	}
	return format(xs), st
}

func (c *figureCalc) stdRatios(typed bool, metric func(*result) float64, keys ...string) []float64 {
	var xs []float64
	for _, s := range stdBaselines {
		if len(keys) > 0 && !slices.Contains(keys, s.key) {
			continue
		}
		num := "Parse/" + s.workload + "/generated/codepoints"
		if typed {
			num = "Parse/" + s.workload + "/generated_ast"
		}
		xs = append(xs, metric(c.get(num))/metric(c.get(s.bench)))
	}
	return xs
}

var scoreboard = []scoreEntry{
	{"Compute limit overhead, generated Go `Parse` and `ParseValue`, limits on ÷ off",
		"≤ 1.15×; ≤ 1.05× (stretch)",
		func(c *figureCalc) (string, state) {
			// ControlledParse runs with finite limits; the ordinary Parse benchmarks run without a control.
			var xs []float64
			for _, w := range []string{"JSON", "XML"} {
				for _, u := range []string{"codepoints", "bytes"} {
					xs = append(xs, c.get("ControlledParse/"+w+"/generated/"+u).ns/c.get("Parse/"+w+"/generated/"+u).ns)
				}
				xs = append(xs, c.get("ControlledParse/"+w+"/generated_ast/codepoints").ns/c.get("Parse/"+w+"/generated_ast").ns)
			}
			return atMost(1.15, false, xs, factorRange)
		}},
	{"Typed `ParseValue` ÷ standard library, JSON and XML (typed values with spans)",
		"< 1.0×",
		func(c *figureCalc) (string, state) {
			return atMost(1.0, true, c.stdRatios(true, nsOf, "json", "xml"), factorRange)
		}},
	{"Typed `ParseValue` ÷ standard library, JSON and XML (typed values without spans, byte positions)",
		"≤ 1.0×",
		notMeasured("no benchmark generates values without spans")},
	{"Typed `ParseValue` ÷ standard library, CSV (without spans) and arithmetic (with spans, against `go/parser`'s positioned AST)",
		"≤ 2.0×",
		func(c *figureCalc) (string, state) {
			return atMost(2.0, false, c.stdRatios(true, nsOf, "csv", "operators"), factorRange)
		}},
	{"Generated `Parse` ÷ standard library, comparable workloads (a positioned tree with rule names, a richer output than the baselines)",
		"≤ 2.0×",
		func(c *figureCalc) (string, state) {
			return atMost(2.0, false, c.stdRatios(false, nsOf), factorRange)
		}},
	{"Generated `Recognize` ÷ `json.Valid` (acceptance and error offset, no expected sets)",
		"≤ 3.0×",
		func(c *figureCalc) (string, state) {
			x := c.get("Recognize/JSON/generated").ns / c.get("Recognize/JSON/json_Valid").ns
			return atMost(3.0, false, []float64{x}, factorRange)
		}},
	{"Allocations per generated `Recognize`, pooled parser in steady state",
		"0 on JSON, CSV and arithmetic (met today)",
		func(c *figureCalc) (string, state) {
			st := met
			var parts []string
			for _, w := range []string{"JSON", "CSV", "Arith_OperatorsRule"} {
				n := c.get("Recognize/" + w + "/generated").allocs
				if n != 0 {
					st = missed
				}
				parts = append(parts, fmt.Sprintf("%s %s", w, fmtCount(n)))
			}
			return strings.Join(parts, ", ") + " allocs/op", st
		}},
	{"Allocations per typed `ParseValue`",
		"Count grows no faster than the logarithm of the input (slab growth only)",
		notMeasured("each workload has one input size, so growth with the input is not measured")},
	{"Typed allocated bytes ÷ standard library, JSON and XML",
		"≤ 1.0× (with spans)",
		func(c *figureCalc) (string, state) {
			return atMost(1.0, false, c.stdRatios(true, bytesOf, "json", "xml"), factorRange)
		}},
	{"Generated TypeScript `parse` ÷ generated Go `Parse`",
		"≤ 2.0×",
		func(c *figureCalc) (string, state) {
			var xs []float64
			for _, w := range allWorkloads {
				if w != "Recovery" { // no TypeScript benchmark
					xs = append(xs, c.get("TS/Parse/"+w).ns/c.get("Parse/"+w+"/generated/codepoints").ns)
				}
			}
			return atMost(2.0, false, xs, factorRange)
		}},
	{"Generated Go streaming ÷ `encoding/csv` read loop (benchmark to be added)",
		"≤ 2.0× (stretch)",
		func(c *figureCalc) (string, state) {
			x := c.get("Stream/closure").ns / c.get("Stream/encoding_csv").ns
			return "not measured for generated Go; closure `ParseStream` is " + fmtSig(x, 3) + "×", open
		}},
	{"`Document` edit plus reparse at 100,000 lines, p99",
		"Same-length edits ≤ 1 ms; length-changing edits ≤ 2 ms (stretch)",
		notMeasured("the incremental benchmarks report means, not a p99 at 100,000 lines")},
	{"`Document` memory ÷ tree memory",
		"≤ 3×",
		notMeasured("no benchmark compares the retained memory of a `Document` with the tree")},
	{"Short-input latency (≤ 1 KB) ÷ the faster of pigeon and participle, same grammar and output class",
		"≤ 1.0×; ≤ 0.7× (stretch)",
		notMeasured("pigeon and participle are not in the harness")},
	{"Minimal generated package and binary-size delta",
		"Measured and published against pigeon, participle and ANTLR Go",
		notMeasured("sizes are measured for SyntaxIR only, not against pigeon, participle or ANTLR Go")},
	{"Size profile, DuckDB `parser.go`",
		"≥ 15% smaller than the speed profile",
		notMeasured("there is no size profile and no DuckDB grammar benchmark")},
	{"Cross-tool batch parsing (harness below), same output class",
		"Published, with losses reported",
		notMeasured("the cross-tool harness is not built")},
	{"Cross-tool incremental edit latency, same traces and output class",
		"Published against gotreesitter and gpeg",
		notMeasured("the cross-tool harness is not built")},
}

// renderScoreboard renders docs/scoreboard.md from the saved results.
func renderScoreboard(data, tsData []byte) ([]byte, error) {
	r, err := mergedResults(data, tsData)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, `# Performance Scoreboard

Each row of the target table in [the strategy](strategy.md#performance-leadership) against the saved results of
[the benchmarks](benchmarks.md), measured at commit `+"`%s`"+` on %s. A row is **met** or **not met** when the results
decide its Credibility target, and **open** when no benchmark measures it yet or the target is not a number; an open row
says what is missing. Where a target gives a stretch value, the standing is judged against the commitment.

This file is generated: `+"`go run ./bench/report -scoreboard`"+` rewrites it from `+"`bench/results*.txt`"+` without running
any benchmark, and a test checks that it matches the results and the strategy table. Do not edit it by hand.

| Measure (output class) | Credibility target | Measured | Standing |
|:--|:--|:--|:--|
`, r.meta["commit"], r.meta["date"])
	counts := map[state]int{}
	for _, e := range scoreboard {
		var missing []string
		c := &figureCalc{meta: r.meta, get: func(name string) *result {
			if x := r.bench[name]; x != nil {
				return x
			}
			missing = append(missing, name)
			return &result{metrics: map[string]float64{}}
		}}
		value, st := e.eval(c)
		if len(missing) > 0 {
			value, st = "not measured: the results lack "+missing[0], open
		}
		counts[st]++
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", e.measure, e.target, value, st)
	}
	fmt.Fprintf(&b, "\n%d met, %d not met, %d open.\n", counts[met], counts[missed], counts[open])
	return []byte(b.String()), nil
}

// writeScoreboard writes docs/scoreboard.md (or path) from the saved results in dir.
func writeScoreboard(root, dir, path string) error {
	if dir == "" {
		dir = filepath.Join(root, "bench")
	}
	data, tsData, _, err := loadResults(filepath.Join(dir, "results.txt"))
	if err != nil {
		return err
	}
	doc, err := renderScoreboard(data, tsData)
	if err != nil {
		return err
	}
	if path == "" {
		path = filepath.Join(root, "docs", "scoreboard.md")
	}
	if err := os.WriteFile(path, doc, 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	return nil
}
