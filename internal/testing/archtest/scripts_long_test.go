//go:build sxir_long

package archtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"syntaxir.dev/sxir/internal/testing/mutanttest"
)

// scriptsDeps are what the tests of the queue scripts depend on: the scripts and the test package (the harness
// helpers live in its other files). The gate skips them when none changed since its base (see mutanttest).
var scriptsDeps = []string{".agents/scripts", "internal/testing/archtest"}

// The queue scripts' races and signal handling, run against temporary state directories. They
// take minutes, so they belong to the long tier.

func scriptPath(t *testing.T, name string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, ".agents", "scripts", name)
}

func procStart(t *testing.T, pid int) string {
	t.Helper()
	out := mustRun(t, ".", os.Environ(), "ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
	return strings.TrimSpace(out)
}

// rendezvousTools puts a find and a cat first on the PATH that wait until two claimers have called them
// with the same arguments (or for 1.5 seconds): the find that judges a mutex stale by its age, and the
// cat that reads the pid of a slot. The scripts judge a holder dead from what they read and act on it
// afterwards, so two claimers that are not serialized both judge the same dead holder before either
// acts, and each then takes the slot. Claimers that are serialized wait for the full time, one at a time.
func rendezvousTools(t *testing.T, env []string, dir string) []string {
	t.Helper()
	bin := t.TempDir()
	for _, tool := range []string{"find", "cat"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s is not installed", tool)
		}
		write(t, filepath.Join(bin, tool), `#!/bin/sh
case "$*" in *-maxdepth*|*/pid)
  d=`+dir+`/$(echo "$*" | tr -c 'a-z0-9' _)
  mkdir -p "$d"; : > "$d/$$"; i=0
  while [ "$(ls "$d" | wc -l)" -lt 2 ] && [ "$i" -lt 30 ]; do sleep 0.05; i=$((i + 1)); done ;;
esac
exec `+real+` "$@"
`, 0o755)
	}
	return append(env, "PATH="+bin+":"+os.Getenv("PATH"))
}

// A dead mutex and dead slots left by killed holders, with many waiters: reclaiming must be one
// step, so no more than TESTSLOT_N commands run at once.
func TestTestslotReclaimDoesNotAdmitExtraHolders(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := append(scriptEnv(t, state), "TESTSLOT_N=2")
	slots := filepath.Join(state, "slots")
	write(t, filepath.Join(slots, ".mutex", "owner"), "99999 Mon Jan  1 00:00:00 2001\n", 0o644)
	for _, i := range []string{"1", "2"} {
		write(t, filepath.Join(slots, i, "pid"), "99998\n", 0o644)
	}
	run := t.TempDir()
	work := filepath.Join(run, "work.sh")
	write(t, work, `#!/usr/bin/env bash
echo $$ > "$1/live.$$"
sleep 0.4
ls "$1" | grep -c '^live\.' >> "$1/counts"
rm -f "$1/live.$$"
`, 0o755)
	var cmds []*exec.Cmd
	for i := 0; i < 8; i++ {
		cmd := exec.Command("bash", scriptPath(t, "testslot.sh"), work, run)
		cmd.Env = env
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := os.ReadFile(filepath.Join(run, "counts"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(counts))
	if len(lines) != 8 {
		t.Fatalf("%d of 8 commands ran: %q", len(lines), lines)
	}
	for _, l := range lines {
		if c, _ := strconv.Atoi(l); c > 2 {
			t.Errorf("%s commands ran at once with 2 slots: %q", l, lines)
		}
	}
}

// Two claimers that judge the same dead mutex and slot together must not both take the slot: the claim
// is one step, serialized between the scripts.
func TestTestslotClaimIsSerialized(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	for round := 0; round < 3; round++ {
		state := t.TempDir()
		rdv := t.TempDir()
		env := rendezvousTools(t, append(scriptEnv(t, state), "TESTSLOT_N=1"), rdv)
		slots := filepath.Join(state, "slots")
		// A mutex left without an owner is judged stale by its age.
		mutex := filepath.Join(slots, ".mutex")
		if err := os.MkdirAll(mutex, 0o755); err != nil {
			t.Fatal(err)
		}
		past := time.Now().Add(-3 * time.Minute)
		if err := os.Chtimes(mutex, past, past); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(slots, "1", "pid"), "99998\n", 0o644)
		run := t.TempDir()
		work := filepath.Join(run, "work.sh")
		write(t, work, `#!/usr/bin/env bash
echo $$ > "$1/live.$$"
sleep 1
ls "$1" | grep -c '^live\.' >> "$1/counts"
rm -f "$1/live.$$"
`, 0o755)
		var cmds []*exec.Cmd
		for i := 0; i < 2; i++ {
			cmd := exec.Command("bash", scriptPath(t, "testslot.sh"), work, run)
			cmd.Env = env
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			cmds = append(cmds, cmd)
		}
		for _, cmd := range cmds {
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
		}
		counts, _ := os.ReadFile(filepath.Join(run, "counts"))
		lines := strings.Fields(string(counts))
		if len(lines) != 2 {
			t.Fatalf("round %d: %d of 2 commands ran: %q", round, len(lines), lines)
		}
		for _, l := range lines {
			if c, _ := strconv.Atoi(l); c > 1 {
				t.Errorf("round %d: %s commands ran at once with 1 slot: %q", round, l, lines)
			}
		}
	}
}

// stagedTools puts a find and a cat first on the PATH that stage two claimers, a and b, through the
// race that only the flock closes. Both judge the dead .mutex stale; a acts on it and takes the
// mutex; b, delayed after its check, removes a's live mutex and takes it too, so both are inside.
// a reads slot 1 (dead holder) and waits until b has read it as well; b then waits until a has
// claimed slot 1 and acts on its stale read: it removes a's slot and claims it. Each wait gives up
// after three seconds, so claimers that are serialized pass the stage one at a time.
func stagedTools(t *testing.T, env []string, dir, role string) []string {
	t.Helper()
	bin := t.TempDir()
	wait := `i=0; until COND || [ "$i" -ge 60 ]; do sleep 0.05; i=$((i + 1)); done`
	for _, tool := range []string{"find", "cat"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s is not installed", tool)
		}
		var body string
		switch role + tool {
		case "afind": // a judges the mutex only after b has judged it.
			body = `*.mutex\ -maxdepth*) : > ` + dir + `/a.mutex; ` + strings.Replace(wait, "COND", `[ -e `+dir+`/b.mutex ]`, 1) + ` ;;`
		case "bfind": // b judges the mutex, then acts only once a holds it.
			body = `*.mutex\ -maxdepth*) out=$(` + real + ` "$@"); : > ` + dir + `/b.mutex; ` +
				strings.Replace(wait, "COND", `[ -s "$1/owner" ]`, 1) + `; [ -z "$out" ] || echo "$out"; exit 0 ;;`
		case "acat": // a reads slot 1, then acts only once b has read it too.
			body = `*/1/pid) out=$(` + real + ` "$@"); : > ` + dir + `/a.slot; ` +
				strings.Replace(wait, "COND", `[ -e `+dir+`/b.slot ]`, 1) + `; echo "$out"; exit 0 ;;`
		case "bcat": // b reads slot 1, then acts only once a has claimed it.
			body = `*/1/pid) out=$(` + real + ` "$@"); : > ` + dir + `/b.slot; ` +
				strings.Replace(wait, "COND", `[ -s "$1" ] && [ "$(`+real+` "$1")" != 99998 ]`, 1) + `; echo "$out"; exit 0 ;;`
		}
		write(t, filepath.Join(bin, tool), "#!/bin/sh\ncase \"$*\" in\n"+body+"\nesac\nexec "+real+" \"$@\"\n", 0o755)
	}
	return append(env, "PATH="+bin+":"+os.Getenv("PATH"))
}

// Two claimers that judge the same dead mutex stale must not both get inside it and both take one
// slot. The order is forced (see stagedTools), so a script without the flock that serializes the
// claim runs two commands in its one slot every time.
func TestTestslotStaleMutexRaceNeedsTheFlock(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	stage := t.TempDir()
	slots := filepath.Join(state, "slots")
	mutex := filepath.Join(slots, ".mutex")
	if err := os.MkdirAll(mutex, 0o755); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-3 * time.Minute)
	if err := os.Chtimes(mutex, past, past); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(slots, "1", "pid"), "99998\n", 0o644)
	run := t.TempDir()
	work := filepath.Join(run, "work.sh")
	write(t, work, `#!/usr/bin/env bash
echo $$ > "$1/live.$$"
sleep 1
ls "$1" | grep -c '^live\.' >> "$1/counts"
rm -f "$1/live.$$"
`, 0o755)
	var cmds []*exec.Cmd
	for _, role := range []string{"a", "b"} {
		cmd := exec.Command("bash", scriptPath(t, "testslot.sh"), work, run)
		cmd.Env = stagedTools(t, append(scriptEnv(t, state), "TESTSLOT_N=1"), stage, role)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	counts, _ := os.ReadFile(filepath.Join(run, "counts"))
	lines := strings.Fields(string(counts))
	if len(lines) != 2 {
		t.Fatalf("%d of 2 commands ran: %q", len(lines), lines)
	}
	for _, l := range lines {
		if c, _ := strconv.Atoi(l); c > 1 {
			t.Errorf("%s commands ran at once with 1 slot: %q", l, lines)
		}
	}
}

