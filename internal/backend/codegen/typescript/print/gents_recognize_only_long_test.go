//go:build sxir_long

package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedTSRecognizeOnlyMatchesFullBuildLong runs the recognize-only check on the whole generator
// corpus, which the default tier samples (codegentest.Representative).
func TestGeneratedTSRecognizeOnlyMatchesFullBuildLong(t *testing.T) {
	t.Parallel()
	checkRecognizeOnly(t, codegentest.GenCorpus(t))
}
