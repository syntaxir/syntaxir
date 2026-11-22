package generate_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

var update = flag.Bool("update-pipelines", false, "rewrite the golden pipeline texts")

const (
	// pipelineGrammar is a grammar of two rules.
	pipelineGrammar = "@start\nrule A = \"a\" B\nrule B = \"b\"\n"
	// replanGrammar is a grammar whose TypeScript parser leaves out the call of B after a lookahead
	// that always fails, so print-ts asks for a second run with the reach of its printed code.
	replanGrammar = "@start\nrule main = !\"\"* B / \"x\"\nrule B = \"b\"\n"
)

// pipelineCase is a generation whose pipeline text the tests print: its grammar (nil for the
// TypeScript library alone) and its options.
type pipelineCase struct {
	name string
	src  string
	ts   bool
	o    common.GenOptions
}

var pipelineCases = []pipelineCase{
	{"go", pipelineGrammar, false, common.GenOptions{Module: "example.com/p", Lib: common.LibImport}},
	{"ts", pipelineGrammar, true, common.GenOptions{Lib: common.LibImport, LibModule: "./lib.ts", Types: true}},
	{"ts-lib", "", true, common.GenOptions{Lib: common.LibOnly}},
	{"ts-replan", replanGrammar, true, common.GenOptions{Lib: common.LibImport, LibModule: "./lib.ts"}},
}