// blockWait is how long a claim may take before it counts as blocked: 4 seconds when the claim
// should block, so that the test does not wait long for a claim that never comes, and 60 seconds
// when it should go through, so that a claim slowed by a heavily loaded machine is not taken for
// a blocked one.
func blockWait(blocked bool) time.Duration {
	if blocked {
		return 4 * time.Second
	}
	return 60 * time.Second
}

// A live pid with another start time is a reused pid, not the holder; the same start time is.
func TestTestslotReusedPid(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	live := os.Getpid()
	pid := strconv.Itoa(live)
	for _, c := range []struct {
		name, start string
		blocked     bool
	}{
		{"reused", pid + " Mon Jan  1 00:00:00 2001", false},
		{"holder", pid + " " + procStart(t, live), true},
	} {
		for _, where := range []string{"slot", "mutex"} {
			state := t.TempDir()
			env := append(scriptEnv(t, state), "TESTSLOT_N=1")
			slots := filepath.Join(state, "slots")
			if where == "slot" {
				write(t, filepath.Join(slots, "1", "pid"), pid+"\n", 0o644)
				write(t, filepath.Join(slots, "1.start"), c.start+"\n", 0o644)
			} else {
				write(t, filepath.Join(slots, ".mutex", "owner"), c.start+"\n", 0o644)
			}
			ctx, cancel := context.WithTimeout(context.Background(), blockWait(c.blocked))
			cmd := exec.CommandContext(ctx, "bash", scriptPath(t, "testslot.sh"), "true")
			cmd.Env = env
			err := cmd.Run()
			timedOut := ctx.Err() != nil
			cancel()
			if c.blocked != timedOut {
				t.Errorf("%s %s: blocked=%v, want %v (err %v)", c.name, where, timedOut, c.blocked, err)
			}
		}
	}
}

// lingering lists the processes whose command line holds the marker: the command, and copies of
// the script, which may still be on their way to start it or only leaving.
func lingering(marker string) string {
	out, _ := exec.Command("pgrep", "-f", "sleep "+marker).Output()
	return strings.TrimSpace(string(out))
}

// commandPid is the pid that the command wrote to pidFile, if it started, and whether that
// process is still alive and still the command: its command line holds the marker, which a reused
// pid's does not.
func commandPid(pidFile, marker string) (pid string, alive bool) {
	b, _ := os.ReadFile(pidFile)
	pid = strings.TrimSpace(string(b))
	if pid == "" {
		return "", false
	}
	out, err := exec.Command("ps", "-o", "args=", "-p", pid).Output()
	return pid, err == nil && strings.Contains(string(out), marker)
}

// commandRunning reports whether a process runs exactly "sleep marker": the command itself, not a
// copy of the script whose command line holds it.
func commandRunning(marker string) bool {
	out, _ := exec.Command("ps", "-axo", "args=").Output()
	for _, l := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(l) == "sleep "+marker {
			return true
		}
	}
	return false
}

// startLatency is how long the script takes to start a command, so that the TERMs of the test are
// spread over the window in which it starts and not over a window that the script has passed or not
// reached: a script that takes 400 ms to start under load is not startled by a TERM after 200 ms.
func startLatency(t *testing.T, name string, args []string) time.Duration {
	t.Helper()
	var worst time.Duration
	for k := 0; k < 3; k++ {
		marker := fmt.Sprintf("%d.%03d", 30+k, len(name))
		cmd := exec.Command("bash", append(append([]string{}, args...), "sleep", marker)...)
		cmd.Env = scriptEnv(t, t.TempDir())
		begin := time.Now()
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		for !commandRunning(marker) && time.Since(begin) < 30*time.Second {
			time.Sleep(2 * time.Millisecond)
		}
		worst = max(worst, time.Since(begin))
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
	}
	return worst
}

// A TERM that arrives while the command starts must not leave the command running. The command
// writes its own pid and then becomes a sleep of 40 seconds or more, so a command that was missed
// outlives the wait below. Only that pid counts: a copy of the script that is only leaving, or a
// subshell of it that a loaded machine delays, also holds the marker in its command line.
func TestQueueScriptsTermDuringStart(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	for _, name := range []string{"testslot.sh", "benchlock.sh"} {
		base := []string{scriptPath(t, name)}
		if name == "benchlock.sh" {
			base = append(base, "t")
		}
		latency := startLatency(t, name, base)
		for i := 0; i < 70; i++ {
			state := t.TempDir()
			env := scriptEnv(t, state)
			marker := fmt.Sprintf("%d.%03d", 40+len(name), i)
			pidFile := filepath.Join(state, "command.pid")
			args := append(append([]string{}, base...), "bash", "-c", `echo $$ > "$0"; exec sleep `+marker, pidFile)
			cmd := exec.Command("bash", args...)
			cmd.Env = env
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			// From a fifth of the start latency to one and a half times it.
			delay := latency * time.Duration(20+i*130/69) / 100
			time.Sleep(delay)
			_ = cmd.Process.Signal(syscall.SIGTERM)
			// The script stops its command and exits promptly; a script that cannot signal the
			// command's group keeps waiting for it for the 40 seconds that it sleeps.
			exited := make(chan struct{})
			go func() { _ = cmd.Wait(); close(exited) }()
			select {
			case <-exited:
			case <-time.After(10 * time.Second):
				_ = exec.Command("pkill", "-f", "sleep "+marker).Run()
				_ = cmd.Process.Kill()
				<-exited
				t.Fatalf("%s: still running 10 s after a TERM sent after %v", name, delay)
			}
			// Wait until the command that started is gone, or until nothing that could still start
			// it is left; give up after 5 s.
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if pid, alive := commandPid(pidFile, marker); pid != "" && !alive || pid == "" && lingering(marker) == "" {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if pid, alive := commandPid(pidFile, marker); alive {
				ps, _ := exec.Command("ps", "-o", "pid,ppid,pgid,stat,args", "-p", pid).Output()
				_ = exec.Command("pkill", "-f", "sleep "+marker).Run()
				t.Fatalf("%s: the command survived a TERM sent after %v and 5 s of waiting:\n%s", name, delay, ps)
			}
		}
	}
}

// The command's standard input is /dev/null: job control does not redirect it, and a queued
// command must not read the pipe or the terminal of its caller.
func TestQueueScriptsCommandStdinIsNull(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	for _, name := range []string{"testslot.sh", "benchlock.sh"} {
		args := []string{scriptPath(t, name)}
		if name == "benchlock.sh" {
			args = append(args, "t")
		}
		cmd := exec.Command("bash", append(args, "cat")...)
		cmd.Env = scriptEnv(t, t.TempDir())
		cmd.Stdin = strings.NewReader("from the caller\n")
		out, err := cmd.Output()
		if err != nil || len(out) != 0 {
			t.Errorf("%s: cat printed %q (err %v), want nothing: its input must be /dev/null", name, out, err)
		}
	}
}

// Without perl, which the scripts need for their locks, they stop with a message instead of waiting
// for a lock that they can never take.
func TestQueueScriptsWithoutPerl(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	bin := t.TempDir()
	for _, tool := range []string{"bash", "dirname"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s is not installed", tool)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"testslot.sh", "benchlock.sh"} {
		args := []string{scriptPath(t, name)}
		if name == "benchlock.sh" {
			args = append(args, "t")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, filepath.Join(bin, "bash"), append(args, "true")...)
		cmd.Env = append(scriptEnv(t, t.TempDir()), "PATH="+bin)
		out, err := cmd.CombinedOutput()
		timedOut := ctx.Err() != nil
		cancel()
		if timedOut || err == nil || !strings.Contains(string(out), "perl is required") {
			t.Errorf("%s without perl: timed out %v, err %v, output %q, want a failure that says perl is required", name, timedOut, err, out)
		}
	}
}

// A claimer killed between making a slot and writing its pid leaves a slot without a pid. It is
// taken back once it is older than a minute, and not before: its claimer may still be writing.
func TestTestslotReclaimsEmptySlot(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	for _, c := range []struct {
		name    string
		age     time.Duration
		blocked bool
	}{{"fresh", 0, true}, {"old", 3 * time.Minute, false}} {
		state := t.TempDir()
		env := append(scriptEnv(t, state), "TESTSLOT_N=1")
		slot := filepath.Join(state, "slots", "1")
		if err := os.MkdirAll(slot, 0o755); err != nil {
			t.Fatal(err)
		}
		past := time.Now().Add(-c.age)
		if err := os.Chtimes(slot, past, past); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), blockWait(c.blocked))
		cmd := exec.CommandContext(ctx, "bash", scriptPath(t, "testslot.sh"), "true")
		cmd.Env = env
		err := cmd.Run()
		timedOut := ctx.Err() != nil
		cancel()
		if c.blocked != timedOut {
			t.Errorf("%s empty slot: blocked=%v, want %v (err %v)", c.name, timedOut, c.blocked, err)
		}
	}
}

