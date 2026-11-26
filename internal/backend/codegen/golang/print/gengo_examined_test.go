package print_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/examinedref"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// The examined end of a call in an observed parse is exact: every unit of input that can change the
// call's result lies below it, and nothing beyond what the call or a call it used looked at. The tests
// here compare the calls that the generated parsers report on every route an observed parse takes, the
// typed parse (ParseValue), the Node parse (Parse), the recognizer (Recognize) and the stream parse
// (ParseStream), which reads its input one byte at a time, with the independent reference
// (examinedref) of the same input and unit: every exit must be a call of the reference with the same
// match, end and examined end, every skipped call must fail in the reference, and the start call must
// return what the reference's does.

// examinedProbeMain is the consumer of the generated packages: for each input, unit and entry it prints
// one JSON report with the exits of the observed parse and its error.
const examinedProbeMain = `package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	sxirinstrument "syntaxir.dev/sxir/pkg/runtime/instrument"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
IMPORTS)

var _ *sxirvalue.Node

type entry struct {
	name string
	run  func(string, ...sxirruntime.ParseOption) error
}

var entries = [][]entry{
ENTRIES}

// oneByte returns one byte per Read, so that a stream parse loads its input as late as it can.
type oneByte struct{ s string }

func (r *oneByte) Read(b []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	b[0], r.s = r.s[0], r.s[1:]
	return 1, nil
}

func main() {
	var inputs [][][]byte // bytes: JSON would replace invalid UTF-8 in a string
	data, _ := os.ReadFile(os.Args[1])
	if err := json.Unmarshal(data, &inputs); err != nil {
		panic(err)
	}
	for i := range entries {
		for j, in := range inputs[i] {
			for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
				for _, e := range entries[i] {
					var events []sxirinstrument.TraceEvent
					err := e.run(string(in), sxirruntime.WithUnit(unit), sxirruntime.WithTrace(func(ev sxirinstrument.TraceEvent) {
						if ev.Kind != sxirinstrument.TraceEnter {
							events = append(events, ev)
						}
					}))
					var one *sxirvalue.SyntaxError
					var errs sxirvalue.SyntaxErrors
					var truncated *sxirvalue.TruncatedSyntaxErrors
					b, _ := json.Marshal(struct {
						Case, Input int
						Bytes       bool
						Entry       string
						Events      []sxirinstrument.TraceEvent
						Err         string
						Aborted     bool
					}{i, j, unit == sxirinput.Bytes, e.name, events, fmt.Sprint(err),
						err != nil && !errors.As(err, &one) && !errors.As(err, &errs) && !errors.As(err, &truncated)})
					fmt.Println(string(b))
				}
			}
		}
	}
}
`

// examinedReport is one report of the consumer: the exit and skipped events of an observed parse, its
// error, and whether the error ended the parse before the start call returned (Aborted), as a runtime
// error of a Value Action does.
type examinedReport struct {
	Case, Input int
	Bytes       bool
	Entry       string
	Events      []instrument.TraceEvent
	Err         string
	Aborted     bool
}

// examinedCases are the cases of the exactness tests: the representative cases of the generated corpus,
// the shapes of the observation tests, examinedExtras and the stream grammars.
func examinedCases(t *testing.T) []codegentest.Case {
	t.Helper()
	cases := append(codegentest.Representative(codegentest.GenCorpus(t)), observeShapes...)
	cases = append(cases, examinedExtras()...)
	for _, c := range codegentest.ExaminedShapes() {
		// Generated parsers do not insert tokens, and refuse the grammars that would.
		if !strings.Contains(c.Src, "#missing") {
			cases = append(cases, c)
		}
	}
	for _, c := range streamCases(t) {
		if !strings.Contains(c.Src, "#stream") || len(c.Inputs) == 0 {
			continue
		}
		dup := false
		for _, d := range cases {
			dup = dup || d.Src == c.Src
		}
		if !dup {
			cases = append(cases, c)
		}
	}
	return cases
}

