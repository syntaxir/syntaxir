package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestGeneratedTSRecoverNoValue checks that a generated TypeScript parser gives an expression
// without a value under #recover no value, as the engine does, whether it matches or recovers.
// TestRecoverWithoutValueAddsNoElement in internal/runtime/engine checks the engine's results.
func TestGeneratedTSRecoverNoValue(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	codegentest.TSParsersCorpus(t, tooltest.RequireNode(t), codegentest.RecoverNoValueCases, common.GenOptions{})
}
