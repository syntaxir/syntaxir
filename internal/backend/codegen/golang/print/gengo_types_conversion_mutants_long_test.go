//go:build sxir_long

package print_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
)

// typedConversionMutants each break one conversion of the reference route of typed Go parsers, which
// converts the tree (the typed parsers build options of scalars directly and need no conversion of
// them): an absent option of a scalar converts as the zero of the scalar instead of nil, in the
// helpers astPtrInt, astPtrString and astPtrBool. Go has no counterpart of an unset
// tuple that converts as nil, since a tuple is a struct value whose unset form is its zero value.
var typedConversionMutants = []struct{ name, from, to string }{
	{"an absent option of an int converts as zero", "(int); ok {\n\t\treturn &i\n\t}\n\treturn nil\n}", "(int); ok {\n\t\treturn &i\n\t}\n\tz := 0\n\treturn &z\n}"},
	{"an absent option of a string converts as empty", "(string); ok {\n\t\treturn &s\n\t}\n\treturn nil\n}", "(string); ok {\n\t\treturn &s\n\t}\n\tz := \"\"\n\treturn &z\n}"},
	{"an absent option of a bool converts as false", "(bool); ok {\n\t\treturn &x\n\t}\n\treturn nil\n}", "(bool); ok {\n\t\treturn &x\n\t}\n\tz := false\n\treturn &z\n}"},
}

// TestGeneratedTypesDetectBrokenConversion is the negative control of the comparison of the routes of
// typed Go parsers (TestGeneratedTypes), whose typed parsers are compared with the engine: on the first
// grammar that witnesses each facet of each row of the Go table of typed values, each mutant of
// typedConversionMutants, whose text must occur in the reference route, must make the routes differ.
func TestGeneratedTypesDetectBrokenConversion(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	codegentest.EachMode(t, testGeneratedTypesDetectBrokenConversion)
}

func testGeneratedTypesDetectBrokenConversion(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := codegentest.GoTypedRows.Cover(t, append(codegentest.GeneratedTypesCases(t), codegentest.TypedInsertionCorpus()...))
	for _, m := range typedConversionMutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			var hits atomic.Int64
			got := codegentest.GeneratedTypesReferenceMismatches(t, goBin, cases, common.GenOptions{ConvertTypes: true}, func(code []byte) []byte {
				s := string(code)
				if strings.Contains(s, m.from) {
					hits.Add(1)
				}
				return []byte(strings.ReplaceAll(s, m.from, m.to))
			}, 0)
			if hits.Load() == 0 {
				t.Fatalf("no generated file has %q", m.from)
			}
			routes := 0
			for _, g := range got {
				if strings.HasPrefix(g, "the typed library differs from the reference route") {
					routes++
				}
			}
			if routes == 0 {
				t.Errorf("the reference route differs nowhere from the typed parsers; mismatches:\n%s", strings.Join(got, "\n"))
			}
			t.Logf("%d of %d mismatches between the routes", routes, len(got))
		})
	}
}
