//go:build sxir_long

package examinedref_test

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
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/examinedref"
)

// The generated parsers of these tests compile their grammars with the engine's compiler, as the tests of
// the generator do.
func init() {
	enginetest.UseEngine()
}

// perturbProbeMain is the consumer of the generated packages. For each input, unit and route of each case it
// checks the observed parse by perturbation, and the stream route by its high-water mark, with the checks of
// examinedref, which the test copies into the consumer's module (examinedProbeFiles), and prints one JSON
// report.
const perturbProbeMain = `package main

import (
	"encoding/json"
	"io"
	"os"

	"perturbprobe/examinedref"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirinstrument "syntaxir.dev/sxir/pkg/runtime/instrument"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
IMPORTS)

var _ *sxirvalue.Node

type entry struct {
	name   string
	run    func(string, ...sxirruntime.ParseOption) error
	stream func(io.Reader, ...sxirruntime.ParseOption) error
}

var entries = [][]entry{
ENTRIES}

type job struct {
	Inputs [][]byte // bytes: JSON would replace invalid UTF-8 in a string
	Alpha  string
	Vars   bool
}

type perturbation struct {
	Variant, Change int
	Input           []byte
}

type report struct {
	Case, Input   int
	Bytes         bool
	Entry         string
	Report        *examinedref.Report
	Perturbations []perturbation
	Streamed, EOF bool
	HighWater     []string
}

func eventOf(ev sxirinstrument.TraceEvent) examinedref.Event {
	k := examinedref.EventSkipped
	switch ev.Kind {
	case sxirinstrument.TraceEnter:
		k = examinedref.EventEnter
	case sxirinstrument.TraceExit:
		k = examinedref.EventExit
	}
	return examinedref.Event{Kind: k, Rule: ev.Rule, Level: ev.Level, Depth: ev.Depth, Pos: ev.Pos, Lookahead: ev.Lookahead,
		Matched: ev.Matched, End: ev.End, Memo: ev.Memo, Evals: ev.Evals, ReusedElements: ev.ReusedElements,
		Examined: ev.Examined, SkipReason: string(ev.SkipReason)}
}

func main() {
	var jobs []job
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(data, &jobs); err != nil {
		panic(err)
	}
	enc := json.NewEncoder(os.Stdout)
	for i := range entries {
		for j, raw := range jobs[i].Inputs {
			in := string(raw)
			for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
				bytes := unit == sxirinput.Bytes
				for _, e := range entries[i] {
					out := report{Case: i, Input: j, Bytes: bytes, Entry: e.name}
					if len(examinedref.Units(in, bytes)) <= MAXUNITS {
						run := func(s string) []examinedref.Event {
							var tr []examinedref.Event
							opts := []sxirruntime.ParseOption{sxirruntime.WithUnit(unit), sxirruntime.WithTrace(func(ev sxirinstrument.TraceEvent) {
								tr = append(tr, eventOf(ev))
							})}
							if e.stream != nil {
								_ = e.stream(examinedref.NewCountingReader(s), opts...)
							} else {
								_ = e.run(s, opts...)
							}
							return tr
						}
						r := examinedref.Perturb(run, in, bytes, jobs[i].Vars, jobs[i].Alpha, func(p examinedref.Perturbation) {
							out.Perturbations = append(out.Perturbations, perturbation{int(p.Variant), p.Change, []byte(p.Input)})
						})
						out.Report = &r
					}
					if e.stream != nil {
						r := examinedref.NewCountingReader(in)
						var tr []examinedref.Event
						var reads []examinedref.Read
						_ = e.stream(r, sxirruntime.WithUnit(unit), sxirruntime.WithTrace(func(ev sxirinstrument.TraceEvent) {
							tr = append(tr, eventOf(ev))
							reads = append(reads, examinedref.Read{Delivered: r.Delivered, EOF: r.EOF})
						}))
						out.Streamed, out.EOF = true, r.EOF
						out.HighWater = examinedref.CheckHighWater(in, bytes, tr, reads, examinedref.Read{Delivered: r.Delivered, EOF: r.EOF})
					}
					if err := enc.Encode(out); err != nil {
						panic(err)
					}
				}
			}
		}
	}
}
`

// examinedProbeFiles are the files of examinedref that the consumer compiles: the checks, which import the
// standard library only.
var examinedProbeFiles = []string{"perturb.go", "highwater.go"}

type perturbProbeJob struct {
	Inputs [][]byte
	Alpha  string
	Vars   bool
}

type perturbProbeReport struct {
	Case, Input   int
	Bytes         bool
	Entry         string
	Report        *examinedref.Report
	Perturbations []struct {
		Variant, Change int
		Input           []byte
	}
	Streamed, EOF bool
	HighWater     []string
}

