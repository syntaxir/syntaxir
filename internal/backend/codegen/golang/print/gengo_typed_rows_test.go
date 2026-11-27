package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedTypesCoverEveryMappingRow checks that the corpora that compare the typed Go values
// with the engine's, and the direct route with the converted one, convert types of every row of the
// Go table of typed values (codegentest.GoTypedRows): the corpus of TestGeneratedTypes, and for the
// rows of grammars with #missing the cases of TestGeneratedTypedParsersInsertMissingTokens.
func TestGeneratedTypesCoverEveryMappingRow(t *testing.T) {
	t.Parallel()
	cases := append(codegentest.GeneratedTypesCases(t), codegentest.TypedInsertionCorpus()...)
	codegentest.GoTypedRows.RequireCovered(t, cases)
	_, first := codegentest.GoTypedRows.Uncovered(t, cases)
	for row, i := range first {
		t.Logf("%s: %s", row, cases[i].Name)
	}
}
