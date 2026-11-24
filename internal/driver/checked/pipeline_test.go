package checked

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/closure"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/parser"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

var update = flag.Bool("update-pipelines", false, "rewrite the golden pipeline texts")

// testOnlyOptions are the options of the check that only tests set and no header writes: a program
// without sites, say, cannot be reproduced from the text of its pipeline.
var testOnlyOptions = map[string]bool{"NoRunSites": true, "NoScanSites": true}

// TestProgramHeaderCoversTheCheckOptions is the reflection test of the options of the check over
// the options header of a generation and the program header: the program header holds its fields to
// ProgramLine, and each exported field of check.Options is written by exactly one of the two, and
// the program header writes no field but the role that is not one of them.
func TestProgramHeaderCoversTheCheckOptions(t *testing.T) {
	if err := ProgramHeader.Check(); err != nil {
		t.Fatal(err)
	}
	written := map[string][]string{}
	for _, w := range generate.Header.Words {
		if w.Word == "options" {
			for _, f := range w.Fields {
				written[f.Field] = append(written[f.Field], "options")
			}
		}
	}
	for _, f := range ProgramHeader.Words[0].Fields {
		written[f.Field] = append(written[f.Field], "program")
	}
	for _, f := range reflect.VisibleFields(reflect.TypeFor[check.Options]()) {
		if testOnlyOptions[f.Name] {
			continue
		}
		if f.IsExported() && len(written[f.Name]) != 1 {
			t.Errorf("the check option %s is written by %v, want one header", f.Name, written[f.Name])
		}
		delete(written, f.Name)
	}
	delete(written, "Start") // the start rule of a generation, which no run-time program has
	delete(written, "Role")
	if len(written) > 0 {
		t.Errorf("the headers write %v, which are no options of the check", written)
	}
}

// runTimeGrammar returns the grammar of the run-time texts, whose parser can insert missing tokens,
// so that it has an insertion program, with every rule a start rule.
func runTimeGrammar(t *testing.T) *ast.Grammar {
	t.Helper()
	g, err := parser.Parse(statetest.InsertionCycle)
	if err != nil {
		t.Fatal(err)
	}
	return starttest.MarkAll(g)
}

