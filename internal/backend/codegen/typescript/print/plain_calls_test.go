package print_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// plainCallFunc matches a function that a call site calls in place of invokePlain: its comment line
// and its text up to the closing brace at the start of a line.
var plainCallFunc = regexp.MustCompile(`(?ms)^// [^\n]*, called as by invokePlain\n.*?^}\n`)

// TestPlainCallsGolden keeps the text of the functions that the printer prints from the invokePlain
// template for a recursive and a non-recursive plain rule under each form of the parser: with and
// without limit controls, with observability, and with recognizers, whose calls make no value.
func TestPlainCallsGolden(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`@start
rule main = list
rule list = "(" item* ")"
rule item = list / word / ","
rule word = [a-z]+ ("-" [a-z]+)*
`)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for _, v := range []struct {
		name string
		opts common.GenOptions
	}{
		{"limits", common.GenOptions{}},
		{"no limits", common.GenOptions{NoLimitControls: true}},
		{"observability", common.GenOptions{Observability: true}},
		{"recognize", common.GenOptions{Recognize: true}},
	} {
		files, err := codegentest.GenerateTSRaw(g, v.opts)
		if err != nil {
			t.Fatalf("%s: %v", v.name, err)
		}
		var parser string
		for _, f := range files {
			if f.Path == "parser.ts" {
				parser = string(f.Content)
			}
		}
		fs := plainCallFunc.FindAllString(parser, -1)
		if len(fs) == 0 {
			t.Fatalf("%s: parser.ts has no plain call", v.name)
		}
		fmt.Fprintf(&out, "=== %s\n%s", v.name, strings.Join(fs, "\n"))
	}
	path := filepath.Join("testdata", "plain_calls.golden")
	if *codegentest.UpdateGoldens {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != out.String() {
		t.Errorf("the plain calls differ from %s (rerun with -update to accept):\n%s", path, out.String())
	}
}
