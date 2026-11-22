package print_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// A parser generated under a bisection limit of 0, which skips every optional run and generates
// once with the rules that the printed code cannot reach, is a Go package that type-checks, as is
// one with typed output.
func TestBisectLimitZeroBuilds(t *testing.T) {
	zero := 0
	for _, types := range []bool{false, true} {
		var out strings.Builder
		opts := common.GenOptions{Module: "example.com/bisect", Lib: common.LibImport, Types: types,
			Debug: common.Debug{OptBisectLimit: &zero, Output: &out}}
		files := generateWithStats(t, opts, nil)
		if !strings.HasPrefix(out.String(), "opt-bisect: 1 skip ") || strings.Contains(out.String(), " run ") {
			t.Fatalf("types %v: the decisions under limit 0:\n%.500s", types, out.String())
		}
		dir := t.TempDir()
		codegentest.WriteGenerated(t, dir, "example.com/bisect", files)
		codegentest.GoVetAndTest(t, dir, false)
	}
}
