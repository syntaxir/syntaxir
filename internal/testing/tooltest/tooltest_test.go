package tooltest

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// requireToolChildEnv makes TestRequireToolChild call the helper that the variable names, in the test
// binary that TestRequireToolWithoutTools starts.
const requireToolChildEnv = "TOOLTEST_CHILD"

// TestRequireToolChild is the body that TestRequireToolWithoutTools runs in a child process, because a
// test that fails cannot be observed from inside its own process. Alone it does nothing.
func TestRequireToolChild(t *testing.T) {
	switch os.Getenv(requireToolChildEnv) {
	case "":
		t.Skip("runs only as the child of TestRequireToolWithoutTools")
	case "node":
		RequireNode(t)
	case "tsc":
		RequireTSC(t)
	}
	t.Log("the tool was found")
}

// TestRequireToolWithoutTools checks the rule of RequireNode and RequireTSC with an empty PATH: the
// test fails naming the tool; SKIP_NODE=1 skips it; SKIP_NODE=0 or unset does not skip.
func TestRequireToolWithoutTools(t *testing.T) {
	for _, tc := range []struct {
		tool, env string
		skipped   bool
	}{
		{"node", "", false},
		{"tsc", "", false},
		{"node", "SKIP_NODE=1", true},
		{"tsc", "SKIP_NODE=1", true},
		{"node", "SKIP_NODE=0", false},
	} {
		t.Run(tc.tool+"/"+tc.env, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestRequireToolChild$", "-test.v")
			// The environment starts from nothing: the tools are not found, and the opt-out is only what the case sets.
			cmd.Env = []string{"PATH=" + t.TempDir(), requireToolChildEnv + "=" + tc.tool}
			if tc.env != "" {
				cmd.Env = append(cmd.Env, tc.env)
			}
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			failed := errors.As(err, &exit)
			if err != nil && !failed {
				t.Fatal(err)
			}
			text := string(out)
			switch {
			case tc.skipped && (failed || !strings.Contains(text, "--- SKIP: TestRequireToolChild")):
				t.Errorf("with %s the test is skipped, got (failed %v):\n%s", tc.env, failed, text)
			case !tc.skipped && (!failed || !strings.Contains(text, tc.tool+" not found")):
				t.Errorf("the test fails naming %s, got (failed %v):\n%s", tc.tool, failed, text)
			}
			if strings.Contains(text, "the tool was found") {
				t.Errorf("the test went on after the missing tool:\n%s", text)
			}
		})
	}
}
