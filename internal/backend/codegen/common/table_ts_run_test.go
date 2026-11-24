package common_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// runTSTable runs main.ts of the module in dir, which a harness of a table wrote, in a worker of the node
// pool (codegentest.RunNodeScript), and returns what it printed. A script that fails fails t with its
// error output, as the node process that the harnesses started before did.
func runTSTable(t *testing.T, node, dir string) []byte {
	t.Helper()
	res, err := codegentest.RunNodeScript(t, node, dir, "main.ts")
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	if res.Code != 0 {
		t.Fatalf("node: exit status %d\n%s", res.Code, codegentest.Clip(res.Stderr, 0))
	}
	return []byte(res.Stdout)
}
