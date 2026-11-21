package print_test

import (
	"os/exec"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

func TestCompareGeneratedTypeScriptParity(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	codegentest.TSParsersCorpus(t, node, codegentest.CompareCases(), common.GenOptions{})
	codegentest.TSParsersCorpus(t, node, codegentest.CompareCases(), common.GenOptions{SourceInfo: true})
}