// mainScripts copies the queue scripts of the revision before the race fixes, kept in testdata,
// to a directory of their own, so that they run as an earlier checkout's copy runs beside these.
func mainScripts(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"lib.sh", "testslot.sh", "benchlock.sh"} {
		b, err := os.ReadFile(filepath.Join("testdata", "queue-scripts-f454c9f3e", name))
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(dir, name), string(b), 0o755)
	}
	return dir
}

// overlapWork is a command "work.sh RUN [NAME [SECONDS]]" that records how many copies of it run
// at once in RUN/counts, and its start and end under NAME in RUN/events. It runs 2 seconds. With
// WORK_RENDEZVOUS=N in its environment it first waits, for at most 60 seconds, until N copies run,
// and then runs its 2 seconds: on a loaded machine the copies that a queue admits together can start
// seconds apart, and a fixed run would end before the next one starts, so that copies the queue let
// run at once would look serialized. The rendezvous only waits: copies that the queue keeps apart
// still never run at once, and more copies than N that it lets in still show in the counts. With
// WORK_HOLD=FILE it then waits until FILE exists, so that a test decides when it ends.
func overlapWork(t *testing.T) (work, run string) {
	t.Helper()
	run = t.TempDir()
	work = filepath.Join(run, "work.sh")
	write(t, work, `#!/usr/bin/env bash
echo "start ${2:-}" >> "$1/events"
echo $$ > "$1/live.$$"
if [ -n "${WORK_RENDEZVOUS:-}" ]; then
	n=0
	while [ "$(ls "$1" | grep -c '^live\.')" -lt "$WORK_RENDEZVOUS" ] && [ "$n" -lt 600 ]; do n=$((n + 1)); sleep 0.1; done
fi
sleep "${3:-2}"
while [ -n "${WORK_HOLD:-}" ] && [ ! -e "$WORK_HOLD" ]; do sleep 0.05; done
ls "$1" | grep -c '^live\.' >> "$1/counts"
rm -f "$1/live.$$"
echo "end ${2:-}" >> "$1/events"
`, 0o755)
	return work, run
}

// startQueued starts a queue script on the work command and returns once the command runs.
func startQueued(t *testing.T, env []string, run string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("bash", args...)
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for begin := time.Now(); time.Since(begin) < 20*time.Second; time.Sleep(20 * time.Millisecond) {
		if m, _ := filepath.Glob(filepath.Join(run, "live.*")); len(m) > 0 {
			return cmd
		}
	}
	_ = cmd.Process.Kill()
	t.Fatalf("%v: the command did not start", args)
	return nil
}

// waitOverlap waits for the commands and fails when they did not all run or ran together.
func waitOverlap(t *testing.T, what, run string, want int, cmds ...*exec.Cmd) {
	t.Helper()
	waitConcurrency(t, what, run, want, 1, cmds...)
}

// waitConcurrency waits for the commands and fails when they did not all run, or when the most
// copies that ran at once is not most.
func waitConcurrency(t *testing.T, what, run string, want, most int, cmds ...*exec.Cmd) {
	t.Helper()
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	counts, _ := os.ReadFile(filepath.Join(run, "counts"))
	lines := strings.Fields(string(counts))
	if len(lines) != want {
		t.Fatalf("%s: %d of %d commands ran: %q", what, len(lines), want, lines)
	}
	seen := 0
	for _, l := range lines {
		c, _ := strconv.Atoi(l)
		seen = max(seen, c)
	}
	if seen != most {
		t.Errorf("%s: at most %d commands ran at once, want %d: %q", what, seen, most, lines)
	}
}

// A live holder started by an earlier checkout's script excludes a claimer of the current one, and
// the other way round, for the test slots and the heavy-run lock: lanes on older branches share the
// queue with the current scripts.
func TestQueueScriptsBesideEarlierVersion(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	old := mainScripts(t)
	for _, name := range []string{"testslot.sh", "benchlock.sh"} {
		for _, holderOld := range []bool{true, false} {
			state := t.TempDir()
			env := append(scriptEnv(t, state), "TESTSLOT_N=1")
			work, run := overlapWork(t)
			args := func(dir, tag string) []string {
				a := []string{filepath.Join(dir, name)}
				if name == "benchlock.sh" {
					a = append(a, tag)
				}
				return append(a, work, run)
			}
			cur := filepath.Dir(scriptPath(t, name))
			holder, claimer := args(cur, "cur"), args(old, "old")
			if holderOld {
				holder, claimer = args(old, "old"), args(cur, "cur")
			}
			h := startQueued(t, env, run, holder...)
			c := exec.Command("bash", claimer...)
			c.Env = env
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			waitOverlap(t, fmt.Sprintf("%s, earlier holder %v", name, holderOld), run, 2, h, c)
		}
	}
}

