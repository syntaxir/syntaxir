package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Workloads of the parsing benchmarks, in the order of the tables. Direct workloads build the
// values of ParseValue without the nodes of Parse; the others convert the tree of Parse.
var (
	allWorkloads    = []string{"JSON", "CSV", "XML", "Arith_OperatorsRule", "Arith_LeftRec", "Minilang", "Recovery", "Outline"}
	directWorkloads = []string{"JSON", "CSV", "XML", "Arith_OperatorsRule", "Arith_LeftRec", "Outline"}
)

// A figure is a number or a range that documents outside docs/benchmarks.md quote from the results,
// so that they say what the results of the latest run say. The guides mark a quoted figure
//
//	<!-- fig:NAME -->0.44–0.61×<!-- /fig -->
//
// and go run ./bench/report rewrites the text between the markers with the value of figure NAME;
// TestFiguresAreUpToDate fails when a marked text differs from the value, and when a marker names
// no figure.
type figure struct {
	name    string
	meaning string
	// ts marks a figure computed from the TypeScript results, which a run may not have.
	ts    bool
	value func(c *figureCalc) string
}

// figureCalc reads the results for the value of a figure.
type figureCalc struct {
	get func(name string) *result
}

// ratios is, for each workload, the figure of a over the figure of b: a and b are the names of
// the benchmarks of a workload w, given by w, and metric picks the measure compared.
func (c *figureCalc) ratios(workloads []string, a, b func(w string) string, metric func(*result) float64) []float64 {
	var xs []float64
	for _, w := range workloads {
		xs = append(xs, metric(c.get(a(w)))/metric(c.get(b(w))))
	}
	return xs
}

func nsOf(r *result) float64    { return r.ns }
func bytesOf(r *result) float64 { return r.bytesPerOp }

func benchName(format string) func(w string) string {
	return func(w string) string { return fmt.Sprintf(format, w) }
}

// factorRange formats the smallest and the largest ratio with two decimals and an ×: "0.44–0.61×",
// or "0.95×" when both give the same text.
func factorRange(xs []float64) string {
	lo, hi := slices.Min(xs), slices.Max(xs)
	return rangeOf(fmtFactor(lo), fmtFactor(hi)) + "×"
}

// sigRange formats the smallest and the largest ratio with two significant digits.
func sigRange(xs []float64) string {
	lo, hi := slices.Min(xs), slices.Max(xs)
	return rangeOf(fmtSig(lo, 2), fmtSig(hi, 2)) + "×"
}

// pairMs is "5.7 → 3.9 ms" for two benchmarks.
func (c *figureCalc) pairMs(a, b string) string {
	return fmt.Sprintf("%s → %s ms", fmtSig(c.get(a).ns/1e6, 2), fmtSig(c.get(b).ns/1e6, 2))
}

