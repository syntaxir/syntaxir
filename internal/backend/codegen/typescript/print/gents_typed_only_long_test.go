//go:build sxir_long

package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedTSTypedOnlyMatchesFullBuildLong runs the typed-only check on the whole typed corpus of the generator
// (the directly cut and the joined typed grammars), which the default tier samples.
func TestGeneratedTSTypedOnlyMatchesFullBuildLong(t *testing.T) {
	t.Parallel()
	checkTypedOnly(t, append(codegentest.TypedFramedCutCases(t), codegentest.TypedJoinMemberCases()...), 0)
}
