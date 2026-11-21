package common_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestGeneratedLongExpectedLists checks that generated Go parsers, untyped and typed, and generated
// TypeScript parsers give the engine's messages for syntax errors that expect more than ten
// entries, in both units.
func TestGeneratedLongExpectedLists(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testGeneratedLongExpectedLists)
}

func testGeneratedLongExpectedLists(t *testing.T) { checkGeneratedCase(t, "long expected lists") }

// TestGeneratedLiteralExpectedText checks that generated Go parsers, untyped and typed, and generated
// TypeScript parsers print the text of literal expected items, escaped, non-ASCII and
// case-insensitive, as the engine does.
func TestGeneratedLiteralExpectedText(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, func(t *testing.T) { checkGeneratedCase(t, "literal expected text") })
}

func checkGeneratedCase(t *testing.T, name string) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	var cases []codegentest.Case
	for _, c := range codegentest.GenCorpus(t) {
		if c.Name == name {
			cases = append(cases, c)
		}
	}
	if len(cases) != 1 {
		t.Fatalf("%d cases", len(cases))
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	codegentest.GeneratedParsersCorpus(t, goBin, cases, common.GenOptions{})
	codegentest.GeneratedTypesCorpus(t, goBin, cases, common.GenOptions{ConvertTypes: true}, 1)
	node := tooltest.RequireNode(t)
	codegentest.TSParsersCorpus(t, node, cases, common.GenOptions{})
}