// figureCatalog is every figure that a document can quote; each is in the table of the figures in
// docs/benchmarks.md.
var figureCatalog = []figure{
	{name: "generated-vs-closure-codepoints", meaning: "Generated Go `Parse` time over closure time, in code points, across the eight workloads",
		value: func(c *figureCalc) string {
			return factorRange(c.ratios(allWorkloads, benchName("Parse/%s/generated/codepoints"), benchName("Parse/%s/closure/codepoints"), nsOf))
		}},
	{name: "generated-vs-closure-bytes", meaning: "The same in bytes",
		value: func(c *figureCalc) string {
			return factorRange(c.ratios(allWorkloads, benchName("Parse/%s/generated/bytes"), benchName("Parse/%s/closure/bytes"), nsOf))
		}},
	{name: "bytecode-vs-closure", meaning: "Recursive bytecode VM parse time over closure time, both position units together",
		value: func(c *figureCalc) string {
			return factorRange(append(
				c.ratios(allWorkloads, benchName("Parse/%s/bytecode/codepoints"), benchName("Parse/%s/closure/codepoints"), nsOf),
				c.ratios(allWorkloads, benchName("Parse/%s/bytecode/bytes"), benchName("Parse/%s/closure/bytes"), nsOf)...))
		}},
	{name: "iterative-vs-closure", meaning: "Iterative bytecode VM parse time over closure time, both position units together",
		value: func(c *figureCalc) string {
			return factorRange(append(
				c.ratios(allWorkloads, benchName("Parse/%s/iterative/codepoints"), benchName("Parse/%s/closure/codepoints"), nsOf),
				c.ratios(allWorkloads, benchName("Parse/%s/iterative/bytes"), benchName("Parse/%s/closure/bytes"), nsOf)...))
		}},
	{name: "typed-vs-parse-time", meaning: "`ParseValue` time over generated `Parse` time, the workloads whose values are built directly",
		value: func(c *figureCalc) string {
			return factorRange(c.ratios(directWorkloads, benchName("Parse/%s/generated_ast"), benchName("Parse/%s/generated/codepoints"), nsOf))
		}},
	{name: "typed-vs-parse-memory", meaning: "`ParseValue` bytes allocated over `Parse` bytes allocated, the workloads whose values are built directly",
		value: func(c *figureCalc) string {
			return factorRange(c.ratios(directWorkloads, benchName("Parse/%s/generated_ast"), benchName("Parse/%s/generated/codepoints"), bytesOf))
		}},
	{name: "converted-vs-parse-time", meaning: "`ParseValue` time over `Parse` time, the workloads that convert the tree",
		value: func(c *figureCalc) string {
			return factorRange(c.ratios(convertedWorkloads(), benchName("Parse/%s/generated_ast"), benchName("Parse/%s/generated/codepoints"), nsOf))
		}},
	{name: "converted-vs-parse-memory", meaning: "`ParseValue` bytes allocated over `Parse` bytes allocated, the workloads that convert the tree",
		value: func(c *figureCalc) string {
			return factorRange(c.ratios(convertedWorkloads(), benchName("Parse/%s/generated_ast"), benchName("Parse/%s/generated/codepoints"), bytesOf))
		}},
	{name: "json-typed-time", meaning: "JSON, generated Go `Parse` time then `ParseValue` time",
		value: func(c *figureCalc) string {
			return c.pairMs("Parse/JSON/generated/codepoints", "Parse/JSON/generated_ast")
		}},
	{name: "json-typed-memory", meaning: "JSON, generated Go `Parse` bytes allocated then `ParseValue` bytes allocated",
		value: func(c *figureCalc) string {
			return fmt.Sprintf("%s → %s MB", fmtSig(c.get("Parse/JSON/generated/codepoints").bytesPerOp/1e6, 2), fmtSig(c.get("Parse/JSON/generated_ast").bytesPerOp/1e6, 2))
		}},
	{name: "recognize-speedup-closure", meaning: "Closure `Parse` time over closure `Recognize` time",
		value: func(c *figureCalc) string {
			return sigRange(c.ratios(allWorkloads, benchName("Parse/%s/closure/codepoints"), benchName("Recognize/%s/closure"), nsOf))
		}},
	{name: "recognize-vs-parse-generated", meaning: "Generated Go `Recognize` time over generated `Parse` time, in code points",
		value: func(c *figureCalc) string {
			return factorRange(c.ratios(allWorkloads, benchName("Recognize/%s/generated"), benchName("Parse/%s/generated/codepoints"), nsOf))
		}},
	{name: "json-recognize-time", meaning: "JSON, generated Go `Parse` time then `Recognize` time",
		value: func(c *figureCalc) string {
			return c.pairMs("Parse/JSON/generated/codepoints", "Recognize/JSON/generated")
		}},
	{name: "csv-recognize-time", meaning: "CSV, generated Go `Parse` time then `Recognize` time",
		value: func(c *figureCalc) string {
			return c.pairMs("Parse/CSV/generated/codepoints", "Recognize/CSV/generated")
		}},
	{name: "operators-closure-time", meaning: "Arith_OperatorsRule, closure `Parse` time in code points",
		value: func(c *figureCalc) string { return fmtTime(c.get("Parse/Arith_OperatorsRule/closure/codepoints").ns) }},
	{name: "leftrec-closure-time", meaning: "Arith_LeftRec, closure `Parse` time in code points",
		value: func(c *figureCalc) string { return fmtTime(c.get("Parse/Arith_LeftRec/closure/codepoints").ns) }},
	{name: "leftrec-over-operators", meaning: "Arith_LeftRec closure time over Arith_OperatorsRule closure time",
		value: func(c *figureCalc) string {
			return fmtSig(c.get("Parse/Arith_LeftRec/closure/codepoints").ns/c.get("Parse/Arith_OperatorsRule/closure/codepoints").ns, 2)
		}},
	{name: "typescript-json-parse-time", meaning: "JSON, generated TypeScript `parse` time under Node, code points", ts: true,
		value: func(c *figureCalc) string { return fmtSig(c.get("TS/Parse/JSON").ns/1e6, 2) + " ms" }},
}