// benchlockClass runs the current benchlock.sh in a class on the work command.
func benchlockClass(t *testing.T, env []string, class, tag string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("bash", append([]string{scriptPath(t, "benchlock.sh"), tag}, args...)...)
	cmd.Env = append(append([]string{}, env...), "BENCHLOCK_CLASS="+class)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

// waitQueued returns once a queued record of the tag exists.
func waitQueued(t *testing.T, state, tag string) {
	t.Helper()
	for begin := time.Now(); time.Since(begin) < 20*time.Second; time.Sleep(20 * time.Millisecond) {
		m, _ := filepath.Glob(filepath.Join(state, "runs", tag+".*"))
		for _, f := range m {
			if b, _ := os.ReadFile(f); strings.HasPrefix(string(b), "state=queued") {
				return
			}
		}
	}
	t.Fatalf("no queued record of %s", tag)
}

// events reads the start and end events of the work commands, in order.
func events(t *testing.T, run string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(run, "events"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// before reports whether event a comes before event b.
func before(ev []string, a, b string) bool {
	ia, ib := -1, -1
	for i, e := range ev {
		if e == a && ia < 0 {
			ia = i
		}
		if e == b && ib < 0 {
			ib = i
		}
	}
	return ia >= 0 && ib >= 0 && ia < ib
}

// Shared jobs run K at a time, and no more: four of them with K = 2 run two by two. The jobs wait
// for a second one to run (WORK_RENDEZVOUS), so that two admitted together overlap however loaded
// the machine is; a queue that admitted them one at a time leaves each alone after the wait.
func TestBenchlockSharedSlots(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := append(scriptEnv(t, state), "BENCHLOCK_SHARED=2", "WORK_RENDEZVOUS=2")
	work, run := overlapWork(t)
	var cmds []*exec.Cmd
	for i := 0; i < 4; i++ {
		cmds = append(cmds, benchlockClass(t, env, "shared", fmt.Sprintf("s%d", i), work, run))
	}
	waitConcurrency(t, "four shared jobs, K = 2", run, 4, 2, cmds...)
}

// An exclusive job waits for the shared holders, and a shared job waits for an exclusive holder.
func TestBenchlockExclusiveAndSharedExclude(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	for _, first := range []string{"shared", "exclusive"} {
		state := t.TempDir()
		env := append(scriptEnv(t, state), "BENCHLOCK_SHARED=2")
		work, run := overlapWork(t)
		second := map[string]string{"shared": "exclusive", "exclusive": "shared"}[first]
		h := benchlockClass(t, env, first, "h", work, run, "h")
		for begin := time.Now(); ; time.Sleep(20 * time.Millisecond) {
			if m, _ := filepath.Glob(filepath.Join(run, "live.*")); len(m) > 0 {
				break
			}
			if time.Since(begin) > 20*time.Second {
				t.Fatalf("%s holder did not start", first)
			}
		}
		c := benchlockClass(t, env, second, "c", work, run, "c")
		waitOverlap(t, first+" holder, then "+second, run, 2, h, c)
	}
}

// A queued exclusive job blocks the shared jobs queued after it, though slots are free (K = 3 leaves
// one even when the exclusive job is counted as taking one), so a stream
// of shared jobs cannot starve it. A record without class=, written by an earlier version, ranks
// as an exclusive one.
func TestBenchlockExclusiveIsNotStarved(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	for _, waiter := range []string{"current", "earlier"} {
		state := t.TempDir()
		env := append(scriptEnv(t, state), "BENCHLOCK_SHARED=3")
		work, run := overlapWork(t)
		h := benchlockClass(t, env, "shared", "s1", work, run, "s1", "3")
		for begin := time.Now(); ; time.Sleep(20 * time.Millisecond) {
			if m, _ := filepath.Glob(filepath.Join(run, "live.*")); len(m) > 0 {
				break
			}
			if time.Since(begin) > 20*time.Second {
				t.Fatal("shared holder did not start")
			}
		}
		var x *exec.Cmd
		if waiter == "current" {
			x = benchlockClass(t, env, "exclusive", "x", work, run, "x", "1")
		} else {
			x = exec.Command("bash", filepath.Join(mainScripts(t), "benchlock.sh"), "x", work, run, "x", "1")
			x.Env = env
			if err := x.Start(); err != nil {
				t.Fatal(err)
			}
		}
		waitQueued(t, state, "x")
		s2 := benchlockClass(t, env, "shared", "s2", work, run, "s2", "1")
		for _, cmd := range []*exec.Cmd{h, x, s2} {
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
		}
		ev := events(t, run)
		if !before(ev, "end s1", "start x") || !before(ev, "end x", "start s2") {
			t.Errorf("%s exclusive waiter: events %q, want s1, then x, then s2", waiter, ev)
		}
	}
}

// Records and holders of the earlier version and of the current shared class share one queue: an
// earlier holder excludes a shared job, and an earlier waiter waits for the shared holders, which
// leave benchlock/ without an owner that it could take over.
func TestBenchlockSharedBesideEarlierVersion(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	old := filepath.Join(mainScripts(t), "benchlock.sh")
	for _, holderOld := range []bool{true, false} {
		state := t.TempDir()
		env := append(scriptEnv(t, state), "BENCHLOCK_SHARED=2")
		work, run := overlapWork(t)
		var h, c *exec.Cmd
		if holderOld {
			h = startQueued(t, env, run, old, "old", work, run)
			c = benchlockClass(t, env, "shared", "cur", work, run)
		} else {
			h = benchlockClass(t, env, "shared", "cur", work, run)
			for begin := time.Now(); ; time.Sleep(20 * time.Millisecond) {
				if m, _ := filepath.Glob(filepath.Join(run, "live.*")); len(m) > 0 {
					break
				}
				if time.Since(begin) > 20*time.Second {
					t.Fatal("shared holder did not start")
				}
			}
			c = exec.Command("bash", old, "old", work, run)
			c.Env = env
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
		}
		waitOverlap(t, fmt.Sprintf("shared beside earlier version, earlier holder %v", holderOld), run, 2, h, c)
	}
}

// A shared holder killed without releasing its slot is taken back, and the next job runs.
func TestBenchlockReclaimsDeadSharedHolder(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := append(scriptEnv(t, state), "BENCHLOCK_SHARED=1")
	work, run := overlapWork(t)
	crashHolder(t, env, state, "shared", "dead")
	for _, class := range []string{"exclusive", "shared"} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		cmd := exec.CommandContext(ctx, "bash", scriptPath(t, "benchlock.sh"), "next", work, run, class, "0")
		cmd.Env = append(append([]string{}, env...), "BENCHLOCK_CLASS="+class)
		err := cmd.Run()
		cancel()
		if err != nil {
			t.Fatalf("%s job after a dead shared holder: %v", class, err)
		}
	}
}

// queue-status.sh names the lock's class and each record's.
func TestQueueStatusShowsClass(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := append(scriptEnv(t, state), "BENCHLOCK_SHARED=2")
	work, run := overlapWork(t)
	h := benchlockClass(t, env, "shared", "s", work, run, "s", "3")
	for begin := time.Now(); ; time.Sleep(20 * time.Millisecond) {
		if m, _ := filepath.Glob(filepath.Join(run, "live.*")); len(m) > 0 {
			break
		}
		if time.Since(begin) > 20*time.Second {
			t.Fatal("shared holder did not start")
		}
	}
	x := benchlockClass(t, env, "exclusive", "x", work, run, "x", "0")
	waitQueued(t, state, "x")
	out, _ := runIn(t, ".", env, "bash", scriptPath(t, "queue-status.sh"))
	for _, want := range []string{"lock: shared, 1 of 2 slots: s ", " shared\tstate=running", " exclusive\tstate=queued"} {
		if !strings.Contains(out, want) {
			t.Errorf("queue-status.sh output lacks %q:\n%s", want, out)
		}
	}
	_ = h.Wait()
	_ = x.Wait()
}

// A stray file in benchlock/ keeps the lock from being removed: the script keeps its command's
// exit status and reports the stray file in the stall log, which queue-status.sh shows.
func TestBenchlockReleaseKeepsStatusBesideStrayFile(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	for _, class := range []string{"exclusive", "shared"} {
		state := t.TempDir()
		cmd := exec.Command("bash", scriptPath(t, "benchlock.sh"), "stray", "bash", "-c", `: > "$1/benchlock/stray"; exit 3`, "_", state)
		cmd.Env = append(scriptEnv(t, state), "BENCHLOCK_CLASS="+class)
		out, err := cmd.CombinedOutput()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 3 {
			t.Errorf("%s: exit %v, want the command's 3; output %s", class, err, out)
		}
		log, _ := os.ReadFile(filepath.Join(state, "benchlock-stalls.log"))
		if !strings.Contains(string(log), "stray: stray.") || !strings.Contains(string(log), "holds: stray") {
			t.Errorf("%s: stall log %q does not report the stray file", class, log)
		}
	}
}

// crashHolder starts a holder of the class under tag and kills it as a crash would: the wrapper
// alone, with SIGKILL, so that it leaves its hold and its running record; then the group of its
// command, which this test started, as the running record names it.
func crashHolder(t *testing.T, env []string, state, class, tag string) {
	t.Helper()
	h := benchlockClass(t, env, class, tag, "sleep", "60")
	pgid := 0
	for begin := time.Now(); pgid == 0; time.Sleep(20 * time.Millisecond) {
		recs, _ := filepath.Glob(filepath.Join(state, "runs", tag+".*"))
		for _, r := range recs {
			b, _ := os.ReadFile(r)
			for _, f := range strings.Fields(string(b)) {
				if v, ok := strings.CutPrefix(f, "pgid="); ok {
					pgid, _ = strconv.Atoi(v)
				}
			}
		}
		if time.Since(begin) > 20*time.Second {
			_ = h.Process.Kill()
			t.Fatalf("%s holder did not start", class)
		}
	}
	if pgid <= 1 {
		t.Fatalf("bad group %d", pgid)
	}
	_ = h.Process.Kill()
	_ = h.Wait()
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

// A shared holder killed with SIGKILL while only waiters of the earlier version are queued: they
// cannot take its slot back, so queue-status.sh must report the dead holder and its record as
// stale and exit 1. The next current benchlock.sh takes the slot back and the queue moves.
func TestQueueStatusReportsDeadSharedHolder(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := append(scriptEnv(t, state), "BENCHLOCK_SHARED=2")
	crashHolder(t, env, state, "shared", "dead")
	work, run := overlapWork(t)
	old := exec.Command("bash", filepath.Join(mainScripts(t), "benchlock.sh"), "old", work, run, "old", "0")
	old.Env = env
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = old.Process.Kill() }()
	waitQueued(t, state, "old")
	time.Sleep(2 * time.Second) // several polls of the earlier waiter
	out, err := runIn(t, ".", env, "bash", scriptPath(t, "queue-status.sh"))
	if err == nil {
		t.Errorf("queue-status.sh exited 0 with a dead shared holder:\n%s", out)
	}
	for _, want := range []string{"STALE: shared holder dead ", "STALE: running record dead.", "STUCK", "queued: 1 running: 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("queue-status.sh output lacks %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(run, "events")); err == nil {
		t.Fatal("the earlier waiter ran while the dead holder's slot was held")
	}
	// A current job takes the slot back; then the earlier waiter runs.
	cur := exec.Command("bash", scriptPath(t, "benchlock.sh"), "cur", work, run, "cur", "0")
	cur.Env = env
	if out, err := cur.CombinedOutput(); err != nil {
		t.Fatalf("current job: %v\n%s", err, out)
	}
	if err := old.Wait(); err != nil {
		t.Fatalf("earlier waiter: %v", err)
	}
	out, err = runIn(t, ".", env, "bash", scriptPath(t, "queue-status.sh"))
	if err != nil || strings.Contains(out, "STALE") {
		t.Errorf("queue-status.sh after the queue moved: %v\n%s", err, out)
	}
}

// The backstop: a waiter of either class that sees the lock free for BENCHLOCK_STALL seconds while
// the ranking holds it back takes the lock and logs the stall. Here a live record that never takes
// the lock is ranked first: a process of a script named benchlock.sh that only sleeps.
func TestBenchlockStallBackstop(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	for _, class := range []string{"shared", "exclusive"} {
		state := t.TempDir()
		env := append(scriptEnv(t, state), "BENCHLOCK_STALL=3")
		fake := filepath.Join(t.TempDir(), "benchlock.sh")
		write(t, fake, "sleep \"$2\"\n", 0o755)
		blocker := exec.Command("bash", fake, "blocker", "60")
		if err := blocker.Start(); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(state, "runs", fmt.Sprintf("blocker.%d", blocker.Process.Pid)),
			fmt.Sprintf("state=queued seq=0 tag=blocker wrapper=%d cwd=/ start=2000-01-01T00:00:00 cmd=sleep 60\n", blocker.Process.Pid), 0o644)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, "bash", scriptPath(t, "benchlock.sh"), "late", "true")
		cmd.Env = append(append([]string{}, env...), "BENCHLOCK_CLASS="+class)
		out, err := cmd.CombinedOutput()
		cancel()
		_ = blocker.Process.Kill()
		_ = blocker.Wait()
		if err != nil {
			t.Fatalf("%s waiter behind a record that never runs: %v\n%s", class, err, out)
		}
		log, _ := os.ReadFile(filepath.Join(state, "benchlock-stalls.log"))
		if !strings.Contains(string(log), "stall: lock free") || !strings.Contains(string(log), class+") took it") {
			t.Errorf("%s: stall log %q does not record the stall", class, log)
		}
	}
}

