package print_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

func TestValueResultsGeneratedGoParity(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testValueResultsGeneratedGoParity)
}

func testValueResultsGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	codegentest.GeneratedParsersCorpus(t, goBin, codegentest.ValueResultCases(), common.GenOptions{})
	codegentest.GeneratedTypesCorpus(t, goBin, codegentest.ValueResultCases(), common.GenOptions{ConvertTypes: true})
}
