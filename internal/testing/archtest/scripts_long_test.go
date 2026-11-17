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

// A TERM that arrives while the command starts must not leave the command running.
func TestQueueScriptsTermDuringStart(t *testing.T) {
	needTools(t)
	for _, name := range []string{"testslot.sh", "benchlock.sh"} {
		for i := 0; i < 70; i++ {
			state := t.TempDir()
			env := scriptEnv(t, state)
			marker := fmt.Sprintf("%d.%03d", 40+len(name), i)
			args := []string{scriptPath(t, name)}
			if name == "benchlock.sh" {
				args = append(args, "t")
			}
			args = append(args, "sleep", marker)
			cmd := exec.Command("bash", args...)
			cmd.Env = env
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Duration(i) * 3 * time.Millisecond)
			_ = cmd.Process.Signal(syscall.SIGTERM)
			_ = cmd.Wait()
			time.Sleep(100 * time.Millisecond)
			if out, _ := exec.Command("pgrep", "-f", "sleep "+marker).Output(); len(out) != 0 {
				_ = exec.Command("pkill", "-f", "sleep "+marker).Run()
				t.Fatalf("%s: the command survived a TERM sent after %d ms (pids %s)", name, i*12, strings.TrimSpace(string(out)))
			}
		}
	}
}
