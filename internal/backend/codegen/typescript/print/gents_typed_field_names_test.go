package print_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestTypedFieldsNamedLikeTheLibrary checks that TypeScript keeps the names of struct fields that Go
// renames (codegentest.LibraryNamedFieldsCase): types.ts declares each field under its grammar name
// beside the span's start and end, and the typed values are the engine's.
func TestTypedFieldsNamedLikeTheLibrary(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript parsers")
	}
	c := codegentest.LibraryNamedFieldsCase()
	g, err := compiletest.Parse(c.Src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := codegentest.GenerateTS(g, common.GenOptions{Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	var typesTS string
	for _, f := range files {
		if f.Path == "types.ts" {
			typesTS = string(f.Content)
		}
	}
	for _, want := range []string{`readonly "Start": number | bigint;`, `readonly "End": number | bigint;`, `readonly "Range": number | bigint;`, `readonly "Span": number | bigint;`, `readonly "TName": string;`, "export interface Thing extends Span {"} {
		if !strings.Contains(typesTS, want) {
			t.Errorf("types.ts has no %q:\n%s", want, typesTS)
		}
	}
	node := tooltest.RequireNode(t)
	for _, m := range codegentest.TSTypedValuesMismatches(t, node, []codegentest.Case{c}, common.GenOptions{}, 0, nil) {
		t.Error(m)
	}
}
