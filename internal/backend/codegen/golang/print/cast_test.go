package print_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

func TestCastGeneratedGoParity(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testCastGeneratedGoParity)
}

func testCastGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	codegentest.GeneratedParsersCorpus(t, goBin, codegentest.CastCases(), common.GenOptions{})
	codegentest.GeneratedParsersCorpus(t, goBin, codegentest.CastCases(), common.GenOptions{SourceInfo: true})
	codegentest.GeneratedTypesCorpus(t, goBin, codegentest.CastCases(), common.GenOptions{ConvertTypes: true})
}
