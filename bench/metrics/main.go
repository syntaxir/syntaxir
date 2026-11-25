// Command metrics collects the deterministic metrics of the repository and compares two reports
// of them (docs/performance.md, "Deterministic metrics"):
//
//	go run ./bench/metrics collect [-o FILE] [-parts LIST]   # write the report of this checkout
//	go run ./bench/metrics compare [-all] BASE HEAD          # judge HEAD against BASE
//
// Every metric is a count that is the same on every run of a commit, whatever the machine and its
// load, so a change of a value is a change of the code: compute units, allocations with the
// collector off, rule evaluations and memo hits, the doubling ratios of units and allocations,
// generated and built sizes, and whether generation is byte-identical across runs. No metric is a
// time. The report format is that of package internal/bench/metrics.
//
// collect runs these parts (-parts selects some, comma-separated):
//
//	workloads    TestMetrics of package bench: units, allocations, profile counts and doubling
//	             ratios of the benchmark workloads and of the complexity controls
//	sizes        the sizes of the generated Go and TypeScript parsers and their builds (as in
//	             bench/results-size.txt)
//	wasm         the size of the playground's WebAssembly binary, raw and gzip -9
//	determinism  whether two generations of each parser, one with GOMAXPROCS=1 and one with more,
//	             are byte-identical
//
// compare prints the changed metrics (all with -all) with their delta and exits with status 1 if
// any is a regression: an increase of an exact metric, an increase beyond the tolerance of one with
// a tolerance, a change of an equal metric, a value above a declared ceiling, or a metric of the
// base that the head lacks. A metric that only the head has is listed as new.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/bench/metrics"
	"syntaxir.dev/sxir/internal/bench/probe"
)

const usage = `usage:
  go run ./bench/metrics collect [-o FILE] [-parts workloads,sizes,wasm,determinism]
  go run ./bench/metrics compare [-all] BASE HEAD`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "collect":
		err = collectCmd(os.Args[2:])
	case "compare":
		var regressions int
		regressions, err = compareCmd(os.Args[2:])
		if err == nil && regressions > 0 {
			fmt.Fprintf(os.Stderr, "metrics: %d regressions\n", regressions)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "metrics:", err)
		os.Exit(1)
	}
}

func compareCmd(args []string) (int, error) {
	fl := flag.NewFlagSet("compare", flag.ExitOnError)
	all := fl.Bool("all", false, "list the unchanged metrics too")
	fl.Parse(args)
	if fl.NArg() != 2 {
		return 0, errors.New("compare takes the base and the head report")
	}
	base, err := readReport(fl.Arg(0))
	if err != nil {
		return 0, err
	}
	head, err := readReport(fl.Arg(1))
	if err != nil {
		return 0, err
	}
	return metrics.WriteRows(os.Stdout, metrics.Compare(base, head), *all)
}

func readReport(path string) ([]metrics.Metric, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ms, err := metrics.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return ms, nil
}

var allParts = []string{"workloads", "sizes", "wasm", "determinism"}

