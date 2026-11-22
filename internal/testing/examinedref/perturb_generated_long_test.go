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
// checks the observed parse by perturbation (examinedref.Perturb, and examinedref.PerturbAt for the reparse
// of a Document after an edit), and the stream route by its high-water mark, with the checks of
// examinedref, which the test copies into the consumer's module (examinedProbeFiles), and prints one JSON
// report.
const perturbProbeMain = `package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"perturbprobe/examinedref"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirinstrument "syntaxir.dev/sxir/pkg/runtime/instrument"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
IMPORTS)

var _ *sxirvalue.Node

// entry is a route of a generated package: run parses a string, stream a reader, and document makes a
// Document, which the routes document and edit parse.
type entry struct {
	name     string
	run      func(string, ...sxirruntime.ParseOption) error
	stream   func(io.Reader, ...sxirruntime.ParseOption) error
	document func(string, ...sxirruntime.ParseOption) (sxirruntime.Document, error)
}

var entries = [][]entry{
ENTRIES}

type job struct {
	Inputs [][]byte // bytes: JSON would replace invalid UTF-8 in a string
	Alpha  string
	Dep    examinedref.Dependence
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
	Panics        []string
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
						// run parses s, or, on the edit route, edits a Document of in to s from the change point on
						// and parses it again, as TestEnginePerturbation's edit route does. A panic of the parser
						// is reported, and the parse counts as refused.
						run := func(change int, s string) (tr []examinedref.Event, ran bool) {
							defer func() {
								if x := recover(); x != nil {
									out.Panics = append(out.Panics, fmt.Sprintf("change point %d, %q: %v", change, s, x))
									tr, ran = nil, false
								}
							}()
							trace := sxirruntime.WithTrace(func(ev sxirinstrument.TraceEvent) { tr = append(tr, eventOf(ev)) })
							switch {
							case e.stream != nil:
								_ = e.stream(examinedref.NewCountingReader(s), sxirruntime.WithUnit(unit), trace)
							case e.name == "edit":
								d, err := e.document(in, sxirruntime.WithUnit(unit))
								if err != nil {
									return nil, false
								}
								_, _ = d.Parse()
								rest := strings.Join(examinedref.Units(s, bytes)[change:], "")
								if d.Edit(change, len(examinedref.Units(in, bytes)), rest) != nil {
									return nil, false
								}
								_, _ = d.Parse(trace)
							case e.document != nil:
								if d, err := e.document(s, sxirruntime.WithUnit(unit)); err == nil {
									_, _ = d.Parse(trace)
								}
							default:
								_ = e.run(s, sxirruntime.WithUnit(unit), trace)
							}
							return tr, true
						}
						each := func(p examinedref.Perturbation) {
							out.Perturbations = append(out.Perturbations, perturbation{int(p.Variant), p.Change, []byte(p.Input)})
						}
						var r examinedref.Report
						if e.name == "edit" {
							r = examinedref.PerturbAt(run, in, bytes, jobs[i].Dep, jobs[i].Alpha, each)
						} else {
							r = examinedref.Perturb(func(s string) []examinedref.Event { tr, _ := run(0, s); return tr }, in, bytes, jobs[i].Dep, jobs[i].Alpha, each)
						}
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
	Dep    examinedref.Dependence
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
	Panics        []string
}

// TestGeneratedPerturbationAndHighWater checks the examined ends of generated Go parsers, built with
// observability, the typed library, the recognizer, Documents where the grammar has them, and ParseStream
// where the start rule has a #stream repetition: by perturbation on the value, parse, recognize, stream,
// document and edit routes (the reparse of a Document after an edit), as TestEnginePerturbation
// checks the engine's, and by the high-water mark of the stream route, as TestEngineStreamHighWater does.
func TestGeneratedPerturbationAndHighWater(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, func(t *testing.T) {
		cases := generatedCases(t)
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
				reportPerturb(t, prefix+" (panic)", r.Panics, 3)
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
		for _, entry := range []string{"value", "parse", "recognize", "stream", "document", "edit"} {
			r := totals[entry]
			if r == nil {
				t.Errorf("%s: no input perturbed", entry)
				continue
			}
			t.Logf("%s: %d inputs, %d perturbations (%d dropped, %d refused): %d identical, %d diverged in a call (%d exits checked, %d exempt, %d growing), %d diverged outside calls",
				entry, r.Parses, r.Perturbed, r.Dropped, r.Refused, r.Identical, r.Diverged, r.Checked, r.Exempt, r.Growing, r.Outside)
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
	// The library of a mutant of TestEngineExaminedMutants comes through codegentest.GoLibPatchEnv.
	write("go.mod", []byte(codegentest.GoModAt("perturbprobe", codegentest.PatchedLib(t))))
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
		o := common.GenOptions{Start: "main", Types: true, Recognize: true, Observability: true, Stream: true, Document: true}
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
		fmt.Fprintf(&entries, `		{name: "document", document: func(in string, o ...sxirruntime.ParseOption) (sxirruntime.Document, error) { return %[1]s.NewDocument(in, o...) }},
		{name: "edit", document: func(in string, o ...sxirruntime.ParseOption) (sxirruntime.Document, error) { return %[1]s.NewDocument(in, o...) }},
`, pkg)
		entries.WriteString("\t},\n")
		cycles, err := examinedref.LeftCycles(g, false)
		if err != nil {
			t.Fatalf("%s: rule facts: %v", c.Name, err)
		}
		job := perturbProbeJob{Alpha: examinedref.GrammarAlphabet(g),
			Dep: examinedref.Dependence{Variables: examinedref.ReadsVariables(g), Cycles: cycles}}
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
