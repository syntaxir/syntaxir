//go:build sxir_long

package print_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// The analysis ts-printable admits every table of every grammar of the corpora under each option set
// that TypeScript generates with: no grammar reaches a construct that the TypeScript printer does not
// print, which is why a table that the analysis rejects is an internal error (typescript.Printable).
// A generation may still fail for a reason of its own, before the analysis, such as a grammar type
// that the TypeScript types reserve; only a failure of the analysis fails the test.
func TestTSPrintableAdmitsTheCorpora(t *testing.T) {
	var cases []codegentest.Case
	for _, cs := range [][]codegentest.Case{
		codegentest.CallProtocolCases(),
		codegentest.CaptureAccessCases(),
		codegentest.CorpusWithParsers(t),
		codegentest.SchemaCorpus(t),
		codegentest.FieldCheckCases(),
		codegentest.FilterCases(),
		codegentest.CastCases(),
		codegentest.CompareCases(),
		codegentest.ConstructorCases(),
		codegentest.GrammarCases(t),
		codegentest.ExaminedShapes(),
		codegentest.TypedFramedCutCases(t),
		codegentest.SpliceGenCases(),
		codegentest.FlagParityCases(),
		codegentest.GenCorpus(t),
		codegentest.SpliceTypeCases(t),
		codegentest.OperatorsNamedNodeCases(),
		codegentest.LookaheadRecoveryCases(),
		codegentest.RunSiteCases(4, true),
		codegentest.IndexCases(),
		codegentest.NodeTypeCases(),
		codegentest.LeftRecursionMemberCases(),
		codegentest.LeftRecursionMemberSeedCases(),
		codegentest.LeftRecursionMemberDocumentCases(),
		codegentest.DependencyMemoControlCases(),
		codegentest.DependencyMemoCases(4),
		codegentest.RecoverBottomGenCases(),
		codegentest.UsedFeatureCases(),
		codegentest.PredicateBlockGeneratedCases(),
		codegentest.InlineActionGenCases(),
		codegentest.WideRepetitionCases(),
		codegentest.ResultCheckCases(),
		codegentest.JoinMemberCases(),
		codegentest.TypedJoinMemberCases(),
		codegentest.SkipRunCases(4),
		codegentest.InvalidLiteralCorpus(),
		codegentest.TopBottomCases(),
		codegentest.ScanSiteCases(4),
		codegentest.GeneratedTypesCases(t),
		codegentest.ValueResultCases(),
		codegentest.UnsetZeroCases(),
	} {
		cases = append(cases, cs...)
	}
	sets := []struct {
		name string
		opts common.GenOptions
	}{
		{"plain", common.GenOptions{Start: "main", NoLimitControls: true}},
		{"recognize", common.GenOptions{Start: "main", Recognize: true}},
		{"types", common.GenOptions{Start: "main", Types: true, Recognize: true}},
		{"document", common.GenOptions{Start: "main", Document: true, Types: true}},
		{"observability", common.GenOptions{Start: "main", Observability: true, Recognize: true}},
		{"size", common.GenOptions{Start: "main", Types: true, Optimize: "size"}},
	}
	generated := 0
	for _, c := range cases {
		g, err := compiletest.Parse(c.Src)
		if err != nil {
			continue
		}
		for _, s := range sets {
			_, err := codegentest.GenerateTSRaw(g, s.opts)
			switch {
			case err == nil:
				generated++
			case strings.Contains(err.Error(), "ts-printable"), strings.Contains(err.Error(), "does not print"):
				t.Errorf("%s (%s): %v", c.Name, s.name, err)
			}
		}
	}
	// The corpora have about two thousand grammars; most generate under every set.
	if generated < 5000 {
		t.Errorf("%d generations succeeded, want at least 5000", generated)
	}
}
