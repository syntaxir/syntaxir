package tooltest

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestHungDecision checks when the pool calls a job hung: by the CPU time it used and by the wall clock
// backstop, and not by the wall time of a job that gets CPU time slowly or none for a long time (a job
// that the machine swaps out): a job that waits for nothing is the worker's to report.
func TestHungDecision(t *testing.T) {
	const timeout, backstop = 10 * time.Second, 100 * time.Second
	start := time.Unix(1000, 0)
	at := func(s int) time.Time { return start.Add(time.Duration(s) * time.Second) }
	for _, c := range []struct {
		name string
		now  time.Time
		cpu  time.Duration
		hung bool
	}{
		{"computing for the timeout", at(10), 10 * time.Second, true},
		{"computing for less, slowly", at(90), 3 * time.Second, false},
		{"no CPU time for the timeout", at(10), 0, false},
		{"no CPU time for long, under the backstop", at(99), 0, false},
		{"over the backstop", at(100), time.Second, true},
		{"under the backstop", at(99), time.Second, false},
	} {
		if got := hung(timeout, backstop, start, c.now, c.cpu) != ""; got != c.hung {
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
		_, err := RunNodeScriptWithin(t, timeout, node, dir, "main.ts")
		if err == nil || !strings.Contains(err.Error(), "ran past") || !strings.Contains(err.Error(), "of CPU time") {
			t.Fatalf("%v; want the loop killed for the CPU time it used", err)
		}
	})

	t.Run("await that never settles", func(t *testing.T) {
		dir := poolScript(t, `await new Promise(() => {});`, nil)
		_, err := RunNodeScriptWithin(t, timeout, node, dir, "main.ts")
		// Reported by what the script waits for, not by a time: the reason is the idle rule's, and neither the CPU
		// limit nor the wall backstop. How long it took depends on the load, so it is not asserted.
		if err == nil || !strings.Contains(err.Error(), "waits for nothing") || strings.Contains(err.Error(), "ran past") || strings.Contains(err.Error(), "CPU time") {
			t.Fatalf("%v; want the await reported as waiting for nothing", err)
		}
	})
}