func (c pipelineCase) grammar(t *testing.T) *ast.Grammar {
	t.Helper()
	if c.src == "" {
		return nil
	}
	g, err := compiletest.Parse(c.src)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// generateCase generates the case c with the debugging settings d, writing to a buffer, and returns
// the files and what the debugging settings wrote.
func generateCase(t *testing.T, c pipelineCase, d common.Debug) ([]common.GeneratedFile, string) {
	t.Helper()
	var out bytes.Buffer
	o := c.o
	o.Debug, o.Debug.Output = d, &out
	var files []common.GeneratedFile
	var err error
	if c.ts {
		files, err = generate.TypeScript(c.grammar(t), o)
	} else {
		files, err = generate.Go(c.grammar(t), o)
	}
	if err != nil {
		t.Fatalf("%s: %v", c.name, err)
	}
	return files, out.String()
}

// pipelineText returns the pipeline text at the start of the debugging output out.
func pipelineText(t *testing.T, out string) string {
	t.Helper()
	i := strings.Index(out, "\n}\n")
	if !strings.HasPrefix(out, "sxir-pipeline ") || i < 0 {
		t.Fatalf("the output does not start with a pipeline text:\n%.300s", out)
	}
	return out[:i+3]
}

// TestPipelineTextGolden checks the pipeline texts that -print-pipeline writes for Go, TypeScript and
// the TypeScript library alone against their golden files, which list every step from the program
// to the output, and that parsing a text and printing it gives the same text.
func TestPipelineTextGolden(t *testing.T) {
	for _, c := range pipelineCases[:3] {
		_, out := generateCase(t, c, common.Debug{PrintPipeline: true})
		text := pipelineText(t, out)
		if text != out {
			t.Errorf("%s: -print-pipeline alone wrote more than the text:\n%s", c.name, out[len(text):])
		}
		golden := filepath.Join("testdata", "pipeline-"+c.name+".txt")
		if *update {
			if err := os.WriteFile(golden, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if text != string(want) {
			t.Errorf("%s: the pipeline text differs from %s (go test -update-pipelines rewrites it):\n%s", c.name, golden, text)
		}
		d, err := pipe.Parse(text)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if d.String() != text {
			t.Errorf("%s: the text read and printed again differs:\n%s", c.name, d.String())
		}
	}
}

// TestPipelineTextRepeatsTheBuild checks that a build from its pipeline text gives the same files and
// writes the same dumps, statistics and text, the fallback of planning of the TypeScript printer
// included, and that a text of this version reads without a warning.
func TestPipelineTextRepeatsTheBuild(t *testing.T) {
	for _, c := range pipelineCases {
		d := common.Debug{PrintPipeline: true, PrintAfter: []string{"all"}, Stats: true}
		if c.src == "" {
			d.PrintAfter = nil
		}
		files, out := generateCase(t, c, d)
		b, err := generate.Read(pipelineText(t, out))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if w := b.Warning(); w != "" {
			t.Errorf("%s: %s", c.name, w)
		}
		var again bytes.Buffer
		got, err := b.Generate(c.grammar(t), driver.Options{}, &again)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(got, [][]common.GeneratedFile{files}) {
			t.Errorf("%s: the build from the text gives other files", c.name)
		}
		if again.String() != out {
			t.Errorf("%s: the build from the text writes other output:\n%.800s\n---\n%.800s", c.name, again.String(), out)
		}
	}
}

// TestPipelineTextEditsOptionalPasses checks that a text can remove, repeat and reorder optional
// passes, which the statistics of the build show, and that a text and edits of the assembled
// pipeline are not given together.
func TestPipelineTextEditsOptionalPasses(t *testing.T) {
	c := pipelineCases[0]
	g := c.grammar(t)
	files, out := generateCase(t, c, common.Debug{PrintPipeline: true, Stats: true})
	text := pipelineText(t, out)
	const passes = "            guards\n            scans\n"
	if !strings.Contains(text, passes) {
		t.Fatalf("the text has no Core passes %q:\n%s", passes, text)
	}
	for _, e := range []struct {
		name, body string
		has, lacks []string
	}{
		{"removed", "            scans\n", []string{"  scans "}, []string{"  guards "}},
		{"repeated and reordered", "            scans\n            guards\n            scans#2\n", []string{"  scans ", "  guards ", "  scans#2 "}, nil},
	} {
		b, err := generate.Read(strings.Replace(text, passes, e.body, 1))
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		var stats bytes.Buffer
		got, err := b.Generate(g, driver.Options{}, &stats)
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		if !reflect.DeepEqual(got, [][]common.GeneratedFile{files}) {
			t.Errorf("%s: the Core passes change the files", e.name)
		}
		s := stats.String()[len(text):]
		for _, w := range e.has {
			if !strings.Contains(s, w) {
				t.Errorf("%s: the statistics lack %q:\n%s", e.name, w, s)
			}
		}
		for _, w := range e.lacks {
			if strings.Contains(s, w) {
				t.Errorf("%s: the statistics have %q:\n%s", e.name, w, s)
			}
		}
	}
	b, err := generate.Read(text)
	if err != nil {
		t.Fatal(err)
	}
	x := driver.Options{Edits: []pipe.Edit{pipe.Fill(driver.CoreSlot, "", pipe.Seq(corepass.IR))}}
	if _, err := b.Generate(g, x, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "a pipeline text or edits of the assembled pipeline, not both") {
		t.Errorf("a text with edits: %v", err)
	}
}

// TestPipelineTextRejects checks that a text that changes the required skeleton of the build, or
// whose headers do not give the options of a generation, is an error that says what is wrong.
func TestPipelineTextRejects(t *testing.T) {
	c := pipelineCases[0]
	g := c.grammar(t)
	_, out := generateCase(t, c, common.Debug{PrintPipeline: true})
	text := pipelineText(t, out)
	for _, e := range []struct {
		name, old, new, want string
		read                 bool // the error is one of Read
	}{
		{"a required step dropped", "            flow-finish!\n", "", "the required steps of the text differ from those of the assembled pipeline", false},
		{"another target", "with target=go {", "with target=ts {", "feature target has no text form: the text gives target=ts where the assembled pipeline gives target=go", false},
		{"an unknown pass", "            scans\n", "            scams\n", "no pass scams", false},
		{"a missing header", "settings profile= opt= no-opt=\n", "", "the header settings is missing", true},
		{"a header twice", "settings profile= opt= no-opt=\n", "settings profile= opt= no-opt=\nsettings profile= opt= no-opt=\n", "the header settings is given twice", true},
		{"a target of no generation", "target go ", "target vm ", `"vm" is no target of a generation`, true},
		{"an unknown key", "settings profile= ", "settings profle= ", "header settings: unknown key profle", true},
	} {
		edited := strings.Replace(text, e.old, e.new, 1)
		if edited == text {
			t.Fatalf("%s: the text has no %q", e.name, e.old)
		}
		b, err := generate.Read(edited)
		if err == nil && !e.read {
			_, err = b.Generate(g, driver.Options{}, &bytes.Buffer{})
		} else if err == nil || !e.read {
			t.Errorf("%s: Read: %v", e.name, err)
			continue
		}
		if err == nil || !strings.Contains(err.Error(), e.want) {
			t.Errorf("%s: %v, want an error with %q", e.name, err, e.want)
		}
	}
}

// severalTargets returns the Go and the TypeScript targets of the first two pipeline cases, with the
// debugging settings d.
func severalTargets(d common.Debug) []driver.Target {
	goT, tsT := pipelineCases[0].o, pipelineCases[1].o
	goT.Debug, tsT.Debug = d, d
	return []driver.Target{{Backend: result.Go, Gen: goT}, {Backend: result.TypeScript, Gen: tsT}}
}

// A generation of several targets gives each target the files it gets alone, prints a text with a
// target line per target that matches its golden file, and a build from that text repeats the
// files, the dumps and the statistics.
func TestPipelineTextOfSeveralTargets(t *testing.T) {
	c := pipelineCases[0]
	g := c.grammar(t)
	var out bytes.Buffer
	d := common.Debug{PrintPipeline: true, PrintAfter: []string{"all"}, Stats: true, Output: &out}
	files, err := generate.Targets(g, severalTargets(d)...)
	if err != nil {
		t.Fatal(err)
	}
	for i, alone := range pipelineCases[:2] {
		want, _ := generateCase(t, alone, common.Debug{})
		if !reflect.DeepEqual(files[i], want) {
			t.Errorf("%s: the files of the target differ from those it gets alone", alone.name)
		}
	}
	text := pipelineText(t, out.String())
	if strings.Count(text, "\ntarget ") != 2 {
		t.Errorf("the text has no target line per target:\n%s", text)
	}
	plain := strings.NewReplacer("print-after=all", "print-after=", "stats=true", "stats=false").Replace(text)
	golden := filepath.Join("testdata", "pipeline-go-ts.txt")
	if *update {
		if err := os.WriteFile(golden, []byte(plain), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if plain != string(want) {
		t.Errorf("the pipeline text differs from %s (go test -update-pipelines rewrites it):\n%s", golden, plain)
	}
	b, err := generate.Read(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Targets) != 2 || b.Targets[0].Backend != result.Go || b.Targets[1].Backend != result.TypeScript {
		t.Fatalf("the text reads as the targets %+v", b.Targets)
	}
	var again bytes.Buffer
	got, err := b.Generate(g, driver.Options{}, &again)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, files) {
		t.Errorf("the build from the text gives other files")
	}
	if again.String() != out.String() {
		t.Errorf("the build from the text writes other output:\n%.800s\n---\n%.800s", again.String(), out.String())
	}
}

// The targets of one generation share the options of the call, and generate the library alone all
// or none; the library alone of several targets is the library of each alone.
func TestTargetsRejectMixedOptions(t *testing.T) {
	g := pipelineCases[0].grammar(t)
	ts := severalTargets(common.Debug{})
	ts[1].Gen.SourceInfo = true
	if _, err := generate.Targets(g, ts...); err == nil || !strings.Contains(err.Error(), "target ts: SourceInfo differs from the value of the compilation") {
		t.Errorf("targets of different program options: %v", err)
	}
	ts = severalTargets(common.Debug{})
	ts[1].Gen.Lib = common.LibOnly
	if _, err := generate.Targets(g, ts...); err == nil || !strings.Contains(err.Error(), "for every target of a generation or for none") {
		t.Errorf("a library alone beside a parser: %v", err)
	}
	libs, err := generate.Targets(nil, driver.Target{Backend: result.Go, Gen: common.GenOptions{Module: "example.com/p", Lib: common.LibOnly}},
		driver.Target{Backend: result.TypeScript, Gen: common.GenOptions{Lib: common.LibOnly}})
	if err != nil {
		t.Fatal(err)
	}
	goLib, err := generate.Go(nil, common.GenOptions{Module: "example.com/p", Lib: common.LibOnly})
	if err != nil {
		t.Fatal(err)
	}
	tsLib, _ := generateCase(t, pipelineCases[2], common.Debug{})
	if !reflect.DeepEqual(libs, [][]common.GeneratedFile{goLib, tsLib}) {
		t.Errorf("the libraries alone of two targets differ from each generated alone")
	}
}
