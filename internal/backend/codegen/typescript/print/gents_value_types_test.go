package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestTSValuesHaveTheirTypes is TestGeneratedValuesHaveTheirTypes of the Go generator for the
// TypeScript parsers.
func TestTSValuesHaveTheirTypes(t *testing.T) {
	node := tooltest.RequireNode(t)
	codegentest.EachMode(t, func(t *testing.T) {
		for _, m := range codegentest.TSValueTypesMismatches(t, node, codegentest.ValueTypeCases(t), common.GenOptions{}, nil) {
			t.Error(m)
		}
	})
}
