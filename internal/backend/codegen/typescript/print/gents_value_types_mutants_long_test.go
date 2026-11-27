//go:build sxir_long

package print_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestTSValueTypesDetectWrongValues is the negative control of TestTSValuesHaveTheirTypes: a
// TypeScript parser whose action of an int returns a string returns values that the check reports as
// not of their types, not only as different from the engine's.
func TestTSValueTypesDetectWrongValues(t *testing.T) {
	codegentest.PinOrdinary(t)
	node := tooltest.RequireNode(t)
	from, to := "return [1, null];", `return ["1", null];`
	applied := false
	got := codegentest.TSValueTypesMismatches(t, node, []codegentest.Case{codegentest.ValueTypesMutantCase}, common.GenOptions{}, func(code string) string {
		applied = strings.Contains(code, from)
		return strings.Replace(code, from, to, 1)
	})
	if !applied {
		t.Fatalf("the generated parser has no %q", from)
	}
	codegentest.RequireValueTypeMismatch(t, "an int action returns a string", got)
}