// TestRunTimePipelineTextGolden checks the pipeline texts of the run-time backends, each alone and
// both in one build, against their golden files, which list every step from the program to the
// output, and that parsing a text and printing it gives the same text.
func TestRunTimePipelineTextGolden(t *testing.T) {
	s, err := Check(runTimeGrammar(t), Options{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		backends []result.Backend
	}{
		{"vm", []result.Backend{result.VM}},
		{"closure", []result.Backend{result.Closure}},
		{"vm-closure", []result.Backend{result.VM, result.Closure}},
	} {
		text, err := Describe(s, common.Debug{}, c.backends...)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		golden := filepath.Join("testdata", "pipeline-"+c.name+".txt")
		if *update {
			if err := os.MkdirAll("testdata", 0o755); err != nil {
				t.Fatal(err)
			}
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

// TestRunTimePipelineTextRepeatsTheBuild checks, for each program of a grammar, that the build from
// its pipeline text compiles the same program under the same settings, and gives the bytecode and
// the Core module of the closures that the program builds itself. The parser's options and the
// settings are not the defaults, so a header that dropped them would build another program. The
// insertion program has no bytecode, since the VM inserts no missing tokens, so it builds for the
// closures alone.
func TestRunTimePipelineTextRepeatsTheBuild(t *testing.T) {
	settings, err := pass.Settings{}.Without(corepass.PassGuards)
	if err != nil {
		t.Fatal(err)
	}
	parserOpts := check.Options{SourceInfo: true, DisableMemo: true}
	plain, err := parser.Parse("@start\nrule main = (item \",\")* item $$\nrule item = [a-z]+ / \"(\" main \")\"\n")
	if err != nil {
		t.Fatal(err)
	}
	both := []result.Backend{result.VM, result.Closure}
	for _, c := range []struct {
		g        *ast.Grammar
		role     driver.Role
		backends []result.Backend
	}{
		{plain, driver.Parser, both},
		{plain, driver.Recognizer, both},
		{runTimeGrammar(t), driver.Insertion, both},
		{runTimeGrammar(t), driver.InsertionRecognizer, both},
	} {
		s, err := Check(c.g, Options{Compile: parserOpts, Settings: settings}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		src := s
		switch c.role {
		case driver.Recognizer:
			d, err := s.Recognizer()
			if err != nil {
				t.Fatal(err)
			}
			src = d.(*Source)
		case driver.Insertion, driver.InsertionRecognizer:
			d, err := s.Insertion()
			if err != nil {
				t.Fatal(err)
			}
			src = d.(*Source)
			if c.role == driver.InsertionRecognizer {
				if d, err = src.Recognizer(); err != nil {
					t.Fatal(err)
				}
				src = d.(*Source)
			}
		}
		role := c.role
		text, err := Describe(src, common.Debug{}, c.backends...)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		b, err := Read(text)
		if err != nil {
			t.Fatalf("%s: %v\n%s", role, err, text)
		}
		if w := b.Warning(); w != "" {
			t.Errorf("%s: %s", role, w)
		}
		if b.Role != role || !reflect.DeepEqual(b.Parser, parserOpts) || !b.Settings.Equal(settings) || !slices.Equal(b.Backends, c.backends) {
			t.Errorf("%s: read the role %s, the parser's options %+v, the settings equal %v and the backends %v", role, b.Role,
				b.Parser, b.Settings.Equal(settings), b.Backends)
		}
		again, err := b.Source(c.g)
		if err != nil {
			t.Fatal(err)
		}
		if again.role != role || !reflect.DeepEqual(again.prog.Opts, src.prog.Opts) {
			t.Errorf("%s: the text builds the %s program with the options %+v, want %+v", role, again.role, again.prog.Opts, src.prog.Opts)
		}
		var out bytes.Buffer
		outs, err := b.Compile(c.g, driver.Options{}, &out)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		for i, out := range outs {
			switch out := out.(type) {
			case *vmlower.Bytecode:
				module, err := src.Module()
				if err != nil {
					t.Fatal(err)
				}
				if got, want := out.Module.Disassemble(), module.Disassemble(); got != want {
					t.Errorf("%s: the bytecode of the text is\n%s\nwant\n%s", role, got, want)
				}
			case *closure.Module:
				core, err := src.Core(nil)
				if err != nil {
					t.Fatal(err)
				}
				if got, want := ir.Print(out.Core, true), ir.Print(core.(*closure.Module).Core, true); got != want {
					t.Errorf("%s: the Core module of the text is\n%s\nwant\n%s", role, got, want)
				}
			default:
				t.Errorf("%s: output %d is %T", role, i, out)
			}
		}
		if len(outs) != len(c.backends) {
			t.Errorf("%s: %d outputs for %d backends", role, len(outs), len(c.backends))
		}
		if out.Len() != 0 {
			t.Errorf("%s: the build without debugging settings wrote %q", role, out.String())
		}
	}
}

// The readers of the two kinds of texts tell them apart: a generation text has no program line, and
// a run-time text has one, which the reader of a generation rejects, naming the reader that takes it.
func TestRunTimePipelineTextIsNoGeneration(t *testing.T) {
	s, err := Check(runTimeGrammar(t), Options{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	text, err := Describe(s, common.Debug{}, result.VM)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generate.Read(text); err == nil || !strings.Contains(err.Error(), "checked.Read") {
		t.Errorf("generate.Read of a run-time text: err = %v", err)
	}
	for _, c := range []struct{ name, from, to, want string }{
		{"unknown word", "\nprogram ", "\nprogrammed ", "a run-time build has no header programmed"},
		{"generation target", "target vm", "target go", `"go" is no target of a run-time build`},
		{"unknown role", "role=parser", "role=lexer", `"lexer" is no role (parser, recognizer, insertion, insertion-recognizer)`},
		{"unknown recognition", "recognition=build-values", "recognition=none", `"none" is no recognition`},
	} {
		if !strings.Contains(text, c.from) {
			t.Fatalf("%s: the text has no %q:\n%s", c.name, c.from, text)
		}
		if _, err := Read(strings.Replace(text, c.from, c.to, 1)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	i := strings.Index(text, "\nprogram ")
	j := i + 1 + strings.Index(text[i+1:], "\n")
	if _, err := Read(text[:i] + text[j:]); err == nil || !strings.Contains(err.Error(), "the header program is missing") {
		t.Errorf("no program line: err = %v", err)
	}
	if _, err := Describe(s, common.Debug{}, result.Go); err == nil {
		t.Error("Describe of a Go target: no error")
	}
}
