package print_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

func TestCompareGeneratedGoParity(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testCompareGeneratedGoParity)
}

func testCompareGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	codegentest.GeneratedParsersCorpus(t, goBin, codegentest.CompareCases(), common.GenOptions{})
	codegentest.GeneratedParsersCorpus(t, goBin, codegentest.CompareCases(), common.GenOptions{SourceInfo: true})
	codegentest.GeneratedTypesCorpus(t, goBin, codegentest.CompareCases(), common.GenOptions{ConvertTypes: true})
}
