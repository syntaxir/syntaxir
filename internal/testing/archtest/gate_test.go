package archtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gateStubGo stands in for the go command in the scratch repository of TestGateGroupRunsStepsTogether: it logs its
// arguments, and its go test, the test-long step, waits until both other steps of the group have started when
// GATE_TEST_WAIT is set, failing when they do not start within 30 seconds.
const gateStubGo = `#!/bin/sh
echo "go $*" >>"$GATE_TEST_DIR/log"
[ "$1" = test ] || exit 0
echo "start test-long" >>"$GATE_TEST_DIR/log"
: >"$GATE_TEST_DIR/test.started"
if [ -n "${GATE_TEST_WAIT:-}" ]; then
	n=0
	while [ ! -e "$GATE_TEST_DIR/alpha.started" ] || [ ! -e "$GATE_TEST_DIR/ref.started" ]; do
		n=$((n + 1)); [ "$n" -lt 300 ] || { echo "test-long: the other steps did not start beside it"; exit 3; }
		sleep 0.1
	done
fi
echo "end test-long" >>"$GATE_TEST_DIR/log"
`

// gateStubStep is a stub of hack/test-alpha-parsers.sh or hack/test-reference-tags.sh: NAME is its marker, and it
// exits with $GATE_TEST_<RC> (default 0).
func gateStubStep(name, rc string) string {
	return `#!/bin/sh
echo "start ` + name + `" >>"$GATE_TEST_DIR/log"
: >"$GATE_TEST_DIR/` + name + `.started"
if [ -n "${GATE_TEST_WAIT:-}" ]; then
	n=0
	while [ ! -e "$GATE_TEST_DIR/test.started" ]; do
		n=$((n + 1)); [ "$n" -lt 300 ] || { echo "` + name + `: test-long did not start beside it"; exit 3; }
		sleep 0.1
	done
fi
echo "output of ` + name + `"
echo "end ` + name + `" >>"$GATE_TEST_DIR/log"
exit "${GATE_TEST_` + rc + `:-0}"
`
}

// TestGateGroupRunsStepsTogether runs hack/gate.sh (level engine) in a scratch repository with stubs of the go
// command, of the affected-packages computation and of the alpha-parsers and reference-tags scripts. The long-tier
// tests, alpha-parsers and reference-tags must run at the same time (each stub waits for the others to start), the
// summary must list every step in plan order with its own exit code, a failed step of the group must fail the gate
// and stop it after the group, before generate, and GATE_PARALLEL=0 must run the steps one after the other.
func TestGateGroupRunsStepsTogether(t *testing.T) {
	needTools(t)
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "hack", "gate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	w := t.TempDir()
	bin := t.TempDir()
	write(t, filepath.Join(w, "hack", "gate.sh"), string(b), 0o755)
	write(t, filepath.Join(w, "hack", "test-alpha-parsers.sh"), gateStubStep("alpha", "ALPHA_RC"), 0o755)
	write(t, filepath.Join(w, "hack", "test-reference-tags.sh"), gateStubStep("ref", "REF_RC"), 0o755)
	write(t, filepath.Join(w, "affected.sh"), "#!/bin/sh\nprintf '%s\\n' syntaxir.dev/sxir/internal/runtime/engine '# generate' '# alpha'\n", 0o755)
	write(t, filepath.Join(bin, "go"), gateStubGo, 0o755)
	env := append(scriptEnv(t, t.TempDir()), "GATE_AFFECTED_CMD="+filepath.Join(w, "affected.sh"))
	mustRun(t, w, env, "git", "init", "-q", "-b", "main")
	mustRun(t, w, env, "git", "add", ".")
	mustRun(t, w, env, "git", "commit", "-q", "-m", "Seed")
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + bin + string(os.PathListSeparator) + strings.TrimPrefix(kv, "PATH=")
		}
	}

	type result struct {
		out, log string
		summary  []string
		failed   bool
	}
	gate := func(keep bool, vars ...string) result {
		t.Helper()
		dir := t.TempDir()
		sum := filepath.Join(dir, "summary")
		args := []string{filepath.Join(w, "hack", "gate.sh"), "--base", "HEAD", "--summary", sum}
		if keep {
			args = append(args, "--keep-going")
		}
		out, err := runIn(t, w, append(append(env[:len(env):len(env)], "GATE_TEST_DIR="+dir), vars...), "bash", append(args, "engine")...)
		s, rerr := os.ReadFile(sum)
		if rerr != nil {
			t.Fatalf("no summary: %v\n%s", rerr, out)
		}
		l, _ := os.ReadFile(filepath.Join(dir, "log"))
		var lines []string
		for line := range strings.SplitSeq(strings.TrimSpace(string(s)), "\n") {
			f := strings.Split(line, "\t")
			lines = append(lines, f[0]+" "+f[1])
		}
		return result{out, string(l), lines, err != nil}
	}
	want := func(r result, failed bool, summary ...string) {
		t.Helper()
		if r.failed != failed {
			t.Errorf("the gate failed: %v, want %v\n%s", r.failed, failed, r.out)
		}
		if strings.Join(r.summary, "; ") != strings.Join(summary, "; ") {
			t.Errorf("summary\n%s\nwant\n%s\noutput:\n%s", strings.Join(r.summary, "\n"), strings.Join(summary, "\n"), r.out)
		}
	}

	r := gate(false, "GATE_TEST_WAIT=1")
	want(r, false, "build exit 0", "vet exit 0", "test-long exit 0", "alpha-parsers exit 0", "reference-tags exit 0", "generate exit 0")
	for _, s := range []string{"gate: == test-long (beside: alpha-parsers reference-tags)", "gate: == alpha-parsers (output; it ran beside test-long)\noutput of alpha", "gate: == reference-tags (output; it ran beside test-long)\noutput of ref"} {
		if !strings.Contains(r.out, s) {
			t.Errorf("the output has no %q:\n%s", s, r.out)
		}
	}

	r = gate(false, "GATE_TEST_WAIT=1", "GATE_TEST_ALPHA_RC=4")
	want(r, true, "build exit 0", "vet exit 0", "test-long exit 0", "alpha-parsers exit 4", "reference-tags exit 0")

	r = gate(true, "GATE_TEST_WAIT=1", "GATE_TEST_ALPHA_RC=4", "GATE_TEST_REF_RC=5")
	want(r, true, "build exit 0", "vet exit 0", "test-long exit 0", "alpha-parsers exit 4", "reference-tags exit 5", "generate exit 0")

	r = gate(false, "GATE_PARALLEL=0", "GATE_TEST_ALPHA_RC=4")
	want(r, true, "build exit 0", "vet exit 0", "test-long exit 0", "alpha-parsers exit 4")
	if !strings.Contains(r.log, "end test-long\nstart alpha\nend alpha\n") || strings.Contains(r.log, "start ref") {
		t.Errorf("GATE_PARALLEL=0 does not run the steps one after the other:\n%s", r.log)
	}
}
