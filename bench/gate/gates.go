package main

import "fmt"

// A gate is a named set of benchmarks that judges a change: it is run for the base, the candidate and a second
// run of the base, in rounds, and the change is judged against the spread of the two runs of the base.
type gate struct {
	Name string
	// Pkg is the package whose test binary holds the benchmarks (a directory of the repository).
	Pkg string
	// Bench is the -bench pattern; it selects the benchmarks of the gate.
	Bench string
	// Time is the -benchtime of one measurement: short, because the power of a gate comes from the number of
	// rounds and not from the length of one.
	Time string
	// Doc says what a change to the gate measures.
	Doc string
}

// level builds a -bench pattern that matches one name per level, as `go test` splits it at the slashes.
func level(parts ...string) string {
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += "/"
		}
		s += "^(" + p + ")$"
	}
	return s
}

// gates lists every gate, in the order of the output. The first four are the quick set that runs by default.
var gates = []gate{
	{"generation", "internal/driver/generate", level("BenchmarkGeneration"), "100ms",
		"generating the parsers of testdata/grammars: the pass pipeline, the pass manager, the printers"},
	{"runtime-compile", "internal/driver/checked", level("BenchmarkRuntimeBackendCompile"), "100ms",
		"what a grammar compiled at run time costs the backends on first use"},
	{"parse", "bench", level("BenchmarkParse", "[A-Za-z_]+", "closure|generated", "codepoints"), "100ms",
		"batch parsing on the closure and generated Go backends, code-point positions, every workload"},
	{"recognize", "bench", level("BenchmarkRecognize", "[A-Za-z_]+", "closure|generated", "codepoints"), "100ms",
		"recognition on the closure and generated Go backends, code-point positions, every workload"},
	{"incremental", "bench", level("BenchmarkIncremental|BenchmarkIncrementalLong"), "100ms",
		"edit and reparse of a Document on every backend"},
	{"stream", "bench", level("BenchmarkStream"), "100ms",
		"reading and discarding a CSV stream on every backend"},
	{"prepare", "bench", level("BenchmarkPrepare"), "100ms",
		"compiling from source and loading a compiled grammar"},
	{"budget", "bench", level("BenchmarkControlledParse|BenchmarkBudgetControlModes"), "100ms",
		"parsing with limit controls and a cancelable context"},
}

// quick is the set that runs when no -gate is given.
var quick = []string{"generation", "runtime-compile", "parse", "recognize"}

func findGate(name string) (gate, error) {
	for _, g := range gates {
		if g.Name == name {
			return g, nil
		}
	}
	return gate{}, fmt.Errorf("no gate named %q (see -list)", name)
}
