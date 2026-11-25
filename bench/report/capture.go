package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"syntaxir.dev/sxir/internal/bench/inputs"
	"syntaxir.dev/sxir/internal/bench/probe"
)

// tsWorkload is a workload measured on the generated TypeScript parser: the grammar and the input
// that the Go benchmarks of the same name use. The recovery workload is left out because a
// recovering parse reports an error, which the measuring script treats as a failed run.
type tsWorkload struct {
	name, grammar string
	input         func() string
}

var tsWorkloads = []tsWorkload{
	{"JSON", "testdata/grammars/json/json.sxir", func() string { return inputs.JSONInput(256 << 10) }},
	{"CSV", "testdata/grammars/csv/csv.sxir", func() string { return inputs.CSVInput(5000) }},
	{"XML", "testdata/grammars/xml/xml.sxir", func() string { return inputs.XMLInput(256 << 10) }},
	{"Arith_OperatorsRule", "examples/calculator/calc.sxir", func() string { return inputs.ArithInput(20000) }},
	{"Arith_LeftRec", "examples/calculator/calc_lr.sxir", func() string { return inputs.ArithInput(20000) }},
	{"Minilang", "examples/minilang/minilang.sxir", func() string { return inputs.MinilangInput(300, 0) }},
	{"Outline", "examples/outline/outline.sxir", func() string { return inputs.OutlineInput(5000) }},
}

// tsModes are the entry points of a generated TypeScript parser that are measured, by the name
// they have in the results.
var tsModes = []struct{ name, entry string }{
	{"Parse", "parse"},
	{"ParseValue", "parseValue"},
	{"Recognize", "recognize"},
}

// captureTS generates a TypeScript parser for every workload, measures each entry point in count
// rounds of fresh Node processes (bench/ts-parse.mjs) and returns the results with a header.
func captureTS(root, commit, work, node string, count int) ([]byte, error) {
	sxirBin, err := buildSXIR(root, work)
	if err != nil {
		return nil, err
	}
	version, err := execOut(root, nil, node, "--version")
	if err != nil {
		return nil, err
	}
	v8, err := execOut(root, nil, node, "-p", "process.versions.v8")
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	writeHeader(&b, commit)
	fmt.Fprintf(&b, "# node: %s\n# v8: %s\n# count: %d\n", strings.TrimSpace(version), strings.TrimSpace(v8), count)
	inputs := filepath.Join(work, "ts-inputs")
	if err := os.MkdirAll(inputs, 0o755); err != nil {
		return nil, err
	}
	for _, w := range tsWorkloads {
		if err := os.WriteFile(filepath.Join(inputs, w.name+".txt"), []byte(w.input()), 0o644); err != nil {
			return nil, err
		}
		args := []string{"gen", "-lang", "ts", "-types", "-recognize", "-g", filepath.Join(root, w.grammar), "-o", filepath.Join(work, "ts", w.name)}
		if _, err := execOut(root, nil, sxirBin, args...); err != nil {
			return nil, err
		}
	}
	// Alternate the runs of the rounds so that drift of the machine affects every workload alike.
	script := filepath.Join(root, "bench", "ts-parse.mjs")
	for round := 0; round < count; round++ {
		for _, w := range tsWorkloads {
			for _, m := range tsModes {
				line, err := execOut(root, nil, node, "--experimental-strip-types", script,
					"--module", filepath.Join(work, "ts", w.name, "parser.ts"),
					"--input", filepath.Join(inputs, w.name+".txt"),
					"--name", m.name+"/"+w.name, "--entry", m.entry)
				if err != nil {
					return nil, fmt.Errorf("%s %s: %w", m.name, w.name, err)
				}
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "BenchmarkTSParse/") {
					return nil, fmt.Errorf("%s %s: unexpected output %q", m.name, w.name, line)
				}
				fmt.Fprintf(os.Stderr, "%s\n", line)
				fmt.Fprintf(&b, "BenchmarkTS/%s\n", strings.TrimPrefix(line, "BenchmarkTSParse/"))
			}
		}
	}
	return b.Bytes(), nil
}

// sizeParsers are the grammars whose generated size is recorded, with the options of
// testdata/grammars/generate.txt (the options of the ready-made parsers).
var sizeParsers = probe.Grammars

// genOptions returns the options of testdata/grammars/generate.txt for grammar name.
func genOptions(root, name string) ([]string, error) { return probe.GenOptions(root, name) }

// parserGenArgs returns the options with which testdata/grammars/generate.txt generates parser name,
// adapted to TypeScript.
func parserGenArgs(root, name string) ([]string, error) { return probe.GenTSArgs(root, name) }

// captureSizes records the sizes of probe.WriteSizes under the header of the capture.
func captureSizes(root, commit, work string) ([]byte, error) {
	sxirBin, err := buildSXIR(root, work)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	writeHeader(&b, commit)
	fmt.Fprintln(&b, "# go binary: a main that calls ParseValue; go build -trimpath -ldflags=\"-s -w\"")
	if err := probe.WriteSizes(&b, root, sxirBin, work); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// sizeOfFile returns the size of a file and of its gzip -9 compression.
func sizeOfFile(path string) (raw, gz int, err error) { return probe.SizeOfFile(path) }

// buildSXIR builds the sxir command of the repository into the work directory.
func buildSXIR(root, work string) (string, error) { return probe.BuildSXIR(root, work) }

// execOut runs a command in dir, with extra environment variables, and returns its standard output.
func execOut(dir string, env []string, name string, args ...string) (string, error) {
	return probe.ExecOut(dir, env, name, args...)
}
