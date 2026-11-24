package print_test

import (
	"encoding/json"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestGeneratedTSRecoverBottom checks that a generated TypeScript parser gives the recovery of an
// expression of type _|_ an error value, and an expression without a value none: each parse
// succeeds or fails as codegentest.RecoverBottomCases states, and agrees with the engine.
func TestGeneratedTSRecoverBottom(t *testing.T) {
	t.Parallel()
	checkGeneratedTSRecoverCases(t, codegentest.RecoverBottomCases)
}

// TestGeneratedTSRecoveredCaptures checks that in a generated TypeScript parser a capture inside a
// recovered expression is null after the recovery, while a capture of the recovery and a name
// captured again after it are set: each parse succeeds or fails as
// codegentest.RecoveredCaptureCases states, and agrees with the engine.
func TestGeneratedTSRecoveredCaptures(t *testing.T) {
	t.Parallel()
	checkGeneratedTSRecoverCases(t, codegentest.RecoveredCaptureCases)
}

// checkGeneratedTSRecoverCases checks that each parse of a generated TypeScript parser of cases
// succeeds or fails as the case states and gives the engine's result.
func checkGeneratedTSRecoverCases(t *testing.T, recoverCases []codegentest.RecoverCase) {
	t.Helper()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node := tooltest.RequireNode(t)
	cases := codegentest.RecoverGenCases(recoverCases)
	lines := strings.Split(strings.TrimSuffix(codegentest.TSParsersOutputs(t, node, cases, common.GenOptions{}, nil), "\n"), "\n")
	k := 0
	for _, c := range recoverCases {
		for i, in := range c.Inputs {
			// Each ASCII input runs in code points and bytes, as bytes and as a string.
			for range 4 {
				if k >= len(lines) {
					t.Fatalf("%d result lines, want more", len(lines))
				}
				var r struct{ Node string } // the JSON text of the node, "null" for a failed parse
				if err := json.Unmarshal([]byte(lines[k]), &r); err != nil {
					t.Fatalf("%s: %v", lines[k], err)
				}
				if accepted := r.Node != "null"; accepted != c.Accept[i] {
					t.Errorf("%s, %q: accepted %v, want %v\n%s", c.Name, in, accepted, c.Accept[i], lines[k])
				}
				k++
			}
		}
	}
	if k != len(lines) {
		t.Errorf("%d result lines, want %d", len(lines), k)
	}
	codegentest.TSParsersCorpus(t, node, cases, common.GenOptions{})
}
