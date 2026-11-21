package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

func TestPredicateBlocksGeneratedTypeScriptParity(t *testing.T) {
	t.Parallel()
	node := tooltest.RequireNode(t)
	codegentest.TSParsersCorpus(t, node, codegentest.PredicateBlockGeneratedCases(), common.GenOptions{})
}