// The slot count is shared by every caller of one state directory. A caller with another TESTSLOT_N
// would count slots that the others do not have: one with 4 beside callers with 1 sees slots 2 to 4
// free while slot 1 is held, and once the stall backstop fires it claims one ahead of the queue and runs
// beside the holder. Such a caller exits 2 before it queues instead. A caller without TESTSLOT_N takes
// the count of the state, 1 when the first caller set none.
func TestTestslotSharedCount(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := scriptEnv(t, state)
	work, run := overlapWork(t)
	hold := filepath.Join(run, "hold")
	holder := startQueued(t, append(env, "WORK_HOLD="+hold), run, scriptPath(t, "testslot.sh"), work, run, "h", "0.1")
	head := startScript(t, append(env, "TESTSLOT_N=1"), scriptPath(t, "testslot.sh"), work, run, "w", "0.1")
	waitRecords(t, state, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	other := exec.CommandContext(ctx, "bash", scriptPath(t, "testslot.sh"), work, run, "x", "0.1")
	other.Env = append(env, "TESTSLOT_N=4", "TESTSLOT_STALL=1")
	out, err := other.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(out), "differs from the slot count 1") {
		t.Errorf("a caller with TESTSLOT_N=4 beside the count 1: %v, %q; want exit 2 naming the shared count", err, out)
	}
	write(t, hold, "", 0o644)
	waitConcurrency(t, "shared count", run, 2, 1, holder, head)
	if ev := events(t, run); !before(ev, "end h", "start w") {
		t.Errorf("the head waiter did not follow the holder: %q", ev)
	}
	noStall(t, "shared count", state)
}

// A live holder in a slot past the count, which a caller of an earlier version that counted more slots
// leaves, takes its share of the count: a claim waits for it although slot 1 is free. The stall
// backstop does not grant a slot past the count either: the waiter is behind a live record and its
// backstop fires after a second, and still it waits.
func TestTestslotCountsHoldersPastTheCount(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := append(scriptEnv(t, state), "TESTSLOT_N=1", "TESTSLOT_STALL=1")
	frontRecord(t, state)
	pid := os.Getpid()
	slot := filepath.Join(state, "slots", "2")
	write(t, filepath.Join(slot, "pid"), fmt.Sprintf("%d\n", pid), 0o644)
	write(t, slot+".start", fmt.Sprintf("%d %s\n", pid, procStart(t, pid)), 0o644)
	work, run := overlapWork(t)
	c := startScript(t, env, scriptPath(t, "testslot.sh"), work, run, "w", "0.1")
	noneStarted(t, "a holder in slot 2", run, 3*time.Second)
	if err := os.RemoveAll(slot); err != nil {
		t.Fatal(err)
	}
	waitWithin(t, "after slot 2 was freed", c, 30*time.Second)
}

// waitRecords returns once the queue of testslot.sh holds n records of waiters.
func waitRecords(t *testing.T, state string, n int) {
	t.Helper()
	for begin := time.Now(); time.Since(begin) < 20*time.Second; time.Sleep(20 * time.Millisecond) {
		if m, _ := filepath.Glob(filepath.Join(state, "slots", "queue", "0*")); len(m) >= n {
			return
		}
	}
	t.Fatalf("fewer than %d waiter records", n)
}

// waitWithin waits for the command and fails when it does not end in time.
func waitWithin(t *testing.T, what string, cmd *exec.Cmd, d time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	case <-time.After(d):
		_ = cmd.Process.Kill()
		t.Fatalf("%s: did not run within %v", what, d)
	}
}

// Waiters of testslot.sh are admitted in the order they started: with one slot held, six waiters
// started in sequence run in that order. Admission by whoever polls first would reorder them.
func TestTestslotAdmitsInArrivalOrder(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := append(scriptEnv(t, state), "TESTSLOT_N=1")
	work, run := overlapWork(t)
	holder := startQueued(t, env, run, scriptPath(t, "testslot.sh"), work, run, "h", "3")
	var cmds []*exec.Cmd
	var names []string
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("w%d", i)
		names = append(names, name)
		c := exec.Command("bash", scriptPath(t, "testslot.sh"), work, run, name, "0.1")
		c.Env = env
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, c)
		waitRecords(t, state, i+1)
	}
	waitOverlap(t, "arrival order", run, 7, append([]*exec.Cmd{holder}, cmds...)...)
	ev := events(t, run)
	for i := 1; i < len(names); i++ {
		if !before(ev, "start "+names[i-1], "start "+names[i]) {
			t.Errorf("%s did not start before %s: %q", names[i-1], names[i], ev)
		}
	}
}

// A waiter that died while queued does not block the waiters behind it: neither a record whose pid
// is gone nor one whose pid now belongs to another process (a reused pid).
func TestTestslotDeadWaiterAheadDoesNotBlock(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	for name, rec := range map[string]string{
		"gone":   "99998 Thu Jan  1 00:00:00 2015\n",
		"reused": strconv.Itoa(os.Getpid()) + " Thu Jan  1 00:00:00 2015\n",
	} {
		state := t.TempDir()
		env := append(scriptEnv(t, state), "TESTSLOT_N=1")
		queue := filepath.Join(state, "slots", "queue")
		write(t, filepath.Join(queue, "000000000000."+strings.Fields(rec)[0]), rec, 0o644)
		work, run := overlapWork(t)
		c := exec.Command("bash", scriptPath(t, "testslot.sh"), work, run, "w", "0.1")
		c.Env = env
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		waitWithin(t, "dead waiter "+name, c, 30*time.Second)
		if m, _ := filepath.Glob(filepath.Join(queue, "0*")); len(m) != 0 {
			t.Errorf("dead waiter %s: records left: %v", name, m)
		}
	}
}

// With the queue wedged by a live waiter that never claims (a record of this very test, which is
// live and first), the stall backstop lets the waiter behind it take a free slot and logs it.
func TestTestslotStallBackstop(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := append(scriptEnv(t, state), "TESTSLOT_N=1", "TESTSLOT_STALL=3")
	queue := filepath.Join(state, "slots", "queue")
	pid := os.Getpid()
	write(t, filepath.Join(queue, "000000000000."+strconv.Itoa(pid)), fmt.Sprintf("%d %s\n", pid, procStart(t, pid)), 0o644)
	work, run := overlapWork(t)
	c := exec.Command("bash", scriptPath(t, "testslot.sh"), work, run, "w", "0.1")
	c.Env = env
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	waitWithin(t, "stalled queue", c, 30*time.Second)
	log, _ := os.ReadFile(filepath.Join(state, "testslot-stalls.log"))
	if !strings.Contains(string(log), "stall: slot free") {
		t.Errorf("stall not logged: %q", log)
	}
}

// queue-status.sh lists the testslot.sh waiters in rank order.
func TestQueueStatusListsSlotWaiters(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := append(scriptEnv(t, state), "TESTSLOT_N=1")
	work, run := overlapWork(t)
	holder := startQueued(t, env, run, scriptPath(t, "testslot.sh"), work, run, "h", "3")
	c := exec.Command("bash", scriptPath(t, "testslot.sh"), work, run, "w", "0.1")
	c.Env = env
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	waitRecords(t, state, 1)
	out, _ := runIn(t, ".", env, "bash", scriptPath(t, "queue-status.sh"))
	for _, want := range []string{"test slots: 1 held", "slot waiter 0", ": waiting"} {
		if !strings.Contains(out, want) {
			t.Errorf("queue-status.sh output lacks %q:\n%s", want, out)
		}
	}
	waitOverlap(t, "status", run, 2, holder, c)
}

