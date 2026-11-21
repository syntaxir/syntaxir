package print_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

func TestFilterGeneratedGoParity(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testFilterGeneratedGoParity)
}

func testFilterGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	codegentest.GeneratedParsersCorpus(t, goBin, codegentest.FilterCases(), common.GenOptions{})
	codegentest.GeneratedTypesCorpus(t, goBin, codegentest.FilterCases(), common.GenOptions{})
}
