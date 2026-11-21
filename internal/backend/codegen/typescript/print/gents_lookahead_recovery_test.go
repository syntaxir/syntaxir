package print_test

import (
	"os/exec"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedTSLookaheadRecoveryMatchesEngine compares the results and errors of TypeScript
// parsers that recover errors inside lookaheads and Operators Rule skips with the engine's.
func TestGeneratedTSLookaheadRecoveryMatchesEngine(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	codegentest.TSParsersCorpus(t, node, codegentest.LookaheadRecoveryCases(), common.GenOptions{})
}
