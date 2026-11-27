//go:build sxir_long

package print_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedValueTypesDetectWrongValues is the negative control of TestGeneratedValuesHaveTheirTypes:
// a generated Go parser that sets a struct field from the capture of another kind, or whose rule of a
// named node type returns a node without the name, returns values that the check reports as not of
// their types, not only as different from the engine's.
func TestGeneratedValueTypesDetectWrongValues(t *testing.T) {
	codegentest.PinOrdinary(t)
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, m := range []struct{ name, from, to string }{
		{"a struct field is set from the capture of an int", `c.ActionSetField(tn3, "A", tf1)`, `c.ActionSetField(tn3, "A", tf2)`},
		{"a rule of a named node type returns a node without the name", `{1, "", "Id", "", 0}`, `{1, "", "", "", 0}`},
	} {
		t.Run(m.name, func(t *testing.T) {
			applied := false
			got := codegentest.GeneratedValueTypesMismatches(t, goBin, []codegentest.Case{codegentest.ValueTypesMutantCase}, common.GenOptions{}, func(code []byte) []byte {
				applied = strings.Contains(string(code), m.from)
				return []byte(strings.Replace(string(code), m.from, m.to, 1))
			})
			if !applied {
				t.Fatalf("the generated parser has no %q", m.from)
			}
			codegentest.RequireValueTypeMismatch(t, m.name, got)
		})
	}
}