// TestParseProcStat checks the clock ticks of /proc/<pid>/stat, with a command name that has spaces and
// a parenthesis.
func TestParseProcStat(t *testing.T) {
	const tail = " S 1 2 3 4 5 6 7 8 9 10 250 50 0 0 20 0"
	for _, name := range []string{"node", "a b) c (d"} {
		got, err := parseProcStat("123 (" + name + ")" + tail)
		if err != nil || got != 3*time.Second {
			t.Errorf("%q: %v, %v; want 3s", name, got, err)
		}
	}
	for _, bad := range []string{"", "1 (x", "1 (x) S 1 2", "1 (x)" + strings.Replace(tail, "250", "x", 1)} {
		if _, err := parseProcStat(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// TestAwaitJobBaseline checks that a failed first CPU reading does not charge the CPU time that the
// worker used for earlier jobs to this one: the job counts from the first reading that succeeds, so a
// worker with a long history is not killed for it, and a loop is still killed.
func TestAwaitJobBaseline(t *testing.T) {
	const timeout = time.Second
	run := func(first time.Duration, reads int, fn func(n int) time.Duration) (string, error) {
		done := make(chan nodeReply)
		var n int
		read := func() (time.Duration, error) {
			n++
			if n <= reads {
				return 0, errors.New("ps failed")
			}
			return fn(n), nil
		}
		killed := false
		var why string
		go func() { time.Sleep(1500 * time.Millisecond); done <- nodeReply{res: NodeResult{Stdout: "ok"}} }()
		_, err := awaitJob(done, read, func() error { killed = true; return nil }, timeout, time.Minute, func(w string) error { why = w; return errors.New(w) })
		if killed != (err != nil) {
			t.Errorf("killed=%v, err=%v", killed, err)
		}
		return why, err
	}
	// A worker with an hour of CPU time from earlier jobs whose first reading failed: the later readings
	// start at that hour and the job adds a little to it.
	if why, err := run(0, 1, func(n int) time.Duration { return time.Hour + time.Duration(n)*time.Millisecond }); err != nil {
		t.Errorf("healthy job with a failed first reading was killed: %s", why)
	}
	// The same worker that burns CPU: killed, counting from the first good reading.
	if _, err := run(0, 1, func(n int) time.Duration { return time.Hour + time.Duration(n)*time.Second }); err == nil {
		t.Errorf("a loop with a failed first reading was not killed")
	}
}

// TestRunNodeScriptWaitsForSomething checks that a script whose top-level await waits for something that
// will come is not reported as waiting for nothing, however long it takes: a timer (here longer than the
// timeout of the job), a file read that is pending on a pipe and a child process. Each script finishes with its output, and a script that awaits a promise that nothing
// settles is still reported at once afterwards in the same worker.
func TestRunNodeScriptWaitsForSomething(t *testing.T) {
	node := RequireNode(t)
	const timeout = time.Second
	run := func(t *testing.T, src string, args []string, during func()) {
		t.Helper()
		dir := poolScript(t, src, nil)
		done := make(chan struct{})
		if during != nil {
			go func() {
				defer close(done)
				during()
			}()
		} else {
			close(done)
		}
		res, err := RunNodeScriptWithin(t, timeout, node, dir, "main.ts", args...)
		<-done
		if err != nil || res.Code != 0 || res.Stdout != "done\n" {
			t.Fatalf("%+v, %v; want the script to finish with its output", res, err)
		}
	}
	t.Run("timer", func(t *testing.T) {
		// Idle on the CPU and 1.5 times as long as the timeout of the job.
		run(t, `await new Promise((r) => setTimeout(r, 1500)); console.log("done");`, nil, nil)
	})
	t.Run("file read on a pipe", func(t *testing.T) {
		// The read stays pending until the test opens the pipe for writing, later than a check or two.
		fifo := filepath.Join(t.TempDir(), "pipe")
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Skip("no named pipes:", err)
		}
		run(t, `import fs from "node:fs";
const text = await fs.promises.readFile(process.argv[2], "utf8");
if (text !== "x") throw new Error("read " + text);
console.log("done");`, []string{fifo}, func() {
			time.Sleep(300 * time.Millisecond)
			f, err := os.OpenFile(fifo, os.O_WRONLY, 0)
			if err != nil {
				t.Error(err)
				return
			}
			f.WriteString("x")
			f.Close()
		})
	})
	t.Run("child process", func(t *testing.T) {
		run(t, `import { spawn } from "node:child_process";
await new Promise((r) => spawn("sleep", ["0.4"]).on("exit", r));
console.log("done");`, nil, nil)
	})
	// Work on node's thread pool is not listed by process.getActiveResourcesInfo while it is pending.
	t.Run("zlib.gzip", func(t *testing.T) {
		run(t, `import zlib from "node:zlib";
import crypto from "node:crypto";
const out = await new Promise((r, j) => zlib.gzip(crypto.randomBytes(8 << 20), (e, b) => (e ? j(e) : r(b))));
if (out.length === 0) throw new Error("empty");
console.log("done");`, nil, nil)
	})
	t.Run("subtle.digest", func(t *testing.T) {
		run(t, `const d = await crypto.subtle.digest("SHA-256", new Uint8Array(1 << 24));
if (d.byteLength !== 32) throw new Error("digest");
console.log("done");`, nil, nil)
	})
	t.Run("pbkdf2", func(t *testing.T) {
		run(t, `import crypto from "node:crypto";
await new Promise((r, j) => crypto.pbkdf2("p", "s", 300000, 64, "sha512", (e) => (e ? j(e) : r())));
console.log("done");`, nil, nil)
	})
	t.Run("an await that nothing settles, after them", func(t *testing.T) {
		dir := poolScript(t, `await new Promise(() => {});`, nil)
		_, err := RunNodeScriptWithin(t, timeout, node, dir, "main.ts")
		if err == nil || !strings.Contains(err.Error(), "waits for nothing") {
			t.Fatalf("%v; want the await reported as waiting for nothing", err)
		}
	})
}
