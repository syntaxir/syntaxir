package bench

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/libsrc"
	"syntaxir.dev/sxir/internal/bench/metrics"
	"syntaxir.dev/sxir/internal/bench/synth"
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/driver"
	gensrc "syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/complexitytest"
	"syntaxir.dev/sxir/internal/testing/linearity"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// The deterministic metrics of the workloads, which go run ./bench/metrics collects through
// TestMetrics (docs/performance.md, "Deterministic metrics"). Every metric is a count that is the
// same on every run of a commit with one Go release and target: compute units, allocations with the
// collector off (leastAllocs), the profile's evaluations and memo hits, the allocations and steps
// of the compiler on synthetic grammars, and ratios of these counts.

var metricsOut = flag.String("metrics.out", "", "write the metrics of TestMetrics to this file (go run ./bench/metrics runs it)")

// linearDoublingMax is the largest cost ratio of a doubling that counts as linear, 2^1.15 in
// thousandths: the band of a linear slope in package complexitytest (Classify). polyDoublingMax is
// the band of Poly(k), 2^(k+0.2).
const linearDoublingMax = 2219

func polyDoublingMax(k int) int64 { return int64(math.Floor(math.Pow(2, float64(k)+0.2) * 1000)) }

var (
	exact = metrics.Policy{Kind: metrics.Lower}
	info  = metrics.Policy{Kind: metrics.Info}
	same  = metrics.Policy{Kind: metrics.Equal}
)

// leastAllocs returns the objects and bytes that a run of work allocates (Allocs of package
// linearity): the least count of allocRounds measurements, with the bytes of that measurement, after
// warm runs of work.
//
// The runtime adds allocations of its own to a run now and then, at random: type-assertion cache
// rebuilds, background timers and new threads (Allocs of package linearity lists them). Each only
// adds to a count, and GOMAXPROCS is 1 while TestMetrics runs, so the least count of several runs
// after the warm ones is the count of the work alone. The caches fill slowly: in 30 parses of the
// Minilang workload into typed values, the first ten rebuilt a cache in about half the runs and the
// last ten in about one in ten, hence the warm runs. A single measurement varied by up to 4 objects
// between processes; this count did not vary in six A/A runs.
func leastAllocs(warm int, work func()) (objects, bytes uint64) {
	for range warm {
		work()
	}
	objects = math.MaxUint64
	for range allocRounds {
		if o, b := linearity.Allocs(work); o < objects {
			objects, bytes = o, b
		}
	}
	return objects, bytes
}

// parseWarmRuns is the number of warm runs of a parse before leastAllocs measures it.
const parseWarmRuns = 20

const allocRounds = 5

func ceiling(limit int64) metrics.Policy {
	return metrics.Policy{Kind: metrics.Lower, HasMax: true, Max: limit}
}