// runExaminedProbes builds the cases with observability, the typed library and the recognizer, and with
// ParseStream where the start rule has a #stream repetition, as the packages f0..fN of a module that
// imports the library from lib (codegentest.LibCopy or a copy with a mutant). mutate, if not nil, changes
// the code of every package. It returns the reports of the consumer.
func runExaminedProbes(t *testing.T, cases []codegentest.Case, lib string, mutate func([]byte) []byte) []examinedReport {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, content []byte) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(codegentest.GoModAt("examinedprobe", lib)))
	var imports, entries strings.Builder
	var inputs [][][]byte
	for i, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		o := common.GenOptions{Start: "main", Types: true, Recognize: true, Observability: true, Stream: true}
		// The package keeps its types in a package of their own: merged into one file, the types of some
		// grammars would collide with the parser's names.
		if _, err := codegentest.GenerateCode(t, g, o); err != nil {
			if !strings.Contains(err.Error(), "needs a #stream repetition") {
				t.Fatalf("%s: %v", c.Name, err)
			}
			o.Stream = false
		}
		pkg := fmt.Sprintf("f%d", i)
		codegentest.GeneratePackageIn(t, dir, "examinedprobe", pkg, g, o)
		if mutate != nil {
			filepath.WalkDir(filepath.Join(dir, pkg), func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
					return err
				}
				code, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				return os.WriteFile(path, mutate(code), 0o644)
			})
		}
		fmt.Fprintf(&imports, "\tf%d \"examinedprobe/f%d\"\n", i, i)
		fmt.Fprintf(&entries, `	{
		{"value", func(in string, o ...sxirruntime.ParseOption) error { _, err := f%[1]d.ParseValue(in, o...); return err }},
		{"parse", func(in string, o ...sxirruntime.ParseOption) error { _, err := f%[1]d.Parse(in, o...); return err }},
		{"recognize", func(in string, o ...sxirruntime.ParseOption) error { return f%[1]d.Recognize(in, o...) }},
`, i)
		if o.Stream {
			fmt.Fprintf(&entries, `		{"stream", func(in string, o ...sxirruntime.ParseOption) error {
			return f%d.ParseStream(&oneByte{in}, func(*sxirvalue.Node) error { return nil }, o...)
		}},
`, i)
		}
		entries.WriteString("\t},\n")
		var in [][]byte
		for _, s := range c.Inputs {
			in = append(in, []byte(s))
		}
		inputs = append(inputs, in)
	}
	write("main.go", []byte(strings.NewReplacer("IMPORTS", imports.String(), "ENTRIES", entries.String()).Replace(examinedProbeMain)))
	data, _ := json.Marshal(inputs)
	write("inputs.json", data)
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".", "inputs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, stderr.String())
	}
	var reports []examinedReport
	for _, l := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		var r examinedReport
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("go run: malformed report %q: %v", l, err)
		}
		reports = append(reports, r)
	}
	return reports
}

// examinedMismatches checks the reports against the reference (see the comment at the top of the
// file), and returns the number of calls compared and the differences, both by entry.
func examinedMismatches(t *testing.T, cases []codegentest.Case, reports []examinedReport) (compared map[string]int, diffs map[string][]string) {
	t.Helper()
	compared, diffs = map[string]int{}, map[string][]string{}
	type key struct {
		c, j   int
		bytes  bool
		stream bool
	}
	type refRun struct {
		res   examinedref.Result
		exits []examinedref.Exit
		err   error
	}
	refs := map[key]refRun{}
	grammars := map[int]*ast.Grammar{}
	for _, r := range reports {
		c := cases[r.Case]
		in := c.Inputs[r.Input]
		// A stream parse builds no value of the start rule at its top call.
		stream := r.Entry == "stream"
		k := key{r.Case, r.Input, r.Bytes, stream}
		ref, ok := refs[k]
		if !ok {
			g := grammars[r.Case]
			if g == nil {
				var err error
				if g, err = parseTest(c.Src); err != nil {
					t.Fatalf("%s: %v", c.Name, err)
				}
				grammars[r.Case] = g
			}
			o := examinedref.Options{Unit: input.CodePoints, NoStartValue: stream}
			if r.Bytes {
				o.Unit = input.Bytes
			}
			ref.res, ref.exits, ref.err = examinedref.EvaluateWith(g, "main", in, o)
			refs[k] = ref
		}
		if ref.err != nil {
			t.Fatalf("%s %q: reference: %v", c.Name, in, ref.err)
		}
		var calls []examinedref.Observed
		for _, e := range r.Events {
			if o, ok := examinedref.ObservedOf(e); ok {
				calls = append(calls, o)
			}
		}
		d := examinedref.CompareParse(ref.res, ref.exits, "main", calls, r.Aborted, entryActions(r.Entry))
		compared[r.Entry] += len(calls)
		for _, msg := range d {
			if r.Aborted {
				msg += fmt.Sprintf(" (aborted by %s)", r.Err)
			}
			diffs[r.Entry] = append(diffs[r.Entry], fmt.Sprintf("%s %q bytes=%v %s: %s", c.Name, in, r.Bytes, r.Entry, msg))
		}
	}
	return compared, diffs
}

