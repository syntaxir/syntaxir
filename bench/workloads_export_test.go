package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/bench/inputs"
	"syntaxir.dev/sxir/internal/bench/probe"
	"syntaxir.dev/sxir/internal/optcatalog"
	"syntaxir.dev/sxir/internal/pass"
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
	// Settings are the per-setting variants (design record 080, "Re-Measurement"): each registered
	// flag set against each profile, as the settings part of bench/metrics measures them. A
	// measurement window times every variant whose Go is true on the parse rows of the matrix, against
	// the profile's own parser.
	Settings []settingSpec `json:"settings"`
}

type settingSpec struct {
	Profile string `json:"profile"`
	Flag    string `json:"flag"`
	// Value is 0 where the profile turns the flag on, and otherwise the value that turns it on.
	Value int `json:"value"`
	// State is the flag's state in the catalog: adopted, opt-in or experiment.
	State string `json:"state"`
	// Args are the options of sxir gen, added after Gen and Extra of a row, that build the variant;
	// the profile's own parser is built with only the -optimize option of Args.
	Args []string `json:"args"`
	// Go says whether the flag reaches the generated Go parsers and keeps their output, so that a
	// timing of them measures it.
	Go bool `json:"go"`
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
		in, err := probe.MatrixInputs(root, g)
		if err != nil {
			return nil, err
		}
		for _, f := range in {
			rel := "matrix/" + g + "/" + f.Name
			if err := write(rel, f.Text); err != nil {
				return nil, err
			}
			r.Inputs = append(r.Inputs, rel)
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
	variants, err := probe.SettingVariants()
	if err != nil {
		return nil, err
	}
	for _, v := range variants {
		state, _, err := optcatalog.StateOf(v.Flag)
		if err != nil {
			return nil, err
		}
		m.Settings = append(m.Settings, settingSpec{Profile: v.Profile, Flag: v.Flag.Name, Value: v.Value,
			State: string(state), Args: v.Args(), Go: v.Flag.Backends&pass.GeneratedGo != 0 && !v.Flag.ChangesOutput})
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
	timed := 0
	for _, v := range m.Settings {
		if v.Go {
			timed++
		}
		if len(v.Args) < 2 || v.Args[0] != "-optimize" || v.Args[1] != v.Profile {
			t.Errorf("setting %s of %s: args %v do not start with the profile", v.Flag, v.Profile, v.Args)
		}
	}
	if timed == 0 {
		t.Errorf("no setting of the %d is timed on the Go parsers", len(m.Settings))
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version": 1`) {
		t.Errorf("manifest.json has no version 1:\n%.200s", data)
	}
}
