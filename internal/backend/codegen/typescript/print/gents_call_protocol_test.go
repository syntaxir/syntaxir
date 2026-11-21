package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedTSCallProtocolMatchesEngine compares the results and errors of TypeScript parsers
// whose rules reuse memo entries in lookaheads, attributes, recovery and growth with the engine's.
func TestGeneratedTSCallProtocolMatchesEngine(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	node := codegentest.RequireNode(t)
	codegentest.TSParsersCorpus(t, node, codegentest.CallProtocolCases(), common.GenOptions{})
}