// TestObservedExaminedEndsAreExact checks the examined ends of the observed parses of every route against
// their references, on examinedCases.
func TestObservedExaminedEndsAreExact(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	codegentest.EachMode(t, func(t *testing.T) {
		cases := examinedCases(t)
		compared, diffs := examinedMismatches(t, cases, runExaminedProbes(t, cases, codegentest.LibCopy(), nil))
		t.Logf("exits compared by entry: %v", compared)
		for _, entry := range []string{"value", "parse", "recognize", "stream"} {
			if compared[entry] == 0 {
				t.Errorf("no exit of %s compared", entry)
			}
			for i, msg := range diffs[entry] {
				if i == 10 {
					t.Errorf("%d more differences of %s", len(diffs[entry])-i, entry)
					break
				}
				t.Error(msg)
			}
		}
	})
}

// examinedExtras are shapes that reach records the library replays only on particular inputs:
// a lookahead at the start of a member called where its leader grows, whose outcome a context keeps, a
// recovery that ends its rule, whose skip walks the skip records serve, a lookahead whose outcome is
// the same for two seeds of a growth but examines a different extent, reduced from random cycle grammar
// 304, and a growth whose last step, which does not grow, is taken from the context, reduced from
// random cycle grammar 103, where the leader r4 takes every growth step from the context at depth 4
// and evaluates nothing, so every producer reports that call with Memo=true and Evals=0 and its
// Document counts it as a reuse, and one reduced from random cycle grammar 210, which needs the record
// of a growth step taken from the context: without it, recognizing "ca" reports r2 at 0 with examined
// end 0, below its match end 2, where the reference examines to 3. Its r2 at 0 also takes every growth
// step from the context. Those two take a growth from the context only in a recognizer; random cycle
// grammar 2136 with cuts takes one in a Document too, which counts it as a reuse.
func examinedExtras() []codegentest.Case {
	unit := strings.Repeat("a", 40) + "c" + strings.Repeat("a", 9) + "e" + strings.Repeat("a", 20) + "d"
	return []codegentest.Case{
		{Name: "lookahead of a member at a growth start", Src: `rule main = e $$
rule e = e "+" m / m
rule m = &(e "x" t) e "x" / t
rule t = [0-9]+`, Inputs: []string{"1x2", "1x2+3", "1+2x34", "1x23x4", "1x2x3", "1+2+3x45"}},
		{Name: "recovery that ends its rule", Src: `rule main = (x "d" / [ace])* $$
rule x = "b" #recover(skip=[ac]+)`, Inputs: []string{strings.Repeat(unit, 6), "b" + strings.Repeat("a", 40) + "cd", strings.Repeat("b"+unit, 4)}},
		{Name: "growth steps of a leader taken again from the context", Src: `rule main = a $$
rule a = b "1" / b "2" / "a"
rule b = a "x" / b "y" / "b"`, Inputs: []string{"b1", "by2", "ax2", "byy1x2", "b1x1y2", "ax1yx2", "byyyy1x2", "byyyyyy2x1"}},
		{Name: "lookahead outcomes of a growth that differ only in their examined ends", Src: `rule main = r0 $$
rule r0 = r2
rule r1 = (r2 (r1 "b" "c" / r0) / &"b" r0) / "a"
rule r2 = r0 "b" / !r1? / "a"`, Inputs: []string{"a", "aac", "acb", "aaac", "acabca", "acbccc"}},
		{Name: "last growth step taken from the context", Src: `rule main = r0
rule r0 = r3 / r1 / "a"
rule r1 = !("c" / r4)* / r0
rule r3 = r0 r3 / r4
rule r4 = &"c" (r1 / r4?)`, Inputs: []string{"a", "c", "cc", "ca", "b", "ac", "cbbcbcaa"}},
		{Name: "only record of an examined end in a growth step taken from the context", Src: `rule main = r0
rule r0 = r4 / r0 r2
rule r2 = &r4 r0
rule r3 = r2 r0 / "a"
rule r4 = r3 / !r0 / !r4 r2 / "c"`, Inputs: []string{"ca"}},
		{Name: "growth taken from the context in a Document", Src: `rule main = r0 $$
rule r0 = !"a" / r1 !r1 r1 / "a"
rule r1 = r2 &"c" / (r3 "a" / "b" r0) / !"c" / "b"
rule r2 = !r2 r1? / ("a" &"c" / r3 !#cut) / "a"
rule r3 = !"a" #cut "b" / r0 / "b"`, Inputs: []string{"a"}},
	}
}

// entryActions tells which Value Actions an entry of the probe runs: recognition runs only those
// that predicates read, and the other entries build values and run them all.
func entryActions(entry string) examinedref.Actions {
	if entry == "recognize" {
		return examinedref.ReadActions
	}
	return examinedref.EveryAction
}
