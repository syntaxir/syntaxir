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
	"path/filepath"
	"strings"
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
	if err := runGates(names, *base, limits{MinRounds: minRounds, MaxRounds: *rounds, MaxSpread: *maxSpread}); err != nil {
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
	defer os.RemoveAll(work)
	same := exec.Command("git", "diff", "--quiet", baseRev, "--").Run() == nil
	baseRoot := root
	if !same {
		baseRoot = filepath.Join(work, "base")
		if _, err := git(root, "worktree", "add", "--detach", baseRoot, baseRev); err != nil {
			return err
		}
		defer exec.Command("git", "-C", root, "worktree", "remove", "--force", baseRoot).Run()
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
		cmd := exec.Command("go", "test", "-c", "-o", out, "./"+pkg)
		cmd.Dir = tree
		if b, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("building %s of %s: %v\n%s", pkg, tag, err, b)
		}
		built[key] = out
		return out, nil
	}
	fmt.Fprintf(os.Stderr, "building the benchmark binaries (outside the benchmark lock)\n")
	for _, g := range sel {
		pg := planGate{Gate: g, BaseDir: filepath.Join(baseRoot, g.Pkg), CandDir: filepath.Join(root, g.Pkg)}
		if pg.BaseBin, err = build(baseRoot, "base", g.Pkg); err != nil {
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

func dirty(root string) bool {
	out, _ := git(root, "status", "--porcelain", "--untracked-files=no")
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
