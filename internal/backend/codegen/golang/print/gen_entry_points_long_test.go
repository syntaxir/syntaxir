//go:build sxir_long

package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// Every combination of entry points over the grammars whose typed entry points convert Nodes.
func TestGeneratedEntryPointCombinationsConverted(t *testing.T) {
	t.Parallel()
	testEntryCombinations(t, codegentest.TypedOnlyConverted)
}