func TestMetrics(t *testing.T) {
	if *metricsOut == "" {
		t.Skip("writes the deterministic metrics report; go run ./bench/metrics runs it with -metrics.out")
	}
	if !state.BudgetControlsEnabled {
		t.Fatal("the compute units need the engine's budget controls, which this build leaves out")
	}
	// One P: the per-P arrays of a sync.Pool, which a parse allocates again after the collections
	// of Allocs have emptied its pools, are as long as GOMAXPROCS, so the bytes would depend on the
	// machine's processors.
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	var ms []metrics.Metric
	for i := range workloads {
		ms = append(ms, workloadMetrics(t, &workloads[i])...)
	}
	ms = append(ms, tsUnits(t)...)
	ms = append(ms, controlMetrics(t)...)
	ms = append(ms, compilerMetrics(t)...)
	f, err := os.Create(*metricsOut)
	if err != nil {
		t.Fatal(err)
	}
	if err := metrics.Write(f, nil, ms); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// route is one way to parse a workload's input: the engine on a backend or the generated parser.
type route struct {
	name string
	run  func(input string, opts ...sxirruntime.ParseOption) (bool, error)
}

func routes(p *engine.Parser, w *workload) []route {
	var rs []route
	for _, be := range backends {
		rs = append(rs, route{"engine-" + be.name, func(s string, o ...sxirruntime.ParseOption) (bool, error) {
			n, err := p.Parse(s, engine.WithBackend(be.b), engine.Use(o...))
			return n != nil, err
		}})
	}
	return append(rs, route{"generated-go", w.run})
}

// parsed fails the test unless the parse succeeded, or returned a tree for a workload that recovers.
func parsed(t *testing.T, w *workload, what string, ok bool, err error) {
	t.Helper()
	if err != nil && !(w.partial && ok) {
		t.Fatalf("%s %s: %v", w.name, what, err)
	}
}

// spentUnits returns the compute units of a parse.
func spentUnits(t *testing.T, w *workload, r route, input string) uint64 {
	t.Helper()
	var n uint64
	ok, err := r.run(input, sxirruntime.WithComputeReport(&n))
	parsed(t, w, r.name, ok, err)
	if n == 0 {
		t.Fatalf("%s %s: no units reported", w.name, r.name)
	}
	return n
}

// allocs returns the objects and bytes that a parse allocates.
func allocs(t *testing.T, w *workload, name string, run func() (bool, error)) (objects, bytes uint64) {
	t.Helper()
	var ok bool
	var err error
	objects, bytes = leastAllocs(parseWarmRuns, func() { ok, err = run() })
	parsed(t, w, name, ok, err)
	return objects, bytes
}

// doubling returns the cost ratio of the full input to the half one, in thousandths, scaled to an
// exact doubling of the length: the inputs of a workload are built to a size and are not exactly
// twice as long.
func doubling(full, half uint64, fullLen, halfLen int) int64 {
	return int64(math.Floor(float64(full) / float64(max(half, 1)) * float64(halfLen) * 2 / float64(fullLen) * 1000))
}

func workloadMetrics(t *testing.T, w *workload) []metrics.Metric {
	p := load(t, w.grammar)
	full, half := w.input(1), w.input(2)
	ms := []metrics.Metric{metrics.New(int64(len(full)), same, "input", w.name, "bytes")}
	rs := routes(p, w)
	// D1: compute units, on every route.
	for _, r := range rs {
		n := spentUnits(t, w, r, full)
		ms = append(ms,
			metrics.New(int64(n), exact, "units", w.name, r.name),
			metrics.New(metrics.Ratio(n, uint64(len(full))), info, "units-per-byte-milli", w.name, r.name))
	}
	// D2: allocations of a parse, on the engine and the generated parser, with each API of the
	// generated parser.
	type allocRun struct {
		name string
		run  func() (bool, error)
	}
	var runs []allocRun
	for _, r := range rs {
		runs = append(runs, allocRun{r.name + "/parse", func() (bool, error) { return r.run(full) }})
	}
	runs = append(runs,
		allocRun{"generated-go/parse-value", func() (bool, error) { err := w.ast(full); return err == nil, err }},
		allocRun{"generated-go/recognize", func() (bool, error) { err := w.rec(full); return err == nil, err }})
	for _, r := range runs {
		if w.partial && strings.HasSuffix(r.name, "/recognize") {
			// Recognition of a recovering parse reports the recovered errors; it still runs to the end.
			run := r.run
			r.run = func() (bool, error) { _, err := run(); return true, err }
		}
		objects, bytes := allocs(t, w, r.name, r.run)
		ms = append(ms,
			metrics.New(int64(objects), exact, "allocs", w.name, r.name),
			metrics.New(int64(bytes), exact, "alloc-bytes", w.name, r.name))
	}
	// D7: the profile of the parse on the engine (the closure backend; the counts are the same on
	// every backend).
	prof := new(instrument.Profile)
	ok, err := rs[0].run(full, sxirruntime.WithProfile(prof))
	parsed(t, w, "profile", ok, err)
	s := prof.Snapshot()
	ms = append(ms,
		metrics.New(int64(s.Calls), exact, "rule-calls", w.name),
		metrics.New(int64(s.Evals), exact, "evaluations", w.name),
		metrics.New(metrics.Ratio(uint64(s.Evals), uint64(len(full))), info, "evaluations-per-byte-milli", w.name),
		metrics.New(int64(s.MemoHits), info, "memo-hits", w.name),
		metrics.New(metrics.Ratio(uint64(s.MemoHits), uint64(s.Calls)), info, "memo-hit-permille", w.name))
	// D5: how the units and the allocations grow when the input doubles.
	gen := rs[len(rs)-1]
	uf, uh := spentUnits(t, w, rs[0], full), spentUnits(t, w, rs[0], half)
	af, _ := allocs(t, w, "generated-go/parse", func() (bool, error) { return gen.run(full) })
	ah, _ := allocs(t, w, "generated-go/parse", func() (bool, error) { return gen.run(half) })
	ms = append(ms,
		metrics.New(doubling(uf, uh, len(full), len(half)), ceiling(linearDoublingMax), "doubling-units", w.name),
		metrics.New(doubling(af, ah, len(full), len(half)), ceiling(linearDoublingMax), "doubling-allocs", w.name, "generated-go"))
	return ms
}

// tsUnits returns the compute units of the generated TypeScript parser of each workload, run with
// Node.js. A report without them would compare as missing metrics, so Node is required even under
// SKIP_NODE, which skips only tests.
func tsUnits(t *testing.T) []metrics.Metric {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("the TypeScript units need Node.js 22.18 or later on the PATH: %v", err)
	}
	var ms []metrics.Metric
	for i := range workloads {
		w := &workloads[i]
		source, err := os.ReadFile(w.grammar)
		if err != nil {
			t.Fatal(err)
		}
		g, err := parser.Parse(string(source))
		if err != nil {
			t.Fatal(err)
		}
		files, err := gensrc.TypeScript(g, common.GenOptions{Types: true, Recognize: true, Start: "main"})
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(dir, f.Path), f.Content, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for name, data := range map[string]string{
			"package.json": `{"type":"module"}`,
			"input.txt":    w.input(1),
			"units.mjs": `import { readFileSync } from "node:fs";
const p = await import("./parser.ts");
const input = readFileSync(new URL("./input.txt", import.meta.url), "utf8");
const report = { units: 0 };
const r = p.parseWithOptions(input, { computeReport: report });
console.log(JSON.stringify({ units: report.units, tree: r.node !== null, error: r.error === null ? "" : String(r.error.message) }));
`,
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command(node, "units.mjs")
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				t.Fatalf("%s TypeScript: %v\n%s", w.name, err, ee.Stderr)
			}
			t.Fatalf("%s TypeScript: %v", w.name, err)
		}
		var res struct {
			Units uint64
			Tree  bool
			Error string
		}
		if err := json.Unmarshal(out, &res); err != nil {
			t.Fatalf("%s TypeScript: %v: %s", w.name, err, out)
		}
		if res.Error != "" && !(w.partial && res.Tree) || res.Units == 0 {
			t.Fatalf("%s TypeScript: %+v", w.name, res)
		}
		ms = append(ms,
			metrics.New(int64(res.Units), exact, "units", w.name, "generated-ts"),
			metrics.New(metrics.Ratio(res.Units, uint64(len(w.input(1)))), info, "units-per-byte-milli", w.name, "generated-ts"))
	}
	return ms
}

