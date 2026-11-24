//go:build sxir_long

package print_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestGeneratedTSInsertMissingTokensDetectsMutation is the negative control of
// TestGeneratedTSInsertMissingTokens: a library whose insertions count no unit, or whose missing nodes
// hold another literal, differs from the engine.
func TestGeneratedTSInsertMissingTokensDetectsMutation(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	node := tooltest.RequireNode(t)
	cases := codegentest.InsertionCases()[:1]
	for _, m := range []struct{ name, old, new string }{
		{"insertion without a unit", "    if (logical) {\n      this.unitsSized(1);\n    }\n    if (s.label !== 0) {", "    if (s.label !== 0) {"},
		{"missing node of another literal", `[{ name: "expected", value: expected }`, `[{ name: "expected", value: expected + "!" }`},
	} {
		replaced := false
		mismatches := codegentest.TSParsersInsertionMismatches(t, node, cases, common.GenOptions{}, 2, func(src string) string {
			replaced = replaced || strings.Contains(src, m.old)
			return strings.ReplaceAll(src, m.old, m.new)
		})
		if !replaced {
			t.Fatalf("%s: the library has no %q", m.name, m.old)
		}
		if len(mismatches) == 0 {
			t.Errorf("%s: every parse matched the engine's", m.name)
		}
	}
}

// TestStreamOracleInsertMissingTokensDetectsMutation is the negative control of
// TestStreamOracleInsertMissingTokens: with a library that gives each element of a stream a limit of
// its own, some stream differs from the engine's.
func TestStreamOracleInsertMissingTokensDetectsMutation(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	// The mutant resets the attempts of the limit around each delivered element.
	const check = "  streamCheck(): void {\n"
	perElement := func(src string) string {
		if !strings.Contains(src, check) {
			t.Fatalf("the library has no %q", check)
		}
		return strings.Replace(src, check, check+"    if (this.missing !== null) this.missing.attempts = 0;\n", 1)
	}
	for _, c := range codegentest.IncrementalInsertionCorpus() {
		for _, limit := range streamInsertionLimits[1:3] {
			if len(streamOracleMismatches(t, streamedInsertionCase(c.Src), c.Inputs, streamOptions{Missing: limit}, perElement)) > 0 {
				return
			}
		}
	}
	t.Error("a limit per element: every stream matched the engine's")
}

// TestTSDocumentsInsertMissingTokensDetectsMutation is the negative control of
// TestTSDocumentsInsertMissingTokens: with a library that records the runs whose elements reached an
// insertion site, some Document parse differs from the parse from scratch.
func TestTSDocumentsInsertMissingTokensDetectsMutation(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	node := tooltest.RequireNode(t)
	const clean = " && this.missingReached() === r.reached0;\n"
	patched := 0
	every := everyRun(&patched)
	ignoring := func(src string) string {
		src = every(src)
		if !strings.Contains(src, clean) {
			return src // the parser records no runs
		}
		return strings.Replace(src, clean, ";\n", 1)
	}
	if len(tsDocumentInsertionMismatches(t, node, codegentest.IncrementalInsertionCorpus(), ignoring, false, false)) == 0 {
		t.Error("runs that ignore insertions: every Document parse matched the parse from scratch")
	}
	if patched == 0 {
		t.Fatal("no Document of the corpus records runs")
	}
}
