package common_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

func TestGeneratedRuleResultChecks(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testGeneratedRuleResultChecks)
}

func testGeneratedRuleResultChecks(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := codegentest.ResultCheckCases()
	codegentest.GeneratedParsersCorpus(t, goBin, cases, common.GenOptions{})
	codegentest.GeneratedTypesCorpus(t, goBin, cases, common.GenOptions{ConvertTypes: true})
	if node, err := exec.LookPath("node"); err == nil {
		codegentest.TSParsersCorpus(t, node, cases, common.GenOptions{})
	} else {
		t.Error("Node is required for result-check parity")
	}
}
