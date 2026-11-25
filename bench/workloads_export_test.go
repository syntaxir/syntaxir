package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/bench/inputs"
	"syntaxir.dev/sxir/internal/bench/probe"
)

// The timed measurements outside this repository (the harness of github.com/syntaxir/bench) drive
// checkouts of it through its commands and this package only. TestExportWorkloads is their
// description of the workloads: with SXIR_BENCH_EXPORT=DIR it writes DIR/manifest.json and the
// inputs it names, so that the harness measures the inputs and generation options that this
// package's benchmarks use, without importing the packages that make them.

// workloadManifestVersion is the version of the manifest's format; the harness rejects others.
const workloadManifestVersion = 1

// workloadManifest describes the workloads of the timed measurements. Paths of grammars are
// relative to the root of the checkout, paths of inputs relative to the manifest's directory.
type workloadManifest struct {
	Version int `json:"version"`
	// TS are the workloads measured on the generated TypeScript parsers, with the inputs of the Go
	// benchmarks of the same name.
	TS []tsWorkloadSpec `json:"ts"`
	// Matrix are the rows of the language-grammar matrix: a grammar of testdata/grammars generated
	// with the options of testdata/grammars/generate.txt (Gen), with Extra added, and measured by
	// Program on its inputs.
	Matrix []matrixRowSpec `json:"matrix"`
}

type tsWorkloadSpec struct {
	Name    string `json:"name"`
	Grammar string `json:"grammar"`
	Input   string `json:"input"`
}

type matrixRowSpec struct {
	Name    string   `json:"name"`
	Grammar string   `json:"grammar"`
	Gen     []string `json:"gen"`
	Extra   []string `json:"extra,omitempty"`
	// Program is the measuring program: parse (ParseValue on every input), incremental (one-digit
	// edits of record 25000 of a long CSV file with the generated Document) or stream (ParseStream
	// of a long CSV file).
	Program string   `json:"program"`
	Inputs  []string `json:"inputs"`
}

// tsWorkloads are the workloads of the TypeScript measurements. The recovery workload is left out
// because a recovering parse reports an error, which the measuring script treats as a failed run.
var tsWorkloads = []struct {
	name, grammar string
	input         func() string
}{
	{"JSON", "testdata/grammars/json/json.sxir", func() string { return inputs.JSONInput(256 << 10) }},
	{"CSV", "testdata/grammars/csv/csv.sxir", func() string { return inputs.CSVInput(5000) }},
	{"XML", "testdata/grammars/xml/xml.sxir", func() string { return inputs.XMLInput(256 << 10) }},
	{"Arith_OperatorsRule", "examples/calculator/calc.sxir", func() string { return inputs.ArithInput(20000) }},
	{"Arith_LeftRec", "examples/calculator/calc_lr.sxir", func() string { return inputs.ArithInput(20000) }},
	{"Minilang", "examples/minilang/minilang.sxir", func() string { return inputs.MinilangInput(300, 0) }},
	{"Outline", "examples/outline/outline.sxir", func() string { return inputs.OutlineInput(5000) }},
}

// matrixScaled are the matrix grammars that also get a generated input of about 256 KiB.
var matrixScaled = map[string]func() string{
	"json": func() string { return inputs.JSONInput(256 << 10) },
	"csv":  func() string { return inputs.CSVInput(5000) },
	"xml":  func() string { return inputs.XMLInput(256 << 10) },
}

// exportWorkloads writes the manifest and its inputs into dir, with root the root of the checkout.
func exportWorkloads(root, dir string) (*workloadManifest, error) {
	m := &workloadManifest{Version: workloadManifestVersion}
	write := func(rel, data string) error {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(data), 0o644)
	}
	for _, w := range tsWorkloads {
		rel := "ts/" + w.name + ".txt"
		if err := write(rel, w.input()); err != nil {
			return nil, err
		}
		m.TS = append(m.TS, tsWorkloadSpec{Name: w.name, Grammar: w.grammar, Input: rel})
	}
	for _, g := range probe.Grammars {
		gen, err := probe.GenOptions(root, g)
		if err != nil {
			return nil, err
		}
		r := matrixRowSpec{Name: g, Grammar: "testdata/grammars/" + g + "/" + g + ".sxir", Gen: gen, Program: "parse"}
		txt, err := filepath.Glob(filepath.Join(root, "testdata", "grammars", g, "*.txt"))
		if err != nil {
			return nil, err
		}
		for _, f := range txt {
			data, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			rel := "matrix/" + g + "/" + filepath.Base(f)
			if err := write(rel, string(data)); err != nil {
				return nil, err
			}
			r.Inputs = append(r.Inputs, rel)
		}
		if scaled, ok := matrixScaled[g]; ok {
			rel := "matrix/" + g + "/scaled.in"
			if err := write(rel, scaled()); err != nil {
				return nil, err
			}
			r.Inputs = append(r.Inputs, rel)
		}
		if len(r.Inputs) == 0 {
			return nil, fmt.Errorf("matrix row %s has no inputs", g)
		}
		m.Matrix = append(m.Matrix, r)
	}
	// The CSV parser generated with the flag of the generated Document and of ParseStream, measured
	// on one long file: the incremental program edits record 25000, the stream program expects 50001
	// records.
	gen, err := probe.GenOptions(root, "csv")
	if err != nil {
		return nil, err
	}
	long := "matrix/csv-long/long.csv"
	if err := write(long, inputs.CSVInput(50000)); err != nil {
		return nil, err
	}
	for _, r := range []struct{ name, flag, program string }{
		{"csv-incremental", "-document", "incremental"},
		{"csv-stream", "-stream", "stream"},
	} {
		m.Matrix = append(m.Matrix, matrixRowSpec{Name: r.name, Grammar: "testdata/grammars/csv/csv.sxir",
			Gen: slices.Clone(gen), Extra: []string{r.flag}, Program: r.program, Inputs: []string{long}})
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return m, write("manifest.json", string(data)+"\n")
}

// TestExportWorkloads writes the workload manifest and its inputs to the directory named by
// SXIR_BENCH_EXPORT. Without the variable it writes them to a temporary directory and checks that
// every grammar and input the manifest names exists.
func TestExportWorkloads(t *testing.T) {
	dir := os.Getenv("SXIR_BENCH_EXPORT")
	if dir == "" {
		dir = t.TempDir()
	}
	m, err := exportWorkloads("..", dir)
	if err != nil {
		t.Fatal(err)
	}
	var grammars, files []string
	for _, w := range m.TS {
		grammars, files = append(grammars, w.Grammar), append(files, w.Input)
	}
	for _, r := range m.Matrix {
		grammars, files = append(grammars, r.Grammar), append(files, r.Inputs...)
		if !slices.Contains([]string{"parse", "incremental", "stream"}, r.Program) {
			t.Errorf("%s: unknown program %q", r.Name, r.Program)
		}
	}
	for _, g := range grammars {
		if _, err := os.Stat(filepath.Join("..", filepath.FromSlash(g))); err != nil {
			t.Errorf("grammar: %v", err)
		}
	}
	for _, f := range files {
		if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil || fi.Size() == 0 {
			t.Errorf("input %s: %v", f, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version": 1`) {
		t.Errorf("manifest.json has no version 1:\n%.200s", data)
	}
}
