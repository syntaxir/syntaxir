package generate_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// goLibraryText generates the Go library with -print-pipeline and the debugging settings d and
// returns its files and what the debugging settings wrote.
func goLibraryText(t *testing.T, d common.Debug) ([]common.GeneratedFile, string) {
	t.Helper()
	var out bytes.Buffer
	d.PrintPipeline, d.Output = true, &out
	files, err := generate.GoLibrary(common.GenOptions{Debug: d})
	if err != nil {
		t.Fatal(err)
	}
	return files, out.String()
}

// The pipeline text of the Go library has the headers sxir, settings and debug and starts at the
// library; it matches its golden file, reads back to an equal text, and a build from it gives the
// same files and writes the same text and statistics.
func TestGoLibraryTextGolden(t *testing.T) {
	files, out := goLibraryText(t, common.Debug{Stats: true})
	text := pipelineText(t, out)
	// The golden text is written without statistics.
	plain := strings.Replace(text, "stats=true", "stats=false", 1)
	golden := filepath.Join("testdata", "pipeline-go-lib.txt")
	if *update {
		if err := os.WriteFile(golden, []byte(plain), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got := plain; got != string(want) {
		t.Errorf("the pipeline text differs from %s (go test -update-pipelines rewrites it):\n%s", golden, got)
	}
	d, err := pipe.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if d.String() != text {
		t.Errorf("the text read and printed again differs:\n%s", d.String())
	}
	b, err := generate.Read(text)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Library || b.Backend != result.Go || b.Warning() != "" {
		t.Errorf("the text reads as %+v", b)
	}
	var again bytes.Buffer
	got, err := b.GoLibrary(driver.Options{}, &again)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, files) {
		t.Errorf("the build from the text gives other files")
	}
	if again.String() != out {
		t.Errorf("the build from the text writes other output:\n%.800s\n---\n%.800s", again.String(), out)
	}
}

// A text of the Go library takes no header of a program or a target and no grammar, and a text of
// a parser does not build the Go library.
func TestGoLibraryTextRejects(t *testing.T) {
	_, out := goLibraryText(t, common.Debug{})
	text := pipelineText(t, out)
	options := "options start= source-info=false default-value=false left-recursion-bindings=false\n"
	for _, e := range []struct{ name, old, new, want string }{
		{"a header of the program", "settings ", options + "settings ", "the text of the Go library has no header options"},
		{"a missing header", "settings profile= opt= no-opt=\n", "", "the header settings is missing"},
	} {
		edited := strings.Replace(text, e.old, e.new, 1)
		if edited == text {
			t.Fatalf("%s: the text has no %q", e.name, e.old)
		}
		if _, err := generate.Read(edited); err == nil || !strings.Contains(err.Error(), e.want) {
			t.Errorf("%s: %v, want an error with %q", e.name, err, e.want)
		}
	}
	b, err := generate.Read(text)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Generate(nil, driver.Options{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "builds the Go library, which takes no grammar") {
		t.Errorf("a library text generated with a grammar: %v", err)
	}
	c := pipelineCases[0]
	_, out = generateCase(t, c, common.Debug{PrintPipeline: true})
	if b, err = generate.Read(pipelineText(t, out)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.GoLibrary(driver.Options{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "builds a parser for the target go, not the Go library") {
		t.Errorf("a parser text built as the Go library: %v", err)
	}
}
