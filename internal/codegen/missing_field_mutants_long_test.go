//go:build zp_long

package codegen_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"zeroparse.dev/zp/internal/codegentest"
)

// missingFieldMutants each give a missing token that the engine inserts a wrong field expected or message, in
// MissingLiteral or MissingErrorScope of internal/engine/state/missing_token.go.
var missingFieldMutants = []struct{ name, fn, old, new string }{
	{"expected holds the message", "MissingLiteral", `{Name: "expected", Value: expected}`, `{Name: "expected", Value: p.RecoveryErrorText(e)}`},
	{"expected is quoted", "MissingLiteral", `{Name: "expected", Value: expected}`, `{Name: "expected", Value: "\"" + expected + "\""}`},
	{"message holds the literal", "MissingLiteral", `{Name: "message", Value: p.RecoveryErrorText(e)}`, `{Name: "message", Value: expected}`},
	{"message has no position", "MissingLiteral", `{Name: "message", Value: p.RecoveryErrorText(e)}`, `{Name: "message", Value: e.Message()}`},
	{"message ignores @error", "MissingErrorScope", "if p.Expectations && p.Missing != nil && p.Missing.label == 0 {", "if false {"},
}

// TestMissingFieldMutants is the proof that TestTermFixedReference holds the fields expected and message of a
// missing token: with a source overlay of each mutant, it fails.
func TestMissingFieldMutants(t *testing.T) {
	root := codegentest.ModuleRoot()
	path := filepath.Join(root, "internal", "engine", "state", "missing_token.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range missingFieldMutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			mutated, ok := codegentest.MutateFunc(string(src), m.fn, m.old, m.new)
			if !ok {
				t.Fatalf("missing_token.go has no %s with %q", m.fn, m.old)
			}
			dir := t.TempDir()
			file := filepath.Join(dir, "missing_token.go")
			if err := os.WriteFile(file, []byte(mutated), 0o644); err != nil {
				t.Fatal(err)
			}
			overlay, _ := json.Marshal(map[string]map[string]string{"Replace": {path: file}})
			overlayFile := filepath.Join(dir, "overlay.json")
			if err := os.WriteFile(overlayFile, overlay, 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "test", "-count=1", "-overlay", overlayFile, "-run", "^TestTermFixedReference$", "./internal/codegen")
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOFLAGS=")
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("TestTermFixedReference passes with the mutant %q:\n%s", m.name, out)
			}
			if !strings.Contains(string(out), "--- FAIL: TestTermFixedReference") {
				t.Fatalf("the mutant %q fails no reference comparison:\n%s", m.name, out)
			}
		})
	}
}
