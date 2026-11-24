package tooltest

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// nodePoolWorker is the script of the workers of the node pool.
//
//go:embed nodepool.mjs
var nodePoolWorker string

// NodeJobTimeout is how much work RunNodeScript lets one script do before it kills the worker that runs it
// and fails the script as hung: this much CPU time, or this much wall time in which the script uses none
// (an await that never settles), and ten times as much wall time at the very most. The limit is on CPU
// time and not on the wall clock so that a loaded machine, which makes a script slow but not hung, does
// not fail it (hung). It is a variable so that the tests of the pool can shorten it.
var NodeJobTimeout = 3 * time.Minute

// NodeDrainTimeout is how long RunNodeScript lets what a script left running (timers, requests) go on after its
// top-level code has finished, as node would run it, before it fails the script and replaces the worker. A
// variable so that the tests of the pool can shorten it.
var NodeDrainTimeout = 10 * time.Second

// nodeJobsPerWorker is the number of scripts after which a worker is replaced, so that the modules that
// the scripts loaded do not accumulate in one process.
const nodeJobsPerWorker = 40

// NodeResult is the outcome of a script that RunNodeScript ran: what it wrote to its standard output
// and error, and its exit code (1 if it threw an error or a promise it left rejected, the argument of
// process.exit if it called it).
type NodeResult struct {
	Stdout, Stderr string
	Code           int
	// Leak describes the state that the script changed in the worker beyond its own module (globals,
	// built-in prototypes, process, built-in modules), one difference per line; RunNodeScript reports
	// it as an error and replaces the worker, so that no other script sees it.
	Leak string
}

// RunNodeScript runs the script of the module in dir (script is its file name there, for example
// main.ts) with Node, as `cd dir && node script args...` would, and returns its outcome. An error
// reports a failure of the pool itself (a worker that died or a script that ran past NodeJobTimeout); a
// script that failed has a nonzero Code.
//
// The scripts run one at a time in the processes of a pool that all the tests of the test process share,
// so a script pays the start of node, and the cost of the first type stripping of a library that a
// process pays, only once for a number of scripts instead of every time. Each script is independent of
// the others: it runs in a directory of its own, as a module that no other script imports, with its
// own output and exit code; one that throws or exits fails alone, and one that hangs is killed with its
// worker, which the pool replaces, without affecting the scripts that run in the other workers. A
// script must import only from its own directory and the built-in modules, must not read its standard
// input, and must not depend on the process (argv, the heap, timing): tests that measure the process run
// node themselves.
//
// A script is finished when what it started is: like node, the pool lets the timers and requests that
// the script left run, up to NodeDrainTimeout, and their output, throws and exits belong to the script. A
// script that leaves resources running past that (a setInterval) fails and its worker is replaced. The
// exit code is the argument of the first call of process.exit, even if the script caught the exception
// that stops it (the output written after the call is dropped), else process.exitCode, else 1 for a throw
// or a rejection that nothing handled, else 0.
//
// A script must leave the worker as it found it. After every script the worker compares everything that
// is reachable from the global object, from process and from the modules fs, path, util, os, events, stream and buffer, and the
// iterator and generator prototypes, through own properties and prototypes (the value or the identity
// that every property holds, the functions of accessors, the prototype and the extensibility of every
// object), the title, umask, exit code and listeners of process, the default listener limit of events, and its environment, with their state at the
// start of the worker; a difference fails the script with an error that lists it, and the worker is
// replaced, so a script that pollutes a global, or a mutant of the library that does, cannot change what
// another script sees. The worker takes the built-ins that it uses for this before it runs a script, so
// a script that replaces them (console.log, Array.prototype.push, JSON.stringify) is detected and does
// not blind the check.
//
// What still differs from `node script`: the standard output and the error output are collected
// separately, so NodeCombinedOutput gives the output followed by the error output instead of the two
// interleaved; an error that no one catches is printed as its stack, without the source excerpt that node
// prints; the 'exit' listeners of process do not run; a top-level await that never settles runs
// until it is reported as having run past its time, where node exits with code 13; and a worker that
// crashes is a pool error, not a script exit code.
func RunNodeScript(t testing.TB, node, dir, script string, args ...string) (NodeResult, error) {
	t.Helper()
	return RunNodeScriptWithin(t, NodeJobTimeout, node, dir, script, args...)
}

// RunNodeScriptWithin is RunNodeScript with the limit after which a script that has not finished is killed
// and reported as hung (instead of NodeJobTimeout; see there for what the limit measures), for the test of a script that hangs on purpose. A timeout
// shorter than NodeDrainTimeout also reports a script whose timers have not run out as hung, since the pool waits
// for them as node does.
func RunNodeScriptWithin(t testing.TB, timeout time.Duration, node, dir, script string, args ...string) (NodeResult, error) {
	t.Helper()
	w, err := nodePool.acquire(node)
	if err != nil {
		return NodeResult{}, err
	}
	res, err := w.run(t, timeout, dir, script, args)
	if err == nil && res.Leak != "" {
		err = fmt.Errorf("node pool: %s in %s changed the state of the worker, which was replaced:\n%s", script, dir, res.Leak)
	}
	nodePool.release(w, err != nil)
	return res, err
}