// TestGeneratedPerturbationAndHighWater checks the examined ends of generated Go parsers, built with
// observability, the typed library, the recognizer and ParseStream where the start rule has a #stream
// repetition: by perturbation on the value, parse, recognize and stream routes, as TestEnginePerturbation
// checks the engine's, and by the high-water mark of the stream route, as TestEngineStreamHighWater does.
func TestGeneratedPerturbationAndHighWater(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, func(t *testing.T) {
		cases := perturbCases(t)
		reports := runPerturbProbes(t, cases)
		totals := map[string]*examinedref.Report{}
		streamed, ended := 0, 0
		for _, r := range reports {
			c := cases[r.Case]
			in := c.Inputs[r.Input]
			unit := "code-points"
			if r.Bytes {
				unit = "bytes"
			}
			prefix := fmt.Sprintf("%s %s: %s", r.Entry, unit, c.Name)
			if r.Report != nil {
				if totals[r.Entry] == nil {
					totals[r.Entry] = &examinedref.Report{}
				}
				totals[r.Entry].Add(*r.Report)
				reportPerturb(t, prefix, r.Report.Violations, 5)
				for _, p := range r.Perturbations {
					if err := validPerturbation(in, r.Bytes, examinedref.Perturbation{Variant: examinedref.Variant(p.Variant), Change: p.Change, Input: string(p.Input)}); err != nil {
						t.Errorf("%s (perturbation): %v", prefix, err)
					}
				}
			}
			if r.Streamed {
				streamed++
				if r.EOF {
					ended++
				}
				reportPerturb(t, fmt.Sprintf("%s high water %q", prefix, in), r.HighWater, 5)
			}
		}
		for _, entry := range []string{"value", "parse", "recognize", "stream"} {
			r := totals[entry]
			if r == nil {
				t.Errorf("%s: no input perturbed", entry)
				continue
			}
			t.Logf("%s: %d inputs, %d perturbations (%d dropped): %d identical, %d diverged in a call (%d exits checked, %d exempt), %d diverged outside calls",
				entry, r.Parses, r.Perturbed, r.Dropped, r.Identical, r.Diverged, r.Checked, r.Exempt, r.Outside)
			if r.Diverged == 0 || r.Identical == 0 || r.Checked == 0 {
				t.Errorf("%s: the perturbations never diverged or never kept the trace", entry)
			}
		}
		t.Logf("stream: %d parses, %d read to the end of the input", streamed, ended)
		if streamed == 0 || ended == 0 || ended == streamed {
			t.Errorf("stream: %d parses, %d of which read to the end", streamed, ended)
		}
	})
}

// runPerturbProbes generates the cases as the packages f0..fN of a module with the consumer and the copied
// checks, runs it and returns its reports.
func runPerturbProbes(t *testing.T, cases []codegentest.Case) []perturbProbeReport {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, content []byte) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(codegentest.GoMod("perturbprobe")))
	for _, f := range examinedProbeFiles {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		write(filepath.Join("examinedref", f), src)
	}
	var imports, entries strings.Builder
	var jobs []perturbProbeJob
	for i, c := range cases {
		g, err := compiletest.Parse(c.Src) // marks every rule @start, as the generator tests do
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		o := common.GenOptions{Start: "main", Types: true, Recognize: true, Observability: true, Stream: true}
		if _, err := codegentest.GenerateCode(t, g, o); err != nil {
			if !strings.Contains(err.Error(), "needs a #stream repetition") {
				t.Fatalf("%s: %v", c.Name, err)
			}
			o.Stream = false
		}
		pkg := fmt.Sprintf("f%d", i)
		codegentest.GeneratePackageIn(t, dir, "perturbprobe", pkg, g, o)
		fmt.Fprintf(&imports, "\t%s \"perturbprobe/%s\"\n", pkg, pkg)
		fmt.Fprintf(&entries, `	{
		{name: "value", run: func(in string, o ...sxirruntime.ParseOption) error { _, err := %[1]s.ParseValue(in, o...); return err }},
		{name: "parse", run: func(in string, o ...sxirruntime.ParseOption) error { _, err := %[1]s.Parse(in, o...); return err }},
		{name: "recognize", run: func(in string, o ...sxirruntime.ParseOption) error { return %[1]s.Recognize(in, o...) }},
`, pkg)
		if o.Stream {
			fmt.Fprintf(&entries, `		{name: "stream", stream: func(r io.Reader, o ...sxirruntime.ParseOption) error {
			return %s.ParseStream(r, func(*sxirvalue.Node) error { return nil }, o...)
		}},
`, pkg)
		}
		entries.WriteString("\t},\n")
		job := perturbProbeJob{Alpha: examinedref.GrammarAlphabet(g), Vars: examinedref.ReadsVariables(g)}
		for _, s := range c.Inputs {
			job.Inputs = append(job.Inputs, []byte(s))
		}
		jobs = append(jobs, job)
	}
	write("main.go", []byte(strings.NewReplacer("IMPORTS", imports.String(), "ENTRIES", entries.String(),
		"MAXUNITS", fmt.Sprint(perturbMaxUnits)).Replace(perturbProbeMain)))
	data, err := json.Marshal(jobs)
	if err != nil {
		t.Fatal(err)
	}
	write("jobs.json", data)
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".", "jobs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, stderr.String())
	}
	var reports []perturbProbeReport
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var r perturbProbeReport
		if err := dec.Decode(&r); err != nil {
			t.Fatalf("go run: malformed report: %v", err)
		}
		reports = append(reports, r)
	}
	if len(reports) == 0 {
		t.Fatal("go run: no report")
	}
	return reports
}
