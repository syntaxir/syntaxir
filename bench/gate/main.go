// Command gate judges a change against its base with the benchmark gates of docs/performance.md.
//
//	go run ./bench/gate                      # the quick set, the working tree against its merge base with origin/main
//	go run ./bench/gate -gate parse -base HEAD~3 -rounds 30
//	go run ./bench/gate -base HEAD           # an A/A run: the base against itself, to see the spread
//	go run ./bench/gate -list
//
// For every gate it builds the benchmark binary of the base and of the working tree outside the benchmark lock,
// then takes the lock and measures in rounds: each round runs the base, the candidate and the base again (the
// A/A pair) in fresh processes, in a rotated order. The per-round ratios of the candidate and of the A/A pair
// to the base give the verdict of the change and the spread of identical code (internal/bench/stat). The
// allocated bytes and the allocation counts are judged exactly, in a section of their own.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	if plan := os.Getenv(planEnv); plan != "" {
		// The timing phase: this process was started by the benchmark lock.
		if err := timePlan(plan, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "gate:", err)
			os.Exit(1)
		}
		return
	}
	var names stringList
	flag.Var(&names, "gate", "a gate to run (repeatable; default the quick set: "+strings.Join(quick, ", ")+")")
	base := flag.String("base", "", "the revision to compare with (default: the merge base of HEAD and origin/main)")
	rounds := flag.Int("rounds", 20, "the most rounds a gate runs")
	maxSpread := flag.Float64("max-spread", 0.015, "the A/A spread above which a gate is invalid, as a fraction")
	maxLoad := flag.Float64("max-load", defaultMaxLoad, "the one-minute load average above which the machine is too busy to judge on (0: no check)")
	loadWait := flag.Duration("load-wait", 5*time.Minute, "how long the timing phase waits for the load to fall to -max-load before it starts")
	list := flag.Bool("list", false, "list the gates and exit")
	flag.Parse()
	if *list {
		for _, g := range gates {
			mark := " "
			for _, q := range quick {
				if q == g.Name {
					mark = "*"
				}
			}
			fmt.Printf("%s %-16s %s\n", mark, g.Name, g.Doc)
		}
		fmt.Println("(* is in the quick set)")
		return
	}
	if len(names) == 0 {
		names = quick
	}
	if err := runGates(names, *base, limits{MinRounds: minRounds, MaxRounds: *rounds, MaxSpread: *maxSpread, MaxLoad: *maxLoad, LoadWait: *loadWait}); err != nil {
		fmt.Fprintln(os.Stderr, "gate:", err)
		os.Exit(1)
	}
}

// minRounds is the fewest rounds a gate runs before it may stop.
const minRounds = 8

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// planEnv names the environment variable that carries the path of the plan to the timing phase. The phase is
// the same program started again by the benchmark lock, so that only the measuring is held by it.
const planEnv = "SXIR_GATE_PLAN"

// plan is what the timing phase needs: the binaries to run and the limits.
type plan struct {
	Limits limits
	Revs   [2]string // the base and the candidate, for the header
	Gates  []planGate
}

type planGate struct {
	Gate             gate
	BaseBin, CandBin string
	BaseDir, CandDir string // the directories the binaries run in (the package directories of their trees)
}

// runGates builds, takes the lock and times.
func runGates(names []string, baseRev string, lim limits) error {
	root, err := git("", "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if baseRev == "" {
		if baseRev, err = git(root, "merge-base", "HEAD", "origin/main"); err != nil {
			return fmt.Errorf("no base given and no merge base with origin/main: %w", err)
		}
	}
	sel := make([]gate, 0, len(names))
	for _, n := range names {
		g, err := findGate(n)
		if err != nil {
			return err
		}
		sel = append(sel, g)
	}
	work, err := os.MkdirTemp("", "sxir-gate-")
	if err != nil {
		return err
	}
	// cleanup removes the temporary base worktree and the work directory, once, whether the run ends or is
	// interrupted (see stopOnSignal).
	tree := &baseTree{root: root, work: work}
	cleanup := tree.cleanup
	defer cleanup()
	defer stopOnSignal(cleanup)()
	// The candidate is the working tree, untracked files included: it is the base itself only when HEAD is the base
	// and nothing is modified or untracked.
	head, _ := git(root, "rev-parse", "HEAD")
	baseSHA, _ := git(root, "rev-parse", baseRev)
	same := head == baseSHA && !dirty(root)
	if !same {
		dir := filepath.Join(work, "base")
		// The worktree exists from the add on, so it is set first: an interrupt during the add removes it.
		tree.setBase(dir)
		if _, err := git(root, "worktree", "add", "--detach", dir, baseRev); err != nil {
			return err
		}
	}
	p := plan{Limits: lim}
	p.Revs[0], _ = git(root, "rev-parse", "--short", baseRev)
	p.Revs[1], _ = git(root, "rev-parse", "--short", "HEAD")
	if !same || dirty(root) {
		p.Revs[1] += " plus the working tree"
	}
	built := map[string]string{}
	build := func(tree, tag, pkg string) (string, error) {
		key := tag + " " + pkg
		if b, ok := built[key]; ok {
			return b, nil
		}
		out := filepath.Join(work, strings.ReplaceAll(pkg, "/", "_")+"."+tag+".test")
		// The build takes a test slot, as every build of more than a few seconds does (testslot.sh), and none of the
		// benchmark lock: it is the timing phase that is exclusive. Inside a slot job it runs directly.
		args := []string{"go", "test", "-c", "-o", out, "./" + pkg}
		if slot := filepath.Join(root, ".agents", "scripts", "testslot.sh"); os.Getenv("SXIR_IN_TESTSLOT") == "" && fileExists(slot) {
			args = append([]string{slot}, args...)
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tree
		if b, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("building %s of %s: %v\n%s", pkg, tag, err, b)
		}
		built[key] = out
		return out, nil
	}
	fmt.Fprintf(os.Stderr, "building the benchmark binaries (in a test slot, outside the benchmark lock)\n")
	for _, g := range sel {
		br := tree.base()
		pg := planGate{Gate: g, BaseDir: filepath.Join(br, g.Pkg), CandDir: filepath.Join(root, g.Pkg)}
		if pg.BaseBin, err = build(br, "base", g.Pkg); err != nil {
			return err
		}
		if same {
			pg.CandBin = pg.BaseBin
		} else if pg.CandBin, err = build(root, "cand", g.Pkg); err != nil {
			return err
		}
		p.Gates = append(p.Gates, pg)
	}
	planFile := filepath.Join(work, "plan.json")
	data, _ := json.Marshal(p)
	if err := os.WriteFile(planFile, data, 0o644); err != nil {
		return err
	}
	return execLocker{root: root}.hold(planFile)
}