// convertedWorkloads are the workloads whose ParseValue converts the tree of Parse.
func convertedWorkloads() []string {
	var ws []string
	for _, w := range allWorkloads {
		if !slices.Contains(directWorkloads, w) {
			ws = append(ws, w)
		}
	}
	return ws
}

// figureRow is a row of the table of figures in docs/benchmarks.md.
type figureRow struct {
	Name, Meaning, Value string
}

// figureValues computes the figures of the catalog from the results. A figure that needs the
// TypeScript results is left out when there are none.
func figureValues(get func(string) *result, hasTS bool) (rows []figureRow, values map[string]string) {
	c := &figureCalc{get: get}
	values = map[string]string{}
	for _, f := range figureCatalog {
		if f.ts && !hasTS {
			continue
		}
		v := f.value(c)
		values[f.name] = v
		rows = append(rows, figureRow{f.name, f.meaning, v})
	}
	return rows, values
}

var figureMarker = regexp.MustCompile(`<!-- fig:([a-z0-9-]+) -->(.*?)<!-- /fig -->`)

// figureDocuments lists the documents that may quote figures: README.md and the Markdown files of
// docs/, except docs/benchmarks.md, which is generated whole.
func figureDocuments(root string) ([]string, error) {
	docs := []string{filepath.Join(root, "README.md")}
	err := filepath.WalkDir(filepath.Join(root, "docs"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") && path != filepath.Join(root, "docs", "benchmarks.md") {
			docs = append(docs, path)
		}
		return nil
	})
	return docs, err
}

// applyFigures sets the text of every figure marker in the documents to the value of its figure. It
// returns the documents whose markers were stale, and writes them when write is set. A marker
// that names no figure, or a figure that this run lacks, is an error.
func applyFigures(root string, values map[string]string, write bool) (stale []string, err error) {
	docs, err := figureDocuments(root)
	if err != nil {
		return nil, err
	}
	for _, path := range docs {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var bad []string
		out := figureMarker.ReplaceAllStringFunc(string(data), func(m string) string {
			sub := figureMarker.FindStringSubmatch(m)
			v, ok := values[sub[1]]
			if !ok {
				bad = append(bad, sub[1])
				return m
			}
			return "<!-- fig:" + sub[1] + " -->" + v + "<!-- /fig -->"
		})
		if len(bad) > 0 {
			rel, _ := filepath.Rel(root, path)
			return nil, fmt.Errorf("%s quotes figures that the results do not give: %s", rel, strings.Join(bad, ", "))
		}
		if out == string(data) {
			continue
		}
		rel, _ := filepath.Rel(root, path)
		stale = append(stale, rel)
		if write {
			if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
				return nil, err
			}
		}
	}
	return stale, nil
}