var nonWord = regexp.MustCompile(`[^A-Za-z0-9]+`)

// controlMetrics returns the doubling ratios of the units and the allocations of the controls of
// package complexitytest, grammars of known growth, between the last two sizes of each control's
// series (the slope that Classify judges), on the engine's closure backend. Exponential and
// capped controls are left out: their cost stops at the cap.
func controlMetrics(t *testing.T) []metrics.Metric {
	var ms []metrics.Metric
	for _, c := range complexitytest.Controls() {
		var limit int64
		switch c.Order.Kind {
		case complexitytest.KindLinear:
			limit = linearDoublingMax
		case complexitytest.KindPoly:
			limit = polyDoublingMax(c.Order.Degree)
		default:
			continue
		}
		if c.Capped {
			continue
		}
		p, err := complexitytest.Compile(c.Source)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		key := "control/" + strings.Trim(nonWord.ReplaceAllString(c.Name, "-"), "-")
		small, large := c.Input(c.Sizes[1]), c.Input(c.Sizes[2])
		parse := func(input string, o ...sxirruntime.ParseOption) {
			o = append(o, sxirruntime.WithLimits(sxirruntime.Limits{Compute: complexitytest.Cap}))
			if _, err := p.Parse(input, engine.WithBackend(engine.Closure), engine.Use(o...)); err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
		}
		spent := func(input string) uint64 {
			var n uint64
			parse(input, sxirruntime.WithComputeReport(&n))
			return n
		}
		allocated := func(input string) uint64 {
			n, _ := leastAllocs(parseWarmRuns, func() { parse(input) })
			return n
		}
		us, ul := spent(small), spent(large)
		as, al := allocated(small), allocated(large)
		ms = append(ms,
			metrics.New(int64(ul), exact, "units", key, fmt.Sprint(c.Sizes[2])),
			metrics.New(metrics.Ratio(ul, us), ceiling(limit), "doubling-units", key),
			metrics.New(metrics.Ratio(al, as), ceiling(limit), "doubling-allocs", key, "engine-closure"))
	}
	return ms
}

