// Command metrics collects the deterministic metrics of the repository and compares two reports
// of them (docs/performance.md, "Deterministic metrics"):
//
//	go run ./bench/metrics collect [-o FILE] [-parts LIST]   # write the report of this checkout
//	go run ./bench/metrics compare [-all] BASE HEAD          # judge HEAD against BASE
//
// Every metric is a count that is the same on every run of a commit with one Go release and target,
// whatever the machine's load and processors, so a change of a value is a change of the code:
// compute units, allocations with the collector off, rule evaluations and memo hits, the heap that
// compiled parsers and Document versions retain, the allocations and steps of the compiler on
// synthetic grammars, the doubling ratios of these counts, generated and built sizes, and whether
// generation is byte-identical across runs. No metric is a time. The report format is that of package internal/bench/metrics.
//
// collect runs these parts (-parts selects some, comma-separated; settings only when named):
//
//	workloads    TestMetrics of package bench: units, allocations, profile counts and doubling
//	             ratios of the benchmark workloads and of the complexity controls
//	sizes        the sizes of the generated Go and TypeScript parsers and their builds (the
//	             sizes that the published benchmark results show)
//	wasm         the size of the playground's WebAssembly binary, raw and gzip -9
//	retained     the live heap that compiled parsers (bench/retained), Document versions
//	             (bench/snapshot-retained) and a parsed Document against its tree
//	             (bench/document-retained) keep after collections, each in a process of its own
//	             with GOGC=off and GOMAXPROCS=1
//	determinism  the files that differ between two generations of each parser, one with
//	             GOMAXPROCS=1 and one with more; any is a regression whatever the base
//	settings     only when -parts names it (settings.go): for each profile and each registered
//	             flag set against it, the change of the compiler steps, sizes, allocations and
//	             retention of the generated parsers from the profile's own, and the grammars
//	             whose parsers the setting leaves byte-identical; all info
//
// The workloads part runs the generated TypeScript parsers, so it needs Node.js 22.18 or later on the
// PATH; collect refuses to start it without one.
//
// compare refuses two reports whose toolchain lines (Go release and target) differ, since
// allocation counts and sizes depend on both. It prints the changed metrics (all with -all) with
// their delta and exits with status 1 if any is a regression: an increase of an exact metric, an
// increase beyond the tolerance of one with a tolerance, a change of an equal metric, a value above
// a declared ceiling, a metric of the base that the head lacks, or a change of a metric's policy.
// Each metric is judged by the base's policy, so a head cannot loosen the policy that judges it,
// and a ceiling holds whatever the base's value. A metric that only the head has is listed as new.
package main

import (
	"bytes"
	"encoding/json"
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
  go run ./bench/metrics collect [-o FILE] [-parts workloads,sizes,wasm,retained,determinism,settings]
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
	if err := metrics.Comparable(base, head); err != nil {
		return 0, err
	}
	return metrics.WriteRows(os.Stdout, metrics.Compare(base.Metrics, head.Metrics), *all)
}

