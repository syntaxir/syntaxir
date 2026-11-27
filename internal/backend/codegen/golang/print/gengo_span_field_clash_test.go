package print_test

import (
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedTypedFieldsNamedLikeSpanFields checks that a struct field named Start or End does
// not shadow the fields the struct promotes from the embedded Span: the Go field gets a trailing
// underscore, so Start and End keep reading the span.
func TestGeneratedTypedFieldsNamedLikeSpanFields(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testGeneratedTypedFieldsNamedLikeSpanFields)
}

func testGeneratedTypedFieldsNamedLikeSpanFields(t *testing.T) {
	g, err := parseTest(`type Thing = { Start int; End int }
rule main Thing = a:"a" => Thing{Start: 7, End: 8}`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: "p", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	var fields []string
	for _, line := range strings.Split(string(code), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && (strings.HasPrefix(f[0], "Start") || strings.HasPrefix(f[0], "End")) {
			fields = append(fields, f[0])
		}
	}
	for _, want := range []string{"Start_", "End_"} {
		if !slices.Contains(fields, want) {
			t.Errorf("no field %s in the generated Thing; fields starting with Start or End: %v", want, fields)
		}
	}
	for _, bad := range []string{"Start", "End"} {
		if slices.Contains(fields, bad) {
			t.Errorf("a field %s shadows the promoted field of Span", bad)
		}
	}
}

// TestGeneratedTypedFieldsNamedLikeTheLibrary checks the names of struct fields named like the names
// that the generated structs have for the typed library: each Go field gets a trailing underscore,
// the embedded Span keeps its span, and the typed values are the engine's, so each field reads back
// under its grammar name.
func TestGeneratedTypedFieldsNamedLikeTheLibrary(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testGeneratedTypedFieldsNamedLikeTheLibrary)
}

func testGeneratedTypedFieldsNamedLikeTheLibrary(t *testing.T) {
	c := codegentest.LibraryNamedFieldsCase()
	g, err := parseTest(c.Src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: "p", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Start_ int", "End_ int", "Range_ int", "Span_ int", "TName_ string"} {
		if !regexp.MustCompile(`(?m)^\t` + strings.Replace(want, " ", ` +`, 1) + `$`).Match(code) {
			t.Errorf("the generated Thing has no field %q", want)
		}
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, m := range codegentest.GeneratedTypesMismatches(t, goBin, []codegentest.Case{c}, common.GenOptions{ConvertTypes: true}, nil, 1) {
		t.Error(m)
	}
}
