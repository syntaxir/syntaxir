package codegentest

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// nodePoolWorker is the script of the workers of the node pool.
//
//go:embed nodepool.mjs
var nodePoolWorker string

// NodeJobTimeout is how long RunNodeScript waits for one script before it kills the worker that runs it
// and fails the script as hung. It is a variable so that the tests of the pool can shorten it.
var NodeJobTimeout = 3 * time.Minute

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
// script must have finished all its output when its top-level code and the macrotasks it queued have
// run, must import only from its own directory and the built-in modules, must not read its standard
// input, and must not depend on the process (argv, the heap, timing): tests that measure the process run
// node themselves.
//
// A script must leave the worker as it found it. After every script the worker compares the own
// properties of the global object, of the built-in constructors and prototypes, of process (keys,
// environment, listeners) and of the modules fs, path, util and os, and the prototype and extensibility of
// each, with their state at its start; a difference fails the script with an error that lists it, and the
// worker is replaced, so a script that pollutes a global, or a mutant of the library that does, cannot
// change what another script sees.
func RunNodeScript(t testing.TB, node, dir, script string, args ...string) (NodeResult, error) {
	t.Helper()
	w, err := nodePool.acquire(node)
	if err != nil {
		return NodeResult{}, err
	}
	res, err := w.run(dir, script, args)
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

func (w *nodeWorker) run(dir, script string, args []string) (NodeResult, error) {
	w.jobs++
	id := nodeJobIDs.Add(1)
	line, err := json.Marshal(map[string]any{"id": id, "dir": dir, "script": script, "args": append([]string{}, args...)})
	if err != nil {
		return NodeResult{}, err
	}
	if _, err := w.in.Write(append(line, '\n')); err != nil {
		return NodeResult{}, fmt.Errorf("node pool: the worker is gone: %w", err)
	}
	type reply struct {
		res NodeResult
		err error
	}
	done := make(chan reply, 1)
	go func() {
		text, err := w.out.ReadBytes('\n')
		if err != nil {
			done <- reply{err: fmt.Errorf("node pool: the worker ended without an answer: %w", err)}
			return
		}
		var r struct {
			ID       uint64
			Out, Err string
			Code     int
			Leak     string
		}
		if err := json.Unmarshal(text, &r); err != nil || r.ID != id {
			done <- reply{err: fmt.Errorf("node pool: bad answer %q (%v)", text, err)}
			return
		}
		done <- reply{res: NodeResult{Stdout: r.Out, Stderr: r.Err, Code: r.Code, Leak: r.Leak}}
	}()
	select {
	case r := <-done:
		return r.res, r.err
	case <-time.After(NodeJobTimeout):
		w.cmd.Process.Kill()
		return NodeResult{}, fmt.Errorf("node pool: %s in %s ran past %v and was killed", script, dir, NodeJobTimeout)
	}
}

func (w *nodeWorker) stop() {
	w.in.Close()
	w.cmd.Process.Kill()
	w.cmd.Wait()
}
