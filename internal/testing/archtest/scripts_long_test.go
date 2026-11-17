//go:build zp_long

package archtest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The queue scripts' races and signal handling, run against temporary state directories. They
// take about a minute, so they belong to the long tier.

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

// A live pid with another start time is a reused pid, not the holder; the same start time is.
func TestTestslotReusedPid(t *testing.T) {
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
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
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

// survivors lists the processes whose command line holds the marker: the command, and a copy of
// the script that is on its way to run it.
func survivors(marker string) string {
	out, _ := exec.Command("pgrep", "-f", "sleep "+marker).Output()
	return strings.TrimSpace(string(out))
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
// is a sleep of 40 seconds or more, so a command that was missed outlives the wait below; a copy
// of the script that is only leaving, or a subshell of it that a loaded machine delays, does not.
func TestQueueScriptsTermDuringStart(t *testing.T) {
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
			args := append(append([]string{}, base...), "sleep", marker)
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
			deadline := time.Now().Add(5 * time.Second)
			for survivors(marker) != "" && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			if pids := survivors(marker); pids != "" {
				ps, _ := exec.Command("ps", "-o", "pid,ppid,pgid,stat,args", "-p", strings.ReplaceAll(pids, "\n", ",")).Output()
				_ = exec.Command("pkill", "-f", "sleep "+marker).Run()
				t.Fatalf("%s: the command survived a TERM sent after %v and 5 s of waiting:\n%s", name, delay, ps)
			}
		}
	}
}

// The command's standard input is /dev/null: job control does not redirect it, and a queued
// command must not read the pipe or the terminal of its caller.
func TestQueueScriptsCommandStdinIsNull(t *testing.T) {
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
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
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