type nodePoolState struct {
	once  sync.Once
	slots chan struct{} // one token per worker that may run
	mu    sync.Mutex
	idle  []*nodeWorker
	dir   string
}

var nodePool nodePoolState

func (p *nodePoolState) init() {
	p.once.Do(func() {
		n := min(max(runtime.GOMAXPROCS(0)/2, 2), 6)
		p.slots = make(chan struct{}, n)
	})
}

func (p *nodePoolState) acquire(node string) (*nodeWorker, error) {
	p.init()
	p.slots <- struct{}{}
	p.mu.Lock()
	if n := len(p.idle); n > 0 {
		w := p.idle[n-1]
		p.idle = p.idle[:n-1]
		p.mu.Unlock()
		return w, nil
	}
	p.mu.Unlock()
	w, err := p.start(node)
	if err != nil {
		<-p.slots
	}
	return w, err
}

// release returns w to the pool, or stops it when it failed or has run enough scripts.
func (p *nodePoolState) release(w *nodeWorker, failed bool) {
	if failed || w.jobs >= nodeJobsPerWorker {
		w.stop()
	} else {
		p.mu.Lock()
		p.idle = append(p.idle, w)
		p.mu.Unlock()
	}
	<-p.slots
}

func (p *nodePoolState) start(node string) (*nodeWorker, error) {
	p.mu.Lock()
	if p.dir == "" {
		d, err := os.MkdirTemp("", "sxir-nodepool-")
		if err != nil {
			p.mu.Unlock()
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(d, "worker.mjs"), []byte(nodePoolWorker), 0o644); err != nil {
			p.mu.Unlock()
			return nil, err
		}
		p.dir = d
	}
	dir := p.dir
	p.mu.Unlock()
	cmd := exec.Command(node, "--disable-warning=ExperimentalWarning", filepath.Join(dir, "worker.mjs"))
	cmd.Dir = dir
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("node pool: %w", err)
	}
	return &nodeWorker{cmd: cmd, in: in, out: bufio.NewReaderSize(outPipe, 1<<20)}, nil
}

type nodeWorker struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	jobs int
}

var nodeJobIDs atomic.Uint64

func (w *nodeWorker) run(t testing.TB, timeout time.Duration, dir, script string, args []string) (NodeResult, error) {
	w.jobs++
	id := nodeJobIDs.Add(1)
	line, err := json.Marshal(map[string]any{"id": id, "dir": dir, "script": script, "drainMs": NodeDrainTimeout.Milliseconds(), "args": append([]string{}, args...)})
	if err != nil {
		return NodeResult{}, err
	}
	if _, err := w.in.Write(append(line, '\n')); err != nil {
		return NodeResult{}, fmt.Errorf("node pool: the worker is gone: %w", err)
	}
	cpu0, _ := processCPU(w.cmd.Process.Pid)
	backstop := wallBackstop(t, timeout)
	done := make(chan nodeReply, 1)
	go func() {
		text, err := w.out.ReadBytes('\n')
		if err != nil {
			done <- nodeReply{err: fmt.Errorf("node pool: the worker ended without an answer: %w", err)}
			return
		}
		var r struct {
			ID       uint64
			Out, Err string
			Code     int
			Leak     string
		}
		if err := json.Unmarshal(text, &r); err != nil || r.ID != id {
			done <- nodeReply{err: fmt.Errorf("node pool: bad answer %q (%v)", text, err)}
			return
		}
		done <- nodeReply{res: NodeResult{Stdout: r.Out, Stderr: r.Err, Code: r.Code, Leak: r.Leak}}
	}()
	return w.await(done, cpu0, timeout, backstop, func(why string) error {
		return fmt.Errorf("node pool: %s in %s ran past %v and was killed: %s", script, dir, timeout, why)
	})
}

func (w *nodeWorker) stop() {
	w.in.Close()
	w.cmd.Process.Kill()
	w.cmd.Wait()
}

// NodeExitError is the error of a script that RunNodeScript ran and that failed: its exit code and error output.
type NodeExitError struct {
	Code   int
	Stderr string
}

