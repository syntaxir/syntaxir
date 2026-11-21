package common_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

func TestGeneratedTextStructs(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testGeneratedTextStructs)
}

func testGeneratedTextStructs(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cases := []codegentest.Case{{Name: "text structs", Src: codegentest.TextStructGrammar, Inputs: []string{"", "ab", "ab 12 3.5", "<x> ! #a # @b @", "ab ?", "12.", "! ab <yz> 7", "[ab]ab", "[ab]cd 1"}}}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	codegentest.GeneratedParsersCorpus(t, goBin, cases, common.GenOptions{})
	codegentest.GeneratedTypesCorpus(t, goBin, cases, common.GenOptions{ConvertTypes: true}, 1)
	node := tooltest.RequireNode(t)
	codegentest.TSParsersCorpus(t, node, cases, common.GenOptions{})
}
