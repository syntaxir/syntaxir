package print_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

func TestUnsetFieldsReadZeroGeneratedGoParity(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testUnsetFieldsReadZeroGeneratedGoParity)
}

func testUnsetFieldsReadZeroGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	codegentest.GeneratedParsersCorpus(t, goBin, codegentest.UnsetZeroCases(), common.GenOptions{})
	codegentest.GeneratedTypesCorpus(t, goBin, codegentest.UnsetZeroCases(), common.GenOptions{ConvertTypes: true}, 0)
}
