package common_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

func TestGeneratedConstructedFieldChecks(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testGeneratedConstructedFieldChecks)
}

func testGeneratedConstructedFieldChecks(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := codegentest.FieldCheckCases()
	codegentest.GeneratedParsersCorpus(t, goBin, cases, common.GenOptions{})
	codegentest.GeneratedTypesCorpus(t, goBin, cases, common.GenOptions{ConvertTypes: true})
	if node, err := exec.LookPath("node"); err == nil {
		codegentest.TSParsersCorpus(t, node, cases, common.GenOptions{})
	} else {
		t.Error("Node is required for field-check parity")
	}
}