// baseTree is the base worktree of a run and the directory of its work, which cleanup removes once, from the
// goroutine that ends the run or from the one that handles a signal. The worktree's path is written by the first
// and read by the second, so a mutex guards it.
type baseTree struct {
	root, work string // the tree under test, and the temporary directory
	once       sync.Once
	mu         sync.Mutex
	dir        string // the base worktree; empty while it is the tree under test
}

func (t *baseTree) setBase(dir string) {
	t.mu.Lock()
	t.dir = dir
	t.mu.Unlock()
}

// base returns the tree that holds the base revision: the worktree, or the tree under test when it is the base.
func (t *baseTree) base() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dir == "" {
		return t.root
	}
	return t.dir
}

func (t *baseTree) cleanup() {
	t.once.Do(func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.dir != "" {
			exec.Command("git", "-C", t.root, "worktree", "remove", "--force", t.dir).Run()
		}
		os.RemoveAll(t.work)
	})
}

// activeChild is the benchmark-lock script while it runs; a signal is passed on to it.
var (
	activeMu    sync.Mutex
	activeChild *os.Process
)

// stopOnSignal makes an interrupt or a termination end the run cleanly: it is passed on to the lock script, which
// stops the timing phase, and this program returns through its deferred cleanup; while no timing phase runs, it
// cleans up and exits. It returns the function that stops listening.
func stopOnSignal(cleanup func()) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		sig, ok := <-ch
		if !ok {
			return
		}
		activeMu.Lock()
		child := activeChild
		activeMu.Unlock()
		if child != nil {
			child.Signal(syscall.SIGTERM)
			return
		}
		cleanup()
		code := 130
		if sig == syscall.SIGTERM {
			code = 143
		}
		os.Exit(code)
	}()
	return func() { signal.Stop(ch); close(ch) }
}

func dirty(root string) bool {
	out, _ := git(root, "status", "--porcelain")
	return out != ""
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// uptime returns the load of the machine for the header; the landing rule records it with every run.
func uptime() string {
	out, err := exec.Command("uptime").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// timestamp is the start of the timing phase, for the header.
func timestamp() string { return time.Now().Format("2006-01-02 15:04:05") }

// defaultMaxLoad is the one-minute load average above which a run is not judged: the background that the team's
// own idle tools produce (see docs/performance.md) sits below it.
const defaultMaxLoad = 4.0

// loadAvg is the one-minute load average; a variable so that tests can replace it.
var loadAvg = func() (float64, error) {
	out, err := exec.Command("uptime").Output()
	if err != nil {
		return 0, err
	}
	return parseLoad(string(out))
}

// parseLoad reads the one-minute load average from the output of uptime ("load averages: 1.20 2.30 3.40" or
// "load average: 1.20, 2.30, 3.40").
func parseLoad(uptimeOut string) (float64, error) {
	i := strings.Index(uptimeOut, "load average")
	if i < 0 {
		return 0, fmt.Errorf("no load average in %q", strings.TrimSpace(uptimeOut))
	}
	rest := uptimeOut[i:]
	rest = rest[strings.Index(rest, ":")+1:]
	f := strings.Fields(strings.ReplaceAll(rest, ",", " "))
	if len(f) == 0 {
		return 0, fmt.Errorf("no load average in %q", strings.TrimSpace(uptimeOut))
	}
	return strconv.ParseFloat(f[0], 64)
}
