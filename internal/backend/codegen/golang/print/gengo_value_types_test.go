package print_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedValuesHaveTheirTypes checks the first part of the type-soundness theorem
// (spec/proofs/type-soundness.md) on the generated Go parsers: the value of every successful parse
// of the corpus from main, and of every rule on each part of an input that an evaluation of the rule
// matched in the engine, is a well-formed value of the rule's checked type, and equals the engine's.
func TestGeneratedValuesHaveTheirTypes(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	codegentest.EachMode(t, func(t *testing.T) {
		for _, m := range codegentest.GeneratedValueTypesMismatches(t, goBin, codegentest.ValueTypeCases(t), common.GenOptions{}, nil) {
			t.Error(m)
		}
	})
}
