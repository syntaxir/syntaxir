package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

func TestCompareGeneratedTypeScriptParity(t *testing.T) {
	t.Parallel()
	node := codegentest.RequireNode(t)
	codegentest.TSParsersCorpus(t, node, codegentest.CompareCases(), common.GenOptions{})
	codegentest.TSParsersCorpus(t, node, codegentest.CompareCases(), common.GenOptions{SourceInfo: true})
}