// compilerShapes are the series of synthetic grammars whose compilation compilerMetrics measures:
// each doubles one dimension of a shape (package synth) from n to 2n and to 4n.
var compilerShapes = []struct {
	name  string
	n     int
	shape func(n int) synth.Shape
}{
	{"rules", 40, func(n int) synth.Shape { return synth.Shape{Rules: n, Alts: 4, Depth: 2, LeftRecursion: 4, Seed: 1} }},
	{"alternatives", 6, func(n int) synth.Shape { return synth.Shape{Rules: 16, Alts: n, Depth: 2, LeftRecursion: 4, Seed: 2} }},
	{"depth", 6, func(n int) synth.Shape { return synth.Shape{Rules: 16, Alts: 4, Depth: n, LeftRecursion: 4, Seed: 3} }},
}

// compilerMetrics returns, for each series of compilerShapes, the allocations and the steps of
// the generation of a Go parser at n, 2n and 4n, and the doubling ratio of each between the
// increments (incrementRatio), which a cost that the compiler pays once whatever the grammar does
// not dilute. A linear compiler has a ratio of 2 and the same ceiling as a linear parse.
//
// The steps are the compiler's unit of work (package cancel): every visit of an element by the
// check, the analyses, the lowerings and the printers, fixed-point rounds included, counted by a
// meter in the context of the generation. The steps of a compilation are the same on every run
// whatever the scheduling, since the meter only adds.
func compilerMetrics(t *testing.T) []metrics.Metric {
	var ms []metrics.Metric
	for _, c := range compilerShapes {
		key := "synthetic-" + c.name
		var objects, bytes, steps, sizes [3]uint64
		for i := range 3 {
			n := c.n << i
			src := synth.Grammar(c.shape(n))
			sizes[i] = uint64(len(src))
			g, err := parser.Parse(src)
			if err != nil {
				t.Fatalf("%s n=%d: %v", key, n, err)
			}
			generate := func(ctx context.Context) {
				target := driver.Target{Backend: result.Go, Gen: common.GenOptions{Module: "example.com/synth", LibGo: libsrc.FS}}
				if _, err := gensrc.TargetsContext(ctx, driver.CheckForGenerator, g, driver.Options{}, target); err != nil {
					t.Fatalf("%s n=%d: %v", key, n, err)
				}
			}
			objects[i], bytes[i] = leastAllocs(0, func() { generate(context.Background()) })
			meter := new(cancel.Meter)
			generate(cancel.WithMeter(context.Background(), meter))
			steps[i] = meter.Steps()
			size := fmt.Sprintf("n=%d", n)
			ms = append(ms,
				metrics.New(int64(len(src)), same, "input", key, size, "bytes"),
				metrics.New(int64(objects[i]), compileAllocsPolicy, "compile-allocs", key, size),
				metrics.New(int64(bytes[i]), compileAllocBytesPolicy, "compile-alloc-bytes", key, size),
				metrics.New(int64(steps[i]), exact, "compile-steps", key, size))
		}
		ms = append(ms,
			metrics.New(incrementRatio(objects, sizes), ceiling(linearDoublingMax), "compile-doubling-allocs", key),
			metrics.New(incrementRatio(steps, sizes), ceiling(linearDoublingMax), "compile-doubling-steps", key))
	}
	return ms
}

// The allocations of a compilation vary between runs, and leastAllocs does not remove the variation:
// the compiler fills maps large enough that the runtime splits their tables, at points that depend
// on the random hash seed of each map, so a run allocates a few more or fewer tables, and their
// groups, than another. A memory profile of six compilations of one grammar in one process showed
// no other allocation that varied. Twelve A/A runs, each in its own process, varied by at most
// 0.0005% of the objects and 0.205% of the bytes; the tolerances are those spreads rounded up. The
// doubling ratios did not vary and are exact.
var (
	compileAllocsPolicy     = metrics.Policy{Kind: metrics.Lower, Tolerance: 0.001}
	compileAllocBytesPolicy = metrics.Policy{Kind: metrics.Lower, Tolerance: 0.3}
)

// incrementRatio returns the ratio of the increments of the costs c of a series at n, 2n and 4n,
// (c[2] - c[1]) / (c[1] - c[0]), in thousandths, scaled to an exact doubling of the grammar's text
// whose sizes are size, as doubling scales a workload's: the rules, alternatives and groups that a
// series adds are drawn at random, so the text from 2n to 4n is not exactly twice what it is from n
// to 2n. It is 0 for costs or sizes that do not grow.
func incrementRatio(c, size [3]uint64) int64 {
	if c[2] < c[1] || c[1] <= c[0] || size[2] <= size[1] || size[1] <= size[0] {
		return 0
	}
	return int64(math.Floor(float64(c[2]-c[1]) / float64(c[1]-c[0]) * float64(size[1]-size[0]) * 2 / float64(size[2]-size[1]) * 1000))
}
