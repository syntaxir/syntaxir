package print_test

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestGeneratedRecoverBottom checks that a generated Go parser gives the recovery of an expression
// of type _|_ an error value, and an expression without a value none: each parse succeeds or fails
// as codegentest.RecoverBottomCases states, and agrees with the engine.
func TestGeneratedRecoverBottom(t *testing.T) {
	t.Parallel()
	checkGeneratedRecoverCases(t, codegentest.RecoverBottomCases)
}

// TestGeneratedRecoveredCaptures checks that in a generated Go parser a capture inside a recovered
// expression is nil after the recovery, while a capture of the recovery and a name captured again
// after it are set: each parse succeeds or fails as codegentest.RecoveredCaptureCases states, agrees
// with the engine, and builds with the capture typed as optional.
func TestGeneratedRecoveredCaptures(t *testing.T) {
	t.Parallel()
	checkGeneratedRecoverCases(t, codegentest.RecoveredCaptureCases)
}

// checkGeneratedRecoverCases checks that each parse of a generated Go parser of cases, in every mode,
// succeeds or fails as the case states and gives the engine's result, also with typed values.
func checkGeneratedRecoverCases(t *testing.T, recoverCases []codegentest.RecoverCase) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := codegentest.RecoverGenCases(recoverCases)
	codegentest.EachMode(t, func(t *testing.T) {
		got, want := codegentest.GeneratedParsersOutputs(t, goBin, cases, common.GenOptions{}, nil)
		k := 0
		for _, c := range recoverCases {
			for i, in := range c.Inputs {
				for _, unit := range []string{"codepoints", "bytes"} {
					head, _, _ := strings.Cut(got[k], " views: ")
					var r struct{ Node json.RawMessage }
					if err := json.Unmarshal([]byte(head), &r); err != nil {
						t.Fatalf("%s: %v", got[k], err)
					}
					if accepted := string(r.Node) != "null"; accepted != c.Accept[i] {
						t.Errorf("%s (%s), %q: accepted %v, want %v\n%s", c.Name, unit, in, accepted, c.Accept[i], got[k])
					}
					if got[k] != want[k] {
						t.Errorf("%s (%s), %q:\n generated %s\n engine    %s", c.Name, unit, in, got[k], want[k])
					}
					k++
				}
			}
		}
		codegentest.GeneratedTypesCorpus(t, goBin, cases, common.GenOptions{})
	})
}