func readReport(path string) (metrics.Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return metrics.Report{}, err
	}
	defer f.Close()
	r, err := metrics.Parse(f)
	if err != nil {
		return metrics.Report{}, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

// allParts are the parts that -parts selects from; defaultParts are those that collect runs without it.
var (
	allParts     = []string{"workloads", "sizes", "wasm", "retained", "determinism", "settings"}
	defaultParts = []string{"workloads", "sizes", "wasm", "retained", "determinism"}
)

func collectCmd(args []string) error {
	fl := flag.NewFlagSet("collect", flag.ExitOnError)
	out := fl.String("o", "", "write the report to this file (default: standard output)")
	partsFlag := fl.String("parts", strings.Join(defaultParts, ","), "parts to collect, comma-separated: "+strings.Join(allParts, ", "))
	fl.Parse(args)
	parts := map[string]bool{}
	for _, p := range strings.Split(*partsFlag, ",") {
		if p = strings.TrimSpace(p); !slices.Contains(allParts, p) {
			return fmt.Errorf("-parts: unknown part %q (want %s)", p, strings.Join(allParts, ", "))
		}
		parts[p] = true
	}
	if parts["workloads"] {
		// TestMetrics measures the units of the generated TypeScript parsers under Node.js; without
		// Node it would fail, or skip under SKIP_NODE and leave no report, only after the Go parts.
		if _, err := exec.LookPath("node"); err != nil {
			return fmt.Errorf("the workloads part runs the generated TypeScript parsers and needs Node.js 22.18 or later on the PATH (%v); install it, or leave the part out with -parts", err)
		}
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
	if parts["retained"] {
		m, err := collectRetained(root, work)
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
	if parts["settings"] {
		m, err := collectSettings(root, work)
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

// header is the provenance of a report; compare checks only its toolchain line.
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
		metrics.Toolchain(),
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
	r, err := readReport(path)
	return r.Metrics, err
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

// retainedCorpora are the corpora of bench/retained.
var retainedCorpora = []string{"json", "xml", "arithmetic", "minilang"}

// collectRetained runs bench/retained for each corpus and bench/snapshot-retained in each mode, every
// run in a process of its own. GOGC=off keeps the collector to the collections that the commands
// force, and GOMAXPROCS=1 keeps the goroutine on one P, so that no per-P cache holds a value in one
// run and not in another.
func collectRetained(root, work string) ([]metrics.Metric, error) {
	env := []string{"GOGC=off", "GOMAXPROCS=1"}
	bins := map[string]string{}
	for _, cmd := range []string{"retained", "snapshot-retained", "document-retained"} {
		bins[cmd] = filepath.Join(work, cmd)
		if _, err := probe.ExecOut(root, nil, "go", "build", "-o", bins[cmd], "./bench/"+cmd); err != nil {
			return nil, err
		}
	}
	var ms []metrics.Metric
	add := func(what []string, out string, bytesPolicy, objectsPolicy metrics.Policy) error {
		var r struct {
			Bytes   *int64 `json:"retained_bytes"`
			Objects *int64 `json:"retained_objects"`
		}
		if err := json.Unmarshal([]byte(out), &r); err != nil || r.Bytes == nil || r.Objects == nil {
			return fmt.Errorf("%s: unexpected output %q (%v)", strings.Join(what, " "), out, err)
		}
		ms = append(ms,
			metrics.New(*r.Bytes, bytesPolicy, append([]string{"retained-bytes"}, what...)...),
			metrics.New(*r.Objects, objectsPolicy, append([]string{"retained-objects"}, what...)...))
		return nil
	}
	for _, corpus := range retainedCorpora {
		out, err := probe.ExecOut(root, env, bins["retained"], "-repo", root, "-corpus", corpus, "-count", strconv.Itoa(retainedParsers))
		if err != nil {
			return nil, err
		}
		if err := add([]string{"compiled-parsers", corpus, "x" + strconv.Itoa(retainedParsers)}, out, parserBytesPolicy, parserObjectsPolicy); err != nil {
			return nil, err
		}
	}
	for _, mode := range []string{"snapshot", "clone"} {
		out, err := probe.ExecOut(root, env, bins["snapshot-retained"], "-mode", mode, "-versions", strconv.Itoa(retainedVersions))
		if err != nil {
			return nil, err
		}
		lower := metrics.Policy{Kind: metrics.Lower}
		if err := add([]string{"document-versions", mode, "x" + strconv.Itoa(retainedVersions)}, out, lower, lower); err != nil {
			return nil, err
		}
	}
	// The Document memory against the tree: the heap that a parsed Document keeps over the heap of the
	// tree that Parse returns for the same text.
	lines := strconv.Itoa(documentLines)
	for _, subject := range documentSubjects {
		policy := documentPolicies[subject]
		kept := map[string]int64{}
		for _, what := range []string{"tree", "document"} {
			out, err := probe.ExecOut(root, env, bins["document-retained"], "-subject", subject, "-what", what, "-lines", lines)
			if err != nil {
				return nil, err
			}
			n := len(ms)
			if err := add([]string{"document-memory", subject, what, lines + "-lines"}, out, policy[0], policy[1]); err != nil {
				return nil, err
			}
			kept[what] = ms[n].Value
		}
		ms = append(ms, metrics.New(metrics.Ratio(uint64(kept["document"]), uint64(kept["tree"])), policy[2],
			"document-memory-permille", subject, lines+"-lines"))
	}
	return ms, nil
}

const (
	retainedParsers  = 128     // compiled parsers that bench/retained keeps per corpus
	retainedVersions = 64      // Document versions that bench/snapshot-retained keeps per mode
	documentLines    = 100_000 // lines of the texts of bench/document-retained
)

// documentSubjects are the subjects of bench/document-retained: the engine's Document on the grammar
// of the incremental parsing guide, and the generated Documents of the edit-latency benchmarks.
var documentSubjects = []string{"settings", "csv", "minilang"}

// The heap that compiled parsers retain varies between processes, as the allocations of the
// compiler do (compileAllocsPolicy in bench/metrics_test.go): the only allocations of a compilation
// that vary between runs are the tables of large maps, which the runtime splits at points that
// depend on the random hash seed of each map, and the parsers keep some of those maps. Twelve A/A
// runs, each in its own process, varied by at most 0.080% of the bytes and 0.052% of the objects;
// the tolerances are those spreads rounded up. The heap of the Document versions did not vary and is
// exact.
var (
	parserBytesPolicy   = metrics.Policy{Kind: metrics.Lower, Tolerance: 0.1}
	parserObjectsPolicy = metrics.Policy{Kind: metrics.Lower, Tolerance: 0.1}
)

// documentPolicies judge the heap of a Document and of its tree (bytes and objects), and their ratio,
// per subject. The generated Documents did not vary in five runs, each in its own process, and are
// exact. The engine's varies as compiled parsers do, since the parser that it compiles in the process
// keeps maps whose tables depend on the hash seed: five runs varied by at most 0.002% of the bytes and
// 0.18% of the objects; the tolerances are those spreads rounded up, and the ratio, in thousandths,
// gets a tolerance that a change of the bytes within theirs cannot exceed.
var documentPolicies = map[string][3]metrics.Policy{
	"settings": {{Kind: metrics.Lower, Tolerance: 0.01}, {Kind: metrics.Lower, Tolerance: 0.3}, {Kind: metrics.Lower, Tolerance: 0.1}},
	"csv":      {{Kind: metrics.Lower}, {Kind: metrics.Lower}, {Kind: metrics.Lower}},
	"minilang": {{Kind: metrics.Lower}, {Kind: metrics.Lower}, {Kind: metrics.Lower}},
}

// collectDeterminism generates each parser of probe.Grammars twice in separate processes, once
// with GOMAXPROCS=1 and once with more, in Go and in TypeScript, and records the number of files
// that differ between the two outputs, or that only one has. The ceiling of 0 makes the metric
// absolute: a nondeterministic generation fails the comparison with any base, also one that was
// nondeterministic itself.
func collectDeterminism(root, sxirBin, work string) ([]metrics.Metric, error) {
	procs := []string{"GOMAXPROCS=1", "GOMAXPROCS=" + strconv.Itoa(max(runtime.NumCPU(), 4))}
	never := metrics.Policy{Kind: metrics.Lower, HasMax: true, Max: 0}
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
			n, err := differingFiles(dirs[0], dirs[1])
			if err != nil {
				return nil, err
			}
			ms = append(ms, metrics.New(int64(n), never, "nondeterministic-files", name, lang))
		}
	}
	return ms, nil
}

// differingFiles returns the number of files that two directories do not hold with the same
// contents: those whose contents differ and those that only one holds.
func differingFiles(a, b string) (int, error) {
	ta, err := readTree(a)
	if err != nil {
		return 0, err
	}
	tb, err := readTree(b)
	if err != nil {
		return 0, err
	}
	n := 0
	for k, v := range ta {
		if w, ok := tb[k]; !ok || !bytes.Equal(v, w) {
			n++
		}
	}
	for k := range tb {
		if _, ok := ta[k]; !ok {
			n++
		}
	}
	return n, nil
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