// writeWindow writes a measurement window record into the state directory, ending at until.
func writeWindow(t *testing.T, state string, until time.Time) {
	t.Helper()
	write(t, filepath.Join(state, "window"),
		fmt.Sprintf("id=w-test owner=test start=%d until=%d reason=a test window until=99999999999\n",
			time.Now().Unix(), until.Unix()), 0o644)
}

// noneStarted fails when a work command starts within the time.
func noneStarted(t *testing.T, what, run string, d time.Duration) {
	t.Helper()
	for begin := time.Now(); time.Since(begin) < d; time.Sleep(50 * time.Millisecond) {
		if m, _ := filepath.Glob(filepath.Join(run, "live.*")); len(m) > 0 {
			t.Fatalf("%s: the command started", what)
		}
	}
}

// An open measurement window holds back a test slot claim, which goes on once the window closes; a
// shared benchlock.sh job waits for it alike. The until= inside the reason is free text and not read.
func TestQueueScriptsWaitForAnOpenWindow(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	for _, name := range []string{"testslot.sh", "benchlock.sh"} {
		state := t.TempDir()
		env := append(scriptEnv(t, state), "TESTSLOT_N=1", "BENCHLOCK_CLASS=shared")
		writeWindow(t, state, time.Now().Add(time.Hour))
		work, run := overlapWork(t)
		args := []string{scriptPath(t, name)}
		if name == "benchlock.sh" {
			args = append(args, "shared-job")
		}
		cmd := exec.Command("bash", append(args, work, run, "job", "0")...)
		cmd.Env = env
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		noneStarted(t, name+" under an open window", run, 4*time.Second)
		if err := os.Remove(filepath.Join(state, "window")); err != nil {
			t.Fatal(err)
		}
		waitConcurrency(t, name+" after the window closed", run, 1, 1, cmd)
	}
}

// A window past its until= does not hold a claim back: the claimer moves it aside and logs that.
func TestTestslotIgnoresAnExpiredWindow(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := append(scriptEnv(t, state), "TESTSLOT_N=1")
	writeWindow(t, state, time.Now().Add(-time.Minute))
	work, run := overlapWork(t)
	h := startQueued(t, env, run, scriptPath(t, "testslot.sh"), work, run, "job", "0")
	waitConcurrency(t, "testslot.sh beside an expired window", run, 1, 1, h)
	if _, err := os.Stat(filepath.Join(state, "window")); !os.IsNotExist(err) {
		t.Errorf("the expired window is still in place: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(state, "window.log")); !strings.Contains(string(b), "expired") {
		t.Errorf("window.log does not log the expiry: %q", b)
	}
}

// An exclusive benchlock.sh job starts only after the live test slot holders have finished, and it
// runs its command with SXIR_BENCHLOCK naming the lock's owner.
func TestBenchlockExclusiveWaitsForTestSlots(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := append(scriptEnv(t, state), "TESTSLOT_N=1")
	work, run := overlapWork(t)
	slot := startQueued(t, env, run, scriptPath(t, "testslot.sh"), work, run, "slot", "4")
	owner := filepath.Join(run, "owner")
	x := exec.Command("bash", scriptPath(t, "benchlock.sh"), "timed", "bash", "-c",
		`echo "$SXIR_BENCHLOCK" > "$1"; cat "$2/benchlock/owner" >> "$1"; exec "$3" "$4" timed 0`, "_", owner, state, work, run)
	x.Env = env
	if err := x.Start(); err != nil {
		t.Fatal(err)
	}
	waitOverlap(t, "an exclusive job beside a test slot", run, 2, slot, x)
	if ev := events(t, run); !before(ev, "end slot", "start timed") {
		t.Errorf("the exclusive job started before the test slot ended: %q", ev)
	}
	b, err := os.ReadFile(owner)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "timed ") || lines[0] != lines[1] {
		t.Errorf("SXIR_BENCHLOCK and benchlock/owner: %q, want the same \"timed <pid>\"", lines)
	}
}

// startScript starts a queue script with its own environment and returns it without waiting.
func startScript(t *testing.T, env []string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("bash", args...)
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd
}

// waitEvent returns once the work commands have logged the event.
func waitEvent(t *testing.T, run, event string) {
	t.Helper()
	for begin := time.Now(); time.Since(begin) < 30*time.Second; time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(filepath.Join(run, "events"))
		if slices.Contains(strings.Split(strings.TrimSpace(string(b)), "\n"), event) {
			return
		}
	}
	t.Fatalf("no event %q", event)
}

// waitRuns returns once benchlock.sh has recorded n jobs in runs/.
func waitRuns(t *testing.T, state string, n int) {
	t.Helper()
	for begin := time.Now(); time.Since(begin) < 20*time.Second; time.Sleep(20 * time.Millisecond) {
		if m, _ := filepath.Glob(filepath.Join(state, "runs", "*")); len(m) >= n {
			return
		}
	}
	t.Fatalf("fewer than %d benchlock.sh records", n)
}

// frontRecord queues a record of this test process, which is live, ahead of every waiter of
// testslot.sh: a waiter behind it is not first and may claim only through the stall backstop.
func frontRecord(t *testing.T, state string) {
	t.Helper()
	pid := os.Getpid()
	write(t, filepath.Join(state, "slots", "queue", "000000000000."+strconv.Itoa(pid)), fmt.Sprintf("%d %s\n", pid, procStart(t, pid)), 0o644)
}

// noStall fails when a stall log of the state directory records a stall.
func noStall(t *testing.T, what, state string) {
	t.Helper()
	for _, log := range []string{"testslot-stalls.log", "benchlock-stalls.log"} {
		if b, _ := os.ReadFile(filepath.Join(state, log)); strings.Contains(string(b), "stall") {
			t.Errorf("%s: %s logs a stall: %q", what, log, b)
		}
	}
}

// A test slot claim waits while an exclusive benchlock.sh job holds the lock, and while one is first
// in the queue waiting for the test slots to drain: in both cases the claim starts only after the
// exclusive job, which a stream of test runs would otherwise starve.
func TestTestslotWaitsForAnExclusiveJob(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	t.Run("holding", func(t *testing.T) {
		state := t.TempDir()
		env := append(scriptEnv(t, state), "TESTSLOT_N=1")
		work, run := overlapWork(t)
		x := startScript(t, env, scriptPath(t, "benchlock.sh"), "timed", work, run, "x", "3")
		waitEvent(t, run, "start x")
		w := startScript(t, env, scriptPath(t, "testslot.sh"), work, run, "w", "0")
		waitOverlap(t, "a claim beside an exclusive holder", run, 2, x, w)
		if ev := events(t, run); !before(ev, "end x", "start w") {
			t.Errorf("the claim started while the exclusive job held the lock: %q", ev)
		}
	})
	t.Run("first", func(t *testing.T) {
		state := t.TempDir()
		env := append(scriptEnv(t, state), "TESTSLOT_N=1")
		work, run := overlapWork(t)
		s := startQueued(t, env, run, scriptPath(t, "testslot.sh"), work, run, "s", "3")
		x := startScript(t, env, scriptPath(t, "benchlock.sh"), "timed", work, run, "x", "1")
		waitRuns(t, state, 1)
		w := startScript(t, env, scriptPath(t, "testslot.sh"), work, run, "w", "0")
		waitOverlap(t, "a claim behind a first exclusive job", run, 3, s, x, w)
		if ev := events(t, run); !before(ev, "start x", "start w") {
			t.Errorf("the claim started ahead of the first exclusive job: %q", ev)
		}
	})
}

