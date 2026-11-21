package common_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

func TestGeneratedNodeConstructors(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testGeneratedNodeConstructors)
}

func testGeneratedNodeConstructors(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := codegentest.ConstructorCases()
	codegentest.GeneratedParsersCorpus(t, goBin, cases, common.GenOptions{})
	codegentest.GeneratedTypesCorpus(t, goBin, cases, common.GenOptions{ConvertTypes: true}, 4)
	if node, err := exec.LookPath("node"); err == nil {
		codegentest.TSParsersCorpus(t, node, cases, common.GenOptions{})
	} else {
		t.Error("Node is required for constructor parity")
	}
}