func (e *NodeExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// NodeOutput runs the script argv[0] of the module in dir, with the arguments argv[1:], like the output of
// `cd dir && node argv...` (exec.Cmd.Output): it returns what the script printed to standard output, and
// an error if it failed (a NodeExitError that holds its error output) or the pool failed. See RunNodeScript for
// what a script must satisfy.
func NodeOutput(t testing.TB, node, dir string, argv ...string) ([]byte, error) {
	t.Helper()
	res, err := RunNodeScript(t, node, dir, argv[0], argv[1:]...)
	if err != nil {
		return []byte(res.Stdout), err
	}
	if res.Code != 0 {
		return []byte(res.Stdout), &NodeExitError{Code: res.Code, Stderr: res.Stderr}
	}
	return []byte(res.Stdout), nil
}

// NodeCombinedOutput is NodeOutput that returns the standard output followed by the error output, like
// exec.Cmd.CombinedOutput without the interleaving of the two.
func NodeCombinedOutput(t testing.TB, node, dir string, argv ...string) ([]byte, error) {
	t.Helper()
	res, err := RunNodeScript(t, node, dir, argv[0], argv[1:]...)
	out := []byte(res.Stdout + res.Stderr)
	if err != nil {
		return out, err
	}
	if res.Code != 0 {
		return out, &NodeExitError{Code: res.Code, Stderr: res.Stderr}
	}
	return out, nil
}

// NodeStderr returns the error output of the script whose failure is err (from NodeOutput or
// NodeCombinedOutput), or "" if err holds none.
func NodeStderr(err error) string {
	var e *NodeExitError
	if errors.As(err, &e) {
		return e.Stderr
	}
	return ""
}

type nodeReply struct {
	res NodeResult
	err error
}

// backstopFactor is how many times the timeout of a job the pool lets it run on the wall clock at most,
// whatever its CPU time: the backstop of a job that neither computes nor idles by the measures of hung.
const backstopFactor = 10

// wallBackstop returns the longest wall time a job with the timeout may take: backstopFactor times the
// timeout, and no later than a few seconds before the deadline of the test binary, so that a script that
// the load starves is reported as such and not by the timeout of `go test`, which kills every test.
func wallBackstop(t testing.TB, timeout time.Duration) time.Duration {
	limit := backstopFactor * timeout
	if d, ok := t.(interface{ Deadline() (time.Time, bool) }); ok {
		if at, ok := d.Deadline(); ok {
			limit = min(limit, max(time.Until(at)-10*time.Second, timeout))
		}
	}
	return limit
}

// hung decides whether a job that began at start is hung at now, from the CPU time that it has used
// since then (cpu), the last time its CPU time grew (progress) and the wall clock limit. It is hung when
//
//   - it has used timeout of CPU time: a loop that never ends computes all the time, wherever the load
//     puts it; or
//   - it has not used any CPU time for timeout of wall time: an await that never settles, or a
//     script that waits for nothing; or
//   - it has run backstop of wall time.
//
// A job that the load starves is none of these while it keeps getting some CPU time: its wall time says
// nothing about whether it is hung, but its CPU time does. It returns the reason, or "".
func hung(timeout, backstop time.Duration, start, now, progress time.Time, cpu time.Duration) string {
	switch {
	case cpu >= timeout:
		return fmt.Sprintf("it used %v of CPU time", cpu.Round(time.Millisecond))
	case now.Sub(progress) >= timeout:
		return fmt.Sprintf("it used no CPU time for %v", now.Sub(progress).Round(time.Millisecond))
	case now.Sub(start) >= backstop:
		return fmt.Sprintf("it ran for %v of wall time", now.Sub(start).Round(time.Millisecond))
	}
	return ""
}

// await waits for the answer of the job of w, killing the worker and failing the job with fail if it is
// hung (hung): the limit is the job's own CPU time, which a loaded machine does not stretch, with a
// wall clock backstop.
func (w *nodeWorker) await(done <-chan nodeReply, cpu0, timeout, backstop time.Duration, fail func(why string) error) (NodeResult, error) {
	start := time.Now()
	progress, last := start, cpu0
	tick := time.NewTicker(min(max(timeout/10, 100*time.Millisecond), 2*time.Second))
	defer tick.Stop()
	for {
		select {
		case r := <-done:
			return r.res, r.err
		case now := <-tick.C:
			cpu, err := processCPU(w.cmd.Process.Pid)
			if err != nil {
				// The worker is gone: its answer or its end is on the way.
				continue
			}
			if cpu > last {
				progress, last = now, cpu
			}
			if why := hung(timeout, backstop, start, now, progress, cpu-cpu0); why != "" {
				w.cmd.Process.Kill()
				return NodeResult{}, fail(why)
			}
		}
	}
}

// processCPU returns the CPU time that the process pid has used (user and system) as `ps` reports it,
// which does not depend on the load of the machine.
func processCPU(pid int) (time.Duration, error) {
	out, err := exec.Command("ps", "-o", "cputime=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	return parsePSTime(strings.TrimSpace(string(out)))
}

// parsePSTime parses the CPU time of ps: [[dd-]hh:]mm:ss[.ff].
func parsePSTime(s string) (time.Duration, error) {
	days := 0
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, fmt.Errorf("ps time %q", s)
		}
		days, s = n, rest
	}
	total := time.Duration(days) * 24 * time.Hour
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("ps time %q", s)
	}
	units := []time.Duration{time.Second, time.Minute, time.Hour}
	for i := range parts {
		p := parts[len(parts)-1-i]
		f, err := strconv.ParseFloat(p, 64)
		if err != nil || f < 0 {
			return 0, fmt.Errorf("ps time %q", s)
		}
		total += time.Duration(f * float64(units[i]))
	}
	return total, nil
}