// With small stall bounds, a waiter that an open window or a first exclusive job holds back neither
// claims nor logs a stall: the backstop counts only time in which nothing legitimate holds it.
func TestQueueScriptsHeldBackDoNotStall(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	t.Run("testslot window", func(t *testing.T) {
		state := t.TempDir()
		env := append(scriptEnv(t, state), "TESTSLOT_N=1", "TESTSLOT_STALL=1")
		frontRecord(t, state)
		writeWindow(t, state, time.Now().Add(time.Hour))
		work, run := overlapWork(t)
		startScript(t, env, scriptPath(t, "testslot.sh"), work, run, "w", "0")
		noneStarted(t, "a waiter under a window", run, 5*time.Second)
		noStall(t, "a waiter under a window", state)
	})
	t.Run("testslot exclusive", func(t *testing.T) {
		state := t.TempDir()
		env := append(scriptEnv(t, state), "TESTSLOT_N=1", "TESTSLOT_STALL=1")
		work, run := overlapWork(t)
		// The exclusive job holds the lock until the test releases it, so the check below runs
		// while it holds however slowly the machine goes.
		hold := filepath.Join(run, "release")
		x := startScript(t, append(env, "WORK_HOLD="+hold), scriptPath(t, "benchlock.sh"), "timed", work, run, "x", "0")
		waitEvent(t, run, "start x")
		frontRecord(t, state)
		w := startScript(t, env, scriptPath(t, "testslot.sh"), work, run, "w", "0")
		time.Sleep(4 * time.Second)
		if ev := events(t, run); slices.Contains(ev, "start w") {
			t.Errorf("a waiter behind an exclusive holder claimed: %q", ev)
		}
		noStall(t, "a waiter behind an exclusive holder", state)
		// With the record ahead gone and the lock released, the waiter claims as the first.
		if err := os.Remove(filepath.Join(state, "slots", "queue", "000000000000."+strconv.Itoa(os.Getpid()))); err != nil {
			t.Fatal(err)
		}
		write(t, hold, "", 0o644)
		waitOverlap(t, "a waiter after the exclusive job", run, 2, x, w)
		if ev := events(t, run); !before(ev, "end x", "start w") {
			t.Errorf("the waiter started before the exclusive job ended: %q", ev)
		}
		noStall(t, "a waiter after the exclusive job", state)
	})
	t.Run("benchlock window", func(t *testing.T) {
		state := t.TempDir()
		env := append(scriptEnv(t, state), "BENCHLOCK_CLASS=shared", "BENCHLOCK_STALL=2")
		writeWindow(t, state, time.Now().Add(time.Hour))
		work, run := overlapWork(t)
		y := startScript(t, env, scriptPath(t, "benchlock.sh"), "shared-job", work, run, "y", "0")
		noneStarted(t, "a shared job under a window", run, 5*time.Second)
		// The job runs once the window closes, without counting the window as a stall.
		if err := os.Remove(filepath.Join(state, "window")); err != nil {
			t.Fatal(err)
		}
		waitConcurrency(t, "a shared job after the window", run, 1, 1, y)
		noStall(t, "a shared job under a window", state)
	})
}

// Claims queued while a window is open are admitted in the order they queued once it closes: six
// waiters queued in sequence run in that order, which admission by whoever polls first would break.
func TestTestslotAdmitsWindowWaitersInOrder(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := append(scriptEnv(t, state), "TESTSLOT_N=1")
	writeWindow(t, state, time.Now().Add(time.Hour))
	work, run := overlapWork(t)
	var cmds []*exec.Cmd
	var names []string
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("w%d", i)
		names = append(names, name)
		cmds = append(cmds, startScript(t, env, scriptPath(t, "testslot.sh"), work, run, name, "0.1"))
		waitRecords(t, state, i+1)
	}
	time.Sleep(2 * time.Second)
	if err := os.Remove(filepath.Join(state, "window")); err != nil {
		t.Fatal(err)
	}
	waitOverlap(t, "claims queued during a window", run, 6, cmds...)
	ev := events(t, run)
	for i := 1; i < len(names); i++ {
		if !before(ev, "start "+names[i-1], "start "+names[i]) {
			t.Errorf("%s did not start before %s: %q", names[i-1], names[i], ev)
		}
	}
}

// A shared benchlock.sh job queued behind an exclusive job that waits for the test slots to drain
// does not count that wait toward its stall: it neither takes the lock ahead of the exclusive job
// nor logs a stall.
func TestBenchlockSharedBehindDrainingExclusive(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	state := t.TempDir()
	env := append(scriptEnv(t, state), "TESTSLOT_N=1")
	work, run := overlapWork(t)
	s := startQueued(t, env, run, scriptPath(t, "testslot.sh"), work, run, "s", "5")
	x := startScript(t, env, scriptPath(t, "benchlock.sh"), "timed", work, run, "x", "0.5")
	waitRuns(t, state, 1)
	y := startScript(t, append(env, "BENCHLOCK_CLASS=shared", "BENCHLOCK_STALL=1"), scriptPath(t, "benchlock.sh"), "shared-job", work, run, "y", "0")
	waitConcurrency(t, "a shared job behind a draining exclusive job", run, 3, 1, s, x, y)
	if ev := events(t, run); !before(ev, "start x", "start y") {
		t.Errorf("the shared job started ahead of the exclusive job: %q", ev)
	}
	noStall(t, "a shared job behind a draining exclusive job", state)
}

// powerProbe makes the power probe of benchlock.sh (SXIR_POWER_CMD) read a file in the format of
// pmset -g batt, and returns the environment and a function that sets what the file reports; an
// empty source removes the file, so that the probe fails. The file is replaced by a rename, so
// that the probe never reads it half written.
func powerProbe(t *testing.T, env []string) ([]string, func(source string, charge int)) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "batt")
	set := func(source string, charge int) {
		t.Helper()
		if source == "" {
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			return
		}
		tmp := filepath.Join(dir, "batt.tmp")
		write(t, tmp, fmt.Sprintf("Now drawing from '%s'\n -InternalBattery-0 (id=1)\t%d%%; charging; 1:00 remaining present: true\n", source, charge), 0o644)
		if err := os.Rename(tmp, file); err != nil {
			t.Fatal(err)
		}
	}
	set("AC Power", 100)
	return append(env, "SXIR_POWER_CMD=cat "+file), set
}

// runBenchlock runs benchlock.sh to its end and returns its exit status and its standard error.
func runBenchlock(t *testing.T, env []string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{scriptPath(t, "benchlock.sh")}, args...)...)
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, stderr.String()
}

// waitExit waits for a started command and returns its exit status.
func waitExit(t *testing.T, cmd *exec.Cmd) int {
	t.Helper()
	err := cmd.Wait()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return 0
}

// powerLog returns benchlock-power.log of a state directory.
func powerLog(state string) string {
	b, _ := os.ReadFile(filepath.Join(state, "benchlock-power.log"))
	return string(b)
}

// queueEmpty fails when a benchlock.sh job left its record or the lock behind.
func queueEmpty(t *testing.T, what, state string) {
	t.Helper()
	if m, _ := filepath.Glob(filepath.Join(state, "runs", "*")); len(m) > 0 {
		t.Errorf("%s: records left in runs/: %v", what, m)
	}
	if _, err := os.Stat(filepath.Join(state, "benchlock")); !os.IsNotExist(err) {
		t.Errorf("%s: the lock is still held (%v)", what, err)
	}
}

