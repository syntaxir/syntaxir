// Package tooltest finds the external tools that tests run (node, tsc) with one rule: a test that needs
// a tool fails when it is missing, so that a comparison with generated TypeScript cannot pass by not
// running. The environment variable SKIP_NODE=1 is the only way to skip such tests instead; it covers
// tsc too, which runs on node.
//
// The package has no dependency on the code generators, so that every test can use it, including the
// tests of the generators' own packages.
package tooltest

import (
	"os"
	"os/exec"
	"testing"
)

// skipEnv names the environment variable that opts out of the tests that need Node or tsc.
const skipEnv = "SKIP_NODE"

// OptedOut reports whether the environment variable SKIP_NODE is 1: the one way to run the tests
// without Node and tsc, which RequireNode and RequireTSC then skip instead of failing.
func OptedOut() bool { return os.Getenv(skipEnv) == "1" }

// RequireNode returns the Node executable. When there is none it fails the test, naming node, unless
// SKIP_NODE is 1, when it skips the test.
func RequireNode(t *testing.T) string {
	t.Helper()
	return require(t, "node")
}

// RequireTSC returns the TypeScript compiler executable, with the rule of RequireNode.
func RequireTSC(t *testing.T) string {
	t.Helper()
	return require(t, "tsc")
}

func require(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		if OptedOut() {
			t.Skipf("%s not found, and %s=1", name, skipEnv)
		}
		t.Fatalf("%s not found: %v (install it, or set %s=1 to skip the tests that need Node or tsc)", name, err, skipEnv)
	}
	return path
}
