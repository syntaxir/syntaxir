//go:build sxir_long

package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedTSRuleValueMatchesGoLong runs the comparison of parseRuleValue with the Go ParseRuleValue on the whole
// typed corpus of the generator (the directly cut and the joined typed grammars), which the default tier samples.
func TestGeneratedTSRuleValueMatchesGoLong(t *testing.T) {
	t.Parallel()
	checkRuleValue(t, append(codegentest.TypedFramedCutCases(t), codegentest.TypedJoinMemberCases()...), 0)
}
