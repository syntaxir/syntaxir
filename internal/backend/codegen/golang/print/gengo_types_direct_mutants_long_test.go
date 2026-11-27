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

// directRouteMutants each break one conversion of the direct route of typed Go parsers (dconv and the
// constructors and list and tuple converters of the typed library), in text that only the direct
// route prints.
var directRouteMutants = []struct{ name, from, to string }{
	{"a list converter reverses its elements", "for i, c := range n.Kids {\n\t\ta.spend()\n\t\tout[i] = ", "for i, c := range n.Kids {\n\t\ta.spend()\n\t\tout[len(out)-1-i] = "},
	{"a tuple converter reads its first element as its second", "TKid(x.Kids, 1)", "TKid(x.Kids, 0)"},
	{"a struct starts one position late", "{Start: c.Start, End: c.End}\n\t\treturn v", "{Start: c.Start + 1, End: c.End}\n\t\treturn v"},
}

// TestGeneratedTypesDetectBrokenDirectRoute is the negative control of the comparison of the direct
// route of typed Go parsers with the converted route (TestGeneratedTypes): on the first grammar
// that converts types of each row of the Go table of typed values, each mutant of directRouteMutants,
// whose text must occur, must make the typed values differ from those of the reference route, and
// the unchanged parsers must not.
func TestGeneratedTypesDetectBrokenDirectRoute(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	codegentest.EachMode(t, testGeneratedTypesDetectBrokenDirectRoute)
}

func testGeneratedTypesDetectBrokenDirectRoute(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := codegentest.GoTypedRows.Cover(t, append(codegentest.GeneratedTypesCases(t), codegentest.TypedInsertionCorpus()...))
	if got := codegentest.GeneratedTypesMismatches(t, goBin, cases, common.GenOptions{ConvertTypes: true}, nil, 0); len(got) != 0 {
		t.Fatalf("control: the unchanged parsers differ:\n%s", strings.Join(got, "\n"))
	}
	for _, m := range directRouteMutants {
		t.Run(m.name, func(t *testing.T) {
			var hits atomic.Int64
			got := codegentest.GeneratedTypesMismatches(t, goBin, cases, common.GenOptions{ConvertTypes: true}, func(code []byte) []byte {
				s := string(code)
				if strings.Contains(s, m.from) {
					hits.Add(1)
				}
				return []byte(strings.ReplaceAll(s, m.from, m.to))
			}, 0)
			if hits.Load() == 0 {
				t.Fatalf("no generated file has %q", m.from)
			}
			direct := 0
			for _, g := range got {
				if strings.HasPrefix(g, "the typed library differs from the reference route") {
					direct++
				}
			}
			if direct == 0 {
				t.Errorf("the direct route differs nowhere from the reference route; mismatches:\n%s", strings.Join(got, "\n"))
			}
			t.Logf("%d of %d mismatches between the routes", direct, len(got))
		})
	}
}
