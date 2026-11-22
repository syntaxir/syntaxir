package print_test

import (
	"regexp"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestAPIReExportsNoLibraryName checks that the user API of a generated Go parser (api.go, and
// the Document of parser.go) names no declaration of the support library: the options, the
// Document, the Span and the other user-facing names are those of the runtime packages, so a user
// never meets a name that is for generated parsers.
func TestAPIReExportsNoLibraryName(t *testing.T) {
	t.Parallel()
	g, err := parseTest("type Pair = { A int; B int }\nrule main = item* #stream $$\nrule item = \"a\" => Pair{A: 1, B: 2}\n")
	if err != nil {
		t.Fatal(err)
	}
	libName := regexp.MustCompile(`\brt\.[A-Za-z_]+`)
	for _, c := range []struct {
		name string
		opts common.GenOptions
	}{
		{"plain", common.GenOptions{Module: "example.com/apis", Start: "main", Lib: common.LibImport}},
		{"document", common.GenOptions{Module: "example.com/apis", Start: "main", Lib: common.LibImport, Document: true}},
		{"typed document", common.GenOptions{Module: "example.com/apis", Start: "main", Lib: common.LibImport, Types: true, Document: true, Observability: true, Stream: true, Recognize: true}},
	} {
		files, err := codegentest.Generate(t, g, c.opts)
		if err != nil {
			t.Fatal(c.name, err)
		}
		found := false
		for _, f := range files {
			if f.Path != "api.go" {
				continue
			}
			found = true
			src := string(f.Content)
			if strings.Contains(src, "pkg/runtime/libgo") {
				t.Errorf("%s: api.go imports the library", c.name)
			}
			if m := libName.FindString(src); m != "" {
				t.Errorf("%s: api.go uses the library name %s", c.name, m)
			}
		}
		if !found {
			t.Errorf("%s: no api.go generated", c.name)
		}
	}
}
