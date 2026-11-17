package archtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The tests below run the repository scripts (hack/check-commits.sh, .agents/scripts) against a
// temporary repository and a temporary bare remote. They never touch this repository or the
// real queue.

func scriptEnv(t *testing.T, state string) []string {
	t.Helper()
	return append(os.Environ(),
		"ZP_AGENT_STATE="+state, "BENCHLOCK_POLL=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
}

func runIn(t *testing.T, dir string, env []string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustRun(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	out, err := runIn(t, dir, env, name, args...)
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return out
}

func needTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"git", "bash", "perl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestCheckCommitsClosingTrailers(t *testing.T) {
	needTools(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	env := scriptEnv(t, t.TempDir())
	mustRun(t, dir, env, "git", "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "a"), "a", 0o644)
	mustRun(t, dir, env, "git", "add", "a")
	mustRun(t, dir, env, "git", "commit", "-q", "-m", "Seed", "-m", "Initial.")
	mustRun(t, dir, env, "git", "checkout", "-q", "-b", "feat/x")
	script := filepath.Join(root, "hack", "check-commits.sh")
	for _, c := range []struct {
		subject, body string
		bad           bool
	}{
		{"Stabilize the flaky tests", "Fix 3 flaky tests in the engine.", false},
		{"Fix 3 flaky tests", "They failed under load.", false},
		{"Rework the prefix handling", "This resolves nothing about issue numbering.", false},
		{"Rework the cache", "Closes #12", true},
		{"Rework the cache", "Closes zeroparse/zp#1", true},
		{"Rework the cache", "fixes: #7", true},
		{"Rework the cache", "Resolved owner/repo#3", true},
	} {
		mustRun(t, dir, env, "git", "commit", "-q", "--allow-empty", "-m", c.subject, "-m", c.body)
		out, err := runIn(t, dir, env, "bash", script, "HEAD~1", "HEAD")
		if (err != nil) != c.bad {
			t.Errorf("subject %q body %q: bad=%v, got err=%v\n%s", c.subject, c.body, c.bad, err, out)
		}
	}
}

// stubGate stands in for hack/gate.sh: it records the levels, and GATE_MODE=kill makes it die
// after one passing step, as a gate killed by benchstop or a reboot does.
const stubGate = `#!/usr/bin/env bash
lv=
while [ $# -gt 0 ]; do case $1 in --summary) sum=$2; shift 2;; --base) shift 2;; *) lv="$lv $1"; shift;; esac; done
printf 'vet\texit 0\t0s\n' > "$sum"
[ "${GATE_MODE:-}" = kill ] && kill -9 $$
printf 'test\texit 0\t1s\n' >> "$sum"
echo "levels:$lv"
`

func TestTrainLandRequiresACompletedGate(t *testing.T) {
	needTools(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	state := filepath.Join(tmp, "state")
	env := scriptEnv(t, state)
	remote := filepath.Join(tmp, "o.git")
	mustRun(t, tmp, env, "git", "init", "-q", "--bare", "-b", "main", remote)
	w := filepath.Join(tmp, "w")
	mustRun(t, tmp, env, "git", "clone", "-q", remote, w)
	for _, name := range []string{"lib.sh", "benchlock.sh", "testslot.sh", "run-gate.sh", "train.sh"} {
		b, err := os.ReadFile(filepath.Join(root, ".agents", "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(w, ".agents", "scripts", name), string(b), 0o755)
	}
	b, err := os.ReadFile(filepath.Join(root, "hack", "check-commits.sh"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(w, "hack", "check-commits.sh"), string(b), 0o755)
	write(t, filepath.Join(w, "hack", "gate.sh"), stubGate, 0o755)
	mustRun(t, w, env, "git", "checkout", "-q", "-b", "main")
	mustRun(t, w, env, "git", "add", ".")
	mustRun(t, w, env, "git", "commit", "-q", "-m", "Seed", "-m", "Initial.")
	mustRun(t, w, env, "git", "push", "-q", "origin", "main")

	branch := func(name, file string) {
		mustRun(t, w, env, "git", "checkout", "-q", "-b", name, "main")
		write(t, filepath.Join(w, file), name, 0o644)
		mustRun(t, w, env, "git", "add", file)
		mustRun(t, w, env, "git", "commit", "-q", "-m", "Add "+file, "-m", "The test needs it.")
	}
	branch("feat/code", "internal/engine/x.go")
	branch("feat/doc", "docs/a.md")
	train := filepath.Join(w, ".agents", "scripts", "train.sh")
	step := func(wantOK bool, genv []string, args ...string) string {
		t.Helper()
		out, err := runIn(t, w, append(env, genv...), "bash", append([]string{train}, args...)...)
		if (err == nil) != wantOK {
			t.Fatalf("train.sh %v: err=%v, want ok=%v\n%s", args, err, wantOK, out)
		}
		return out
	}

	step(true, nil, "stack", "c1", "feat/code")
	// A gate that was killed after a passing step leaves a partial all-pass summary: no landing.
	step(false, []string{"GATE_MODE=kill"}, "gate", "c1")
	if out := step(false, nil, "land", "c1"); !strings.Contains(out, "no completed gate") {
		t.Errorf("land after a killed gate: %s", out)
	}
	// A docs gate on a stack with code is not enough.
	step(true, nil, "gate", "c1", "docs")
	if out := step(false, nil, "land", "c1"); !strings.Contains(out, "needs the gate level engine") {
		t.Errorf("land after a docs gate on code: %s", out)
	}
	// An engine path needs engine and full: the default levels cover them.
	step(true, nil, "gate", "c1", "engine")
	if out := step(false, nil, "land", "c1"); !strings.Contains(out, "needs the gate level full") {
		t.Errorf("land after an engine gate on engine paths: %s", out)
	}
	if out := step(true, nil, "gate", "c1"); !strings.Contains(out, "engine full") {
		t.Errorf("default levels of an engine stack: %s", out)
	}
	step(true, nil, "land", "c1")
	if got := mustRun(t, w, env, "git", "--git-dir", remote, "log", "--format=%s", "main"); !strings.Contains(got, "Add internal/engine/x.go") {
		t.Errorf("main does not hold the landed commit:\n%s", got)
	}

	// A stack of documentation needs only docs.
	step(true, nil, "stack", "d1", "feat/doc")
	if out := step(true, nil, "gate", "d1"); !strings.Contains(out, "gated") || strings.Contains(out, "engine") {
		t.Errorf("default levels of a docs stack: %s", out)
	}
	step(true, nil, "land", "d1")
}