// The power guard of a timed (exclusive) benchlock.sh job: it refuses to start on battery or under the
// charge floor, on AC too, both when it queues and once admitted; it reads the power while the command
// runs and fails the run with exit 75 when any reading was bad, even one between two good ones; a
// failed probe or a reading that names neither AC nor battery is refused like battery, at the start
// and during the run; a machine with no probe runs, with a trace in the log; the test probe counts
// only with SXIR_POWER_TEST=1; the sampler dies with a killed wrapper; and shared jobs are not checked.
func TestBenchlockPowerGuard(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, scriptsDeps...)
	needTools(t)
	refused := func(t *testing.T, source string, charge int, why string) {
		state := t.TempDir()
		env, set := powerProbe(t, scriptEnv(t, state))
		set(source, charge)
		work, run := overlapWork(t)
		code, stderr := runBenchlock(t, env, "timed", work, run, "x", "0")
		if code != 75 || !strings.Contains(stderr, why) {
			t.Errorf("exit %d, stderr %q: want 75 and %q", code, stderr, why)
		}
		if b, _ := os.ReadFile(filepath.Join(run, "events")); len(b) > 0 {
			t.Errorf("the command ran: %q", b)
		}
		if log := powerLog(state); !strings.Contains(log, "refused (queued): timed.") || !strings.Contains(log, why) {
			t.Errorf("benchlock-power.log: %q", log)
		}
		queueEmpty(t, "a refused job", state)
	}
	t.Run("battery", func(t *testing.T) { refused(t, "Battery Power", 80, "on battery (80%)") })
	t.Run("under the floor on AC", func(t *testing.T) { refused(t, "AC Power", 30, "at 30% charge, under the floor of 50%") })
	t.Run("unrecognised source", func(t *testing.T) {
		refused(t, "UPS Power", 80, "on unknown power: the probe named neither AC nor battery")
	})
	t.Run("probe failure", func(t *testing.T) { refused(t, "", 0, "on unknown power: the power probe failed") })
	t.Run("AC above the floor", func(t *testing.T) {
		state := t.TempDir()
		env, set := powerProbe(t, scriptEnv(t, state))
		set("AC Power", 50)
		out := filepath.Join(t.TempDir(), "env")
		code, stderr := runBenchlock(t, env, "timed", "bash", "-c", `printf '%s\n%s\n' "$SXIR_POWER_LOG" "$SXIR_POWER_START" > "$1"; cat "$SXIR_POWER_LOG" >> "$1"; exit 3`, "_", out)
		if code != 3 {
			t.Errorf("exit %d (stderr %q), want the command's 3", code, stderr)
		}
		b, _ := os.ReadFile(out)
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(lines) != 3 || !strings.HasPrefix(lines[0], filepath.Join(state, "power", "timed.")) ||
			lines[1] != "source=ac charge=50 probe=test" || !strings.HasSuffix(lines[2], " "+lines[1]) {
			t.Errorf("SXIR_POWER_LOG, SXIR_POWER_START and the log: %q", lines)
		}
		if _, err := os.Stat(lines[0]); !os.IsNotExist(err) {
			t.Errorf("the readings of a valid run are kept: %v", err)
		}
		if log := powerLog(state); log != "" {
			t.Errorf("benchlock-power.log after a valid run: %q", log)
		}
		queueEmpty(t, "a valid run", state)
	})
	t.Run("admitted", func(t *testing.T) {
		state := t.TempDir()
		env, set := powerProbe(t, scriptEnv(t, state))
		work, run := overlapWork(t)
		hold := filepath.Join(run, "release")
		x := startScript(t, append(env, "WORK_HOLD="+hold), scriptPath(t, "benchlock.sh"), "first", work, run, "x", "0")
		waitEvent(t, run, "start x")
		y := startScript(t, env, scriptPath(t, "benchlock.sh"), "second", work, run, "y", "0")
		waitQueued(t, state, "second")
		set("Battery Power", 70)
		write(t, hold, "", 0o644)
		if code := waitExit(t, y); code != 75 {
			t.Errorf("the job admitted on battery exited %d, want 75", code)
		}
		if code := waitExit(t, x); code != 75 {
			t.Errorf("the job that ended on battery exited %d, want 75", code)
		}
		if ev := events(t, run); slices.Contains(ev, "start y") {
			t.Errorf("the job admitted on battery ran: %q", ev)
		}
		if log := powerLog(state); !strings.Contains(log, "refused (admitted): second.") {
			t.Errorf("benchlock-power.log: %q", log)
		}
		queueEmpty(t, "a job refused once admitted", state)
	})
	// midRun makes the probe report source/charge while a timed job runs, then AC again before the
	// end: the reading between is enough to fail the run. flagged is what queue-status.sh says then.
	midRun := func(t *testing.T, source string, charge int, seen, why, flagged string) {
		state := t.TempDir()
		env, set := powerProbe(t, append(scriptEnv(t, state), "BENCHLOCK_POWER_POLL=1"))
		work, run := overlapWork(t)
		hold := filepath.Join(run, "release")
		var stderr strings.Builder
		x := exec.Command("bash", scriptPath(t, "benchlock.sh"), "timed", work, run, "x", "0")
		x.Env = append(env, "WORK_HOLD="+hold)
		x.Stderr = &stderr
		if err := x.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = x.Process.Kill() })
		waitEvent(t, run, "start x")
		set(source, charge)
		logs, _ := filepath.Glob(filepath.Join(state, "power", "timed.*.log"))
		if len(logs) != 1 {
			t.Fatalf("power logs: %v", logs)
		}
		for begin := time.Now(); ; time.Sleep(50 * time.Millisecond) {
			if b, _ := os.ReadFile(logs[0]); strings.Contains(string(b), seen) {
				break
			}
			if time.Since(begin) > 30*time.Second {
				t.Fatalf("the sampler recorded no %q reading", seen)
			}
		}
		// queue-status.sh flags a timed job that runs on such power.
		if out, err := runIn(t, ".", env, "bash", scriptPath(t, "queue-status.sh")); err == nil || !strings.Contains(out, flagged) {
			t.Errorf("queue-status.sh during the run: %v\n%s", err, out)
		}
		set("AC Power", 100)
		write(t, hold, "", 0o644)
		if code := waitExit(t, x); code != 75 || !strings.Contains(stderr.String(), "its timings are invalid; the command exited 0") {
			t.Errorf("exit %d, stderr %q", code, stderr.String())
		}
		if log := powerLog(state); !strings.Contains(log, "invalid: timed.") || !strings.Contains(log, why) {
			t.Errorf("benchlock-power.log: %q", log)
		}
		if _, err := os.Stat(logs[0]); err != nil {
			t.Errorf("the readings of an invalid run are gone: %v", err)
		}
		queueEmpty(t, "an invalid run", state)
	}
	t.Run("battery during the run", func(t *testing.T) {
		midRun(t, "Battery Power", 70, "source=battery", "on battery (70%)",
			"POWER: 1 exclusive job(s) queued or running while the machine is on battery (70%)")
	})
	t.Run("probe failure during the run", func(t *testing.T) {
		midRun(t, "", 0, "probe=failed", "on unknown power: the power probe failed",
			"POWER: 1 exclusive job(s) queued or running while the machine is on unknown power: the power probe failed")
	})
	t.Run("unknown", func(t *testing.T) {
		state := t.TempDir()
		env := append(scriptEnv(t, state), "SXIR_POWER_CMD=none")
		work, run := overlapWork(t)
		if code, stderr := runBenchlock(t, env, "timed", work, run, "x", "0"); code != 0 {
			t.Errorf("exit %d (stderr %q) with no power probe", code, stderr)
		}
		log := powerLog(state)
		if !strings.Contains(log, "unknown (queued): timed.") || !strings.Contains(log, "runs without a power check") ||
			!strings.Contains(log, "unchecked: timed.") {
			t.Errorf("benchlock-power.log: %q", log)
		}
		logs, _ := filepath.Glob(filepath.Join(state, "power", "timed.*.log"))
		if len(logs) != 1 {
			t.Fatalf("the readings of an unchecked run are not kept: %v", logs)
		}
		if b, _ := os.ReadFile(logs[0]); !strings.Contains(string(b), "source=unknown charge=- probe=none") {
			t.Errorf("the readings of an unchecked run: %q", b)
		}
	})
	t.Run("test probe needs SXIR_POWER_TEST", func(t *testing.T) {
		state := t.TempDir()
		lib := scriptPath(t, "lib.sh")
		probe := func(env []string) string {
			t.Helper()
			out, err := runIn(t, ".", env, "bash", "-c", `. "$1"; sxir_power`, "_", lib)
			if err != nil {
				t.Fatalf("sxir_power: %v\n%s", err, out)
			}
			return strings.TrimSpace(out)
		}
		env := scriptEnv(t, state)
		if out := probe(env); out != "source=ac charge=100 probe=test" {
			t.Errorf("sxir_power with the test probe: %q", out)
		}
		var without []string
		for _, kv := range env {
			if !strings.HasPrefix(kv, "SXIR_POWER_TEST=") {
				without = append(without, kv)
			}
		}
		if out := probe(without); strings.Contains(out, "probe=test") {
			t.Errorf("sxir_power used SXIR_POWER_CMD without SXIR_POWER_TEST: %q", out)
		}
	})
	t.Run("sampler dies with a killed wrapper", func(t *testing.T) {
		state := t.TempDir()
		env, _ := powerProbe(t, append(scriptEnv(t, state), "BENCHLOCK_POWER_POLL=1"))
		work, run := overlapWork(t)
		hold := filepath.Join(run, "release")
		t.Cleanup(func() { write(t, hold, "", 0o644) })
		x := startScript(t, append(env, "WORK_HOLD="+hold), scriptPath(t, "benchlock.sh"), "timed", work, run, "x", "0")
		waitEvent(t, run, "start x")
		logs, _ := filepath.Glob(filepath.Join(state, "power", "timed.*.log"))
		if len(logs) != 1 {
			t.Fatalf("power logs: %v", logs)
		}
		if err := x.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		_ = x.Wait()
		// A sampler already asleep may write one more reading; after that the log must stay still.
		time.Sleep(2500 * time.Millisecond)
		before, _ := os.ReadFile(logs[0])
		time.Sleep(2500 * time.Millisecond)
		if after, _ := os.ReadFile(logs[0]); len(after) != len(before) {
			t.Errorf("the sampler outlived the killed wrapper: %q, then %q", before, after)
		}
	})
	t.Run("shared", func(t *testing.T) {
		state := t.TempDir()
		env, set := powerProbe(t, append(scriptEnv(t, state), "BENCHLOCK_CLASS=shared"))
		set("Battery Power", 10)
		work, run := overlapWork(t)
		if code, stderr := runBenchlock(t, env, "gate", work, run, "s", "0"); code != 0 {
			t.Errorf("a shared job on battery exited %d (stderr %q)", code, stderr)
		}
		if log := powerLog(state); log != "" {
			t.Errorf("benchlock-power.log of a shared job: %q", log)
		}
		if _, err := os.Stat(filepath.Join(state, "power")); !os.IsNotExist(err) {
			t.Errorf("a shared job wrote power readings: %v", err)
		}
	})
}
