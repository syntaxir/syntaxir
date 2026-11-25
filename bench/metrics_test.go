package bench

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/bench/metrics"
	gensrc "syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/complexitytest"
	"syntaxir.dev/sxir/internal/testing/linearity"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// The deterministic metrics of the workloads, which go run ./bench/metrics collects through
// TestMetrics (docs/performance.md, "Deterministic metrics"). Every metric is a count that is the
// same on every run of a commit: compute units, allocations with the collector off (Allocs of
// package linearity), the profile's evaluations and memo hits, and ratios of these counts.

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

// Allocation counts are exact except on a few routes, where they vary between processes; the likely
// cause is the runtime's maps, whose table splits depend on the hash seed of the process. Ten A/A runs of one commit, each in its own
// process, varied only on the routes below, by at most 4 objects and 912 bytes for the typed
// parse of the generated parsers, 1 object and 16 bytes for the engine's closure backend, and 1
// thousandth for the allocation doubling ratio of a control on that backend. Their tolerance is
// twice the largest spread, rounded up to a power of two; every other count is exact
// (docs/performance.md, "Deterministic metrics").
var allocSlack = map[string]struct{ objects, bytes int64 }{
	"generated-go/parse-value": {8, 2048},
	"engine-closure/parse":     {2, 32},
}

const controlDoublingAllocsSlack = 2

// allocPolicies returns the policies of the objects and the bytes that a route allocates.
func allocPolicies(route string) (objects, bytes metrics.Policy) {
	s := allocSlack[route]
	return metrics.Policy{Kind: metrics.Lower, Slack: s.objects}, metrics.Policy{Kind: metrics.Lower, Slack: s.bytes}
}

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
	var ms []metrics.Metric
	for i := range workloads {
		ms = append(ms, workloadMetrics(t, &workloads[i])...)
	}
	ms = append(ms, tsUnits(t)...)
	ms = append(ms, controlMetrics(t)...)
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
	objects, bytes = linearity.Allocs(func() { ok, err = run() })
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
		op, bp := allocPolicies(r.name)
		ms = append(ms,
			metrics.New(int64(objects), op, "allocs", w.name, r.name),
			metrics.New(int64(bytes), bp, "alloc-bytes", w.name, r.name))
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
// Node.js.
func tsUnits(t *testing.T) []metrics.Metric {
	node := tooltest.RequireNode(t)
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
			n, _ := linearity.Allocs(func() { parse(input) })
			return n
		}
		us, ul := spent(small), spent(large)
		as, al := allocated(small), allocated(large)
		ms = append(ms,
			metrics.New(int64(ul), exact, "units", key, fmt.Sprint(c.Sizes[2])),
			metrics.New(metrics.Ratio(ul, us), ceiling(limit), "doubling-units", key),
			metrics.New(metrics.Ratio(al, as), metrics.Policy{Kind: metrics.Lower, Slack: controlDoublingAllocsSlack, HasMax: true, Max: limit}, "doubling-allocs", key, "engine-closure"))
	}
	return ms
}
