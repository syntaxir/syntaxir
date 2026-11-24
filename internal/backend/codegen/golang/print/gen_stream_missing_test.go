package print_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// streamInsertionLimits are the missing-token limits of the generated stream comparisons: none,
// which runs the parser's tables, one and two, which some inputs reach, and one that none reaches.
var streamInsertionLimits = []int{0, 1, 2, 64}

// TestGeneratedStreamsInsertMissingTokens checks that a generated stream parse with a missing-token
// limit inserts what the engine's stream parse does, on the incremental insertion corpus, in both
// units, at several limits, observed and not: the same elements delivered, the same error and the
// same compute units, and trace events when observed.
// The stream rule repeats the start rule of each case, so that an element that inserts follows one
// that does not and the limit spans elements. With a library that gives each element a limit of its
// own, some parse must differ.
func TestGeneratedStreamsInsertMissingTokens(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := codegentest.IncrementalInsertionCorpus()
	if ms := generatedStreamInsertionMismatches(t, goBin, codegentest.LibCopy(), cases); len(ms) > 0 {
		for _, m := range ms {
			t.Error(m)
		}
	}
	// The mutant resets the attempts of the limit at each committed element.
	const commit = "func (p *Parser) streamCommit(pos int) {\n"
	perElement := codegentest.LibCopyWith(t, func(name, src string) string {
		if name != "stream_exec.go" {
			return src
		}
		if !strings.Contains(src, commit) {
			t.Fatalf("stream_exec.go does not declare %q", commit)
		}
		return strings.Replace(src, commit, commit+"\tif p.missing != nil {\n\t\tp.missing.attempts = 0\n\t}\n", 1)
	}, nil)
	if len(generatedStreamInsertionMismatches(t, goBin, perElement, cases)) == 0 {
		t.Error("a limit per element: every generated stream parse matched the engine's")
	}
}

// streamInsertionRun is one stream parse of the comparison: the input of a case, in bytes or code
// points, with a missing-token limit, observed (WithTrace) or not. The input is bytes, which JSON
// keeps when they are not UTF-8.
type streamInsertionRun struct {
	Case     int
	Input    []byte
	Bytes    bool
	Limit    int
	Observed bool
}

// streamInsertionResult is what a stream parse delivered: its elements as JSON, its error, its
// compute units and whether it traced events.
type streamInsertionResult struct {
	Emits  []json.RawMessage `json:",omitempty"`
	Err    string            `json:",omitempty"`
	Units  uint64
	Traced bool
}

// generatedStreamInsertionMismatches generates a stream parser for each case with the library copy
// lib, with observability, runs every input of every case at streamInsertionLimits, observed and
// not, and describes each result that differs from that of the engine.
func generatedStreamInsertionMismatches(t *testing.T, goBin, lib string, cases []codegentest.Case) []string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", codegentest.GoModAt("streammissing", lib))
	var imports, parsers strings.Builder
	var runs []streamInsertionRun
	var want, names []string
	for i, c := range cases {
		// The stream rule is the start rule of the generated parser, which some cases mark (@start).
		src := "@start(default)\nrule streamed = items:main* #stream\n" + c.Src
		g, err := parseTest(src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: fmt.Sprintf("g%d", i), Start: "streamed", Stream: true, SourceInfo: true, Observability: true})
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		write(fmt.Sprintf("g%d/parser.go", i), string(code))
		fmt.Fprintf(&imports, "\tg%d \"streammissing/g%d\"\n", i, i)
		fmt.Fprintf(&parsers, `	%[1]d: func(r io.Reader, emit func(any) error, bytes bool, limit int, units *uint64, opts ...sxirruntime.ParseOption) error {
		u := g%[1]d.CodePoints
		if bytes {
			u = g%[1]d.Bytes
		}
		opts = append(opts, g%[1]d.WithUnit(u), sxirruntime.WithMissingTokens(limit), sxirruntime.WithComputeReport(units))
		return g%[1]d.ParseStream(r, func(n *g%[1]d.Node) error { return emit(n) }, opts...)
	},
`, i)
		prog := mustCompile(t, src, check.Options{NoProjections: true})
		for _, in := range c.Inputs {
			for _, bytes := range []bool{false, true} {
				for _, limit := range streamInsertionLimits {
					for _, observed := range []bool{false, true} {
						v := streamInsertionRun{Case: i, Input: []byte(in), Bytes: bytes, Limit: limit, Observed: observed}
						runs = append(runs, v)
						want = append(want, engineStreamInsertion(prog, v))
						names = append(names, fmt.Sprintf("%s %q bytes=%t limit %d observed=%t", c.Name, in, bytes, limit, observed))
					}
				}
			}
		}
	}
	write("main.go", `package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
`+imports.String()+`)

var parsers = map[int]func(io.Reader, func(any) error, bool, int, *uint64, ...sxirruntime.ParseOption) error{
`+parsers.String()+`}

type run struct {
	Case     int
	Input    []byte
	Bytes    bool
	Limit    int
	Observed bool
}

type result struct {
	Emits  []json.RawMessage `+"`json:\",omitempty\"`"+`
	Err    string            `+"`json:\",omitempty\"`"+`
	Units  uint64
	Traced bool
}

func main() {
	var runs []run
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(data, &runs); err != nil {
		panic(err)
	}
	for _, v := range runs {
		var res result
		emit := func(n any) error {
			b, err := json.Marshal(n)
			if err != nil {
				panic(err)
			}
			res.Emits = append(res.Emits, b)
			return nil
		}
		var opts []sxirruntime.ParseOption
		if v.Observed {
			opts = append(opts, sxirruntime.WithTrace(func(instrument.TraceEvent) { res.Traced = true }))
		}
		if err := parsers[v.Case](strings.NewReader(string(v.Input)), emit, v.Bytes, v.Limit, &res.Units, opts...); err != nil {
			res.Err = err.Error()
		}
		b, _ := json.Marshal(res)
		fmt.Println(string(b))
	}
}
`)
	data, _ := json.Marshal(runs)
	write("runs.json", string(data))
	cmd := exec.Command(goBin, "run", ".", "runs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d\n%s", len(got), len(want), out)
	}
	var mismatches []string
	for k := range want {
		if got[k] != want[k] {
			mismatches = append(mismatches, fmt.Sprintf("%s\n generated %s\n engine    %s", names[k], got[k], want[k]))
		}
	}
	return mismatches
}

// engineStreamInsertion runs v with the engine's stream parse and formats the result as the
// generated test program does.
func engineStreamInsertion(prog *engine.Program, v streamInsertionRun) string {
	var res streamInsertionResult
	emit := func(n *value.Node) error {
		b, err := json.Marshal(n)
		if err != nil {
			panic(err)
		}
		res.Emits = append(res.Emits, b)
		return nil
	}
	o := engine.ParseOptions{Expectations: true, Unit: input.CodePoints, MissingTokens: v.Limit}
	if v.Bytes {
		o.Unit = input.Bytes
	}
	opts := []sxirruntime.ParseOption{sxirruntime.WithComputeReport(&res.Units)}
	if v.Observed {
		opts = append(opts, sxirruntime.WithTrace(func(instrument.TraceEvent) { res.Traced = true }))
	}
	engine.Apply(&o, opts)
	if err := prog.ParseStreamWith("streamed", strings.NewReader(string(v.Input)), emit, o); err != nil {
		res.Err = err.Error()
	}
	b, _ := json.Marshal(res)
	return string(b)
}