func collectCmd(args []string) error {
	fl := flag.NewFlagSet("collect", flag.ExitOnError)
	out := fl.String("o", "", "write the report to this file (default: standard output)")
	partsFlag := fl.String("parts", strings.Join(allParts, ","), "parts to collect, comma-separated")
	fl.Parse(args)
	parts := map[string]bool{}
	for _, p := range strings.Split(*partsFlag, ",") {
		if p = strings.TrimSpace(p); !slices.Contains(allParts, p) {
			return fmt.Errorf("-parts: unknown part %q (want %s)", p, strings.Join(allParts, ", "))
		}
		parts[p] = true
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "sxir-metrics-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	var ms []metrics.Metric
	if parts["workloads"] {
		m, err := collectWorkloads(root, work)
		if err != nil {
			return err
		}
		ms = append(ms, m...)
	}
	var sxirBin string
	if parts["sizes"] || parts["determinism"] {
		if sxirBin, err = probe.BuildSXIR(root, work); err != nil {
			return err
		}
	}
	if parts["sizes"] {
		m, err := collectSizes(root, sxirBin, work)
		if err != nil {
			return err
		}
		ms = append(ms, m...)
	}
	if parts["wasm"] {
		m, err := collectWasm(root, work)
		if err != nil {
			return err
		}
		ms = append(ms, m...)
	}
	if parts["determinism"] {
		m, err := collectDeterminism(root, sxirBin, work)
		if err != nil {
			return err
		}
		ms = append(ms, m...)
	}
	var b bytes.Buffer
	if err := metrics.Write(&b, header(root, *partsFlag), ms); err != nil {
		return err
	}
	if *out == "" {
		_, err = os.Stdout.Write(b.Bytes())
		return err
	}
	return os.WriteFile(*out, b.Bytes(), 0o644)
}

// header is the provenance of a report; compare ignores it.
func header(root, parts string) []string {
	commit, _ := probe.ExecOut(root, nil, "git", "rev-parse", "HEAD")
	status, _ := probe.ExecOut(root, nil, "git", "status", "--porcelain")
	dirty := "clean"
	if strings.TrimSpace(status) != "" {
		dirty = "dirty"
	}
	return []string{
		"sxir deterministic metrics (go run ./bench/metrics; docs/performance.md)",
		fmt.Sprintf("commit %s %s", strings.TrimSpace(commit), dirty),
		fmt.Sprintf("go %s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH),
		"parts " + parts,
	}
}

// collectWorkloads runs TestMetrics of package bench.
func collectWorkloads(root, work string) ([]metrics.Metric, error) {
	path := filepath.Join(work, "workloads.txt")
	cmd := exec.Command("go", "test", "./bench", "-run", "^TestMetrics$", "-count=1", "-timeout=60m", "-metrics.out="+path)
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go test ./bench -run TestMetrics: %w", err)
	}
	return readReport(path)
}

// collectSizes turns the lines of probe.WriteSizes into metrics.
func collectSizes(root, sxirBin, work string) ([]metrics.Metric, error) {
	var b bytes.Buffer
	if err := probe.WriteSizes(&b, root, sxirBin, work); err != nil {
		return nil, err
	}
	var ms []metrics.Metric
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 || f[0] != "size" {
			return nil, fmt.Errorf("unexpected size line %q", line)
		}
		n, err := strconv.ParseInt(f[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("size line %q: %v", line, err)
		}
		ms = append(ms, metrics.New(n, metrics.Policy{Kind: metrics.Lower}, f[:3]...))
	}
	return ms, nil
}

// collectWasm builds the playground's WebAssembly binary as the site does with the Go toolchain.
func collectWasm(root, work string) ([]metrics.Metric, error) {
	out := filepath.Join(work, "sxir.wasm")
	if _, err := probe.ExecOut(root, []string{"GOOS=js", "GOARCH=wasm"}, "go", "build", "-trimpath", "-ldflags=-s -w", "-o", out, "./cmd/playground"); err != nil {
		return nil, err
	}
	raw, gz, err := probe.SizeOfFile(out)
	if err != nil {
		return nil, err
	}
	lower := metrics.Policy{Kind: metrics.Lower}
	return []metrics.Metric{
		metrics.New(int64(raw), lower, "size", "playground", "wasm-raw"),
		metrics.New(int64(gz), lower, "size", "playground", "wasm-gzip"),
	}, nil
}

// collectDeterminism generates each parser of probe.Grammars twice in separate processes, once
// with GOMAXPROCS=1 and once with more, in Go and in TypeScript, and records whether the outputs are
// byte-identical (1) or not (0).
func collectDeterminism(root, sxirBin, work string) ([]metrics.Metric, error) {
	procs := []string{"GOMAXPROCS=1", "GOMAXPROCS=" + strconv.Itoa(max(runtime.NumCPU(), 4))}
	var ms []metrics.Metric
	for _, name := range probe.Grammars {
		for _, lang := range []string{"go", "ts"} {
			var dirs []string
			for i, env := range procs {
				dir := filepath.Join(work, "determinism", name, lang, strconv.Itoa(i))
				gen := probe.GenerateGoEnv
				if lang == "ts" {
					gen = probe.GenerateTSEnv
				}
				if err := gen([]string{env}, root, sxirBin, dir, name); err != nil {
					return nil, err
				}
				dirs = append(dirs, dir)
			}
			same, err := sameTree(dirs[0], dirs[1])
			if err != nil {
				return nil, err
			}
			v := int64(0)
			if same {
				v = 1
			}
			ms = append(ms, metrics.New(v, metrics.Policy{Kind: metrics.Equal}, "determinism", name, lang))
		}
	}
	return ms, nil
}

// sameTree reports whether two directories hold the same files with the same contents.
func sameTree(a, b string) (bool, error) {
	ta, err := readTree(a)
	if err != nil {
		return false, err
	}
	tb, err := readTree(b)
	if err != nil {
		return false, err
	}
	if len(ta) != len(tb) {
		return false, nil
	}
	for k, v := range ta {
		if w, ok := tb[k]; !ok || !bytes.Equal(v, w) {
			return false, nil
		}
	}
	return true, nil
}

func readTree(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files[rel] = data
		return nil
	})
	return files, err
}

// repoRoot returns the directory of the repository's go.mod.
func repoRoot() (string, error) {
	out, err := probe.ExecOut(".", nil, "go", "env", "GOMOD")
	if err != nil {
		return "", err
	}
	mod := strings.TrimSpace(out)
	if mod == "" || mod == os.DevNull {
		return "", errors.New("run go run ./bench/metrics inside the repository")
	}
	return filepath.Dir(mod), nil
}
