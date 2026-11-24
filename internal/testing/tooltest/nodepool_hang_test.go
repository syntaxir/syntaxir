package tooltest

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestHungDecision checks when the pool calls a job hung: by the CPU time it used, by the wall time
// without any CPU time, and by the wall clock backstop, and not by the wall time of a job that gets
// CPU time slowly.
func TestHungDecision(t *testing.T) {
	const timeout, backstop = 10 * time.Second, 100 * time.Second
	start := time.Unix(1000, 0)
	at := func(s int) time.Time { return start.Add(time.Duration(s) * time.Second) }
	for _, c := range []struct {
		name      string
		now, prog time.Time
		cpu       time.Duration
		hung      bool
	}{
		{"computing for the timeout", at(10), at(10), 10 * time.Second, true},
		{"computing for less, slowly", at(90), at(90), 3 * time.Second, false},
		{"no CPU time for the timeout", at(10), at(0), 0, true},
		{"CPU time until a moment ago", at(60), at(59), 2 * time.Second, false},
		{"idle for less than the timeout", at(9), at(0), 0, false},
		{"over the backstop", at(100), at(99), time.Second, true},
		{"under the backstop", at(99), at(98), time.Second, false},
	} {
		if got := hung(timeout, backstop, start, c.now, c.prog, c.cpu) != ""; got != c.hung {
			t.Errorf("%s: hung %v, want %v", c.name, got, c.hung)
		}
	}
}

func TestParsePSTime(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"0:00.31":      310 * time.Millisecond,
		"00:00:01":     time.Second,
		"1:02.50":      62*time.Second + 500*time.Millisecond,
		"01:02:03":     time.Hour + 2*time.Minute + 3*time.Second,
		"2-01:00:00":   49 * time.Hour,
		"12:34:56.789": 12*time.Hour + 34*time.Minute + 56*time.Second + 789*time.Millisecond,
	} {
		got, err := parsePSTime(in)
		if err != nil || got != want {
			t.Errorf("%q: %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "5", "a:b", "1:2:3:4"} {
		if _, err := parsePSTime(in); err == nil {
			t.Errorf("%q parses", in)
		}
	}
}

// writePID is the part of a script that tells the test the process it runs in.
const writePID = `import { writeFileSync } from "node:fs";
writeFileSync(import.meta.dirname + "/pid", String(process.pid));
`

// readPID waits for the file that writePID writes.
func readPID(t *testing.T, dir string) int {
	t.Helper()
	for range 2000 {
		if b, err := os.ReadFile(filepath.Join(dir, "pid")); err == nil && len(b) > 0 {
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err == nil {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the script did not start")
	return 0
}

// TestRunNodeScriptHangDetection checks that the pool tells a hung script from a slow one by the
// script's own CPU time, not the wall clock: a script that computes for a fraction of its timeout
// while the machine gives it a fraction of the CPU, so that it takes longer than its timeout on the
// wall clock, succeeds; a loop that never ends and an await that never settles are killed.
func TestRunNodeScriptHangDetection(t *testing.T) {
	node := RequireNode(t)
	const timeout = 2 * time.Second

	t.Run("starved, not hung", func(t *testing.T) {
		// One second of computation, which the test then gives the worker a quarter of the time for: four
		// seconds or more of wall time, over the two seconds of the timeout.
		dir := poolScript(t, writePID+`const t0 = process.cpuUsage();
for (;;) { const c = process.cpuUsage(t0); if (c.user + c.system >= 1e6) break; }
console.log("done");`, nil)
		stop := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			pid := readPID(t, dir)
			defer syscall.Kill(pid, syscall.SIGCONT)
			for {
				select {
				case <-stop:
					return
				default:
				}
				syscall.Kill(pid, syscall.SIGSTOP)
				time.Sleep(150 * time.Millisecond)
				syscall.Kill(pid, syscall.SIGCONT)
				time.Sleep(50 * time.Millisecond)
			}
		}()
		start := time.Now()
		res, err := RunNodeScriptWithin(t, timeout, node, dir, "main.ts")
		close(stop)
		wg.Wait()
		if err != nil || res.Stdout != "done\n" {
			t.Fatalf("%+v, %v; want the script to finish", res, err)
		}
		if took := time.Since(start); took < timeout {
			t.Errorf("the script took %v, not more than its timeout %v: the test does not starve it", took, timeout)
		}
	})

	t.Run("loop that never ends", func(t *testing.T) {
		dir := poolScript(t, `while (true) {}`, nil)
		start := time.Now()
		_, err := RunNodeScriptWithin(t, timeout, node, dir, "main.ts")
		if err == nil || !strings.Contains(err.Error(), "ran past") || !strings.Contains(err.Error(), "CPU time") {
			t.Fatalf("%v; want the loop killed for the CPU time it used", err)
		}
		if took := time.Since(start); took > 20*time.Second {
			t.Errorf("killed after %v", took)
		}
	})

	t.Run("await that never settles", func(t *testing.T) {
		dir := poolScript(t, `await new Promise(() => {});`, nil)
		_, err := RunNodeScriptWithin(t, timeout, node, dir, "main.ts")
		if err == nil || !strings.Contains(err.Error(), "ran past") || !strings.Contains(err.Error(), "no CPU time") {
			t.Fatalf("%v; want the await killed for using no CPU time", err)
		}
	})
}
