package common_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
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
	node := tooltest.RequireNode(t)
	codegentest.TSParsersCorpus(t, node, cases, common.GenOptions{})
}
