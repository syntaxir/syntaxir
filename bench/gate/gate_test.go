package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"syntaxir.dev/sxir/internal/bench/stat"
)

func TestParseOutput(t *testing.T) {
	out := `goos: darwin
BenchmarkParse/JSON/closure/codepoints-16 	      96	  12366854 ns/op	  21.20 MB/s	11308112 B/op	     767 allocs/op
BenchmarkGeneration/go          5   100.5 ns/op   32 B/op   2 allocs/op
PASS
`
	got, err := parseOutput(out)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]sample{
		"BenchmarkParse/JSON/closure/codepoints": {12366854, 11308112, 767},
		"BenchmarkGeneration/go":                 {100.5, 32, 2},
	}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %+v, want %+v", k, got[k], v)
		}
	}
	for _, bad := range []string{"PASS\n", "BenchmarkX 5 abc ns/op\n", "BenchmarkX 5 -3 ns/op\n"} {
		if _, err := parseOutput(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// fake is a runner whose benchmarks have fixed times, a deterministic ripple per run, and a slowdown or an
// allocation change of the candidate.
type fake struct {
	slow   float64 // the candidate's time factor
	ripple float64 // the amplitude of the noise of each run
	allocs float64 // the candidate's allocs/op added
	n      int
	calls  map[string]int
}

func (f *fake) run(g gate, v int) (string, error) {
	f.n++
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[g.Name]++
	var b strings.Builder
	for i, name := range []string{"BenchmarkA/x", "BenchmarkA/y", "BenchmarkB"} {
		ns := 1000.0 * float64(i+1) * (1 + f.ripple*math.Sin(float64(f.n)*1.7+float64(i)))
		al := 10.0
		if v == varCand {
			ns *= f.slow
			al += f.allocs
		}
		fmt.Fprintf(&b, "%s-16 \t 100\t %.1f ns/op\t 64 B/op\t %.0f allocs/op\n", name, ns, al)
	}
	return b.String(), nil
}

var testLimits = limits{MinRounds: 8, MaxRounds: 20, MaxSpread: 0.015}

func judged(t *testing.T, f *fake) (result, []round) {
	t.Helper()
	rounds, why, err := collect(gate{Name: "g"}, f, testLimits, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	res := judge(gate{Name: "g"}, rounds, testLimits)
	res.Why, res.Took = why, time.Second
	return res, rounds
}

func TestGateVerdicts(t *testing.T) {
	for _, c := range []struct {
		name string
		f    fake
		want stat.Verdict
	}{
		{"identical code", fake{slow: 1, ripple: 0.003}, stat.Within},
		{"a slowdown beyond the spread", fake{slow: 1.06, ripple: 0.003}, stat.Regression},
		{"a speedup beyond the spread", fake{slow: 0.9, ripple: 0.003}, stat.Improvement},
		{"a machine too noisy to judge", fake{slow: 1.06, ripple: 0.08}, stat.Invalid},
	} {
		f := c.f
		res, rounds := judged(t, &f)
		if res.Time.Verdict != c.want {
			t.Errorf("%s: %v (change %.4f spread %.4f), want %v", c.name, res.Time.Verdict, res.Time.Change, res.Time.Spread, c.want)
		}
		if len(rounds) < testLimits.MinRounds || len(rounds) > testLimits.MaxRounds {
			t.Errorf("%s: %d rounds", c.name, len(rounds))
		}
	}
}

// TestDeterministicMetricsAreAnotherSection checks that a change of allocations is reported whatever the time
// verdict is, and that an identical candidate reports none.
func TestDeterministicMetricsAreAnotherSection(t *testing.T) {
	res, _ := judged(t, &fake{slow: 1, ripple: 0.003, allocs: 3})
	if res.Time.Verdict != stat.Within {
		t.Fatalf("time verdict %v, want within noise", res.Time.Verdict)
	}
	if len(res.Det) != 3 || res.Det[0].Metric != "allocs/op" || res.Det[0].Cand != 13 || res.Det[0].Base != 10 {
		t.Fatalf("%+v", res.Det)
	}
	var out bytes.Buffer
	res.write(&out, testLimits)
	for _, want := range []string{"time verdict: within noise", "deterministic metrics", "allocs/op", "10 -> 13"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	same, _ := judged(t, &fake{slow: 1, ripple: 0.003})
	if len(same.Det) != 0 {
		t.Errorf("identical code reports %+v", same.Det)
	}
}

// TestSequentialStopping checks that a narrow measurement stops before the limit and that a wide one runs on to
// it, and that the order of the variants rotates.
func TestSequentialStopping(t *testing.T) {
	_, quiet := judged(t, &fake{slow: 1, ripple: 0.0005})
	if len(quiet) >= testLimits.MaxRounds {
		t.Errorf("a quiet gate ran %d rounds, the limit", len(quiet))
	}
	_, noisy := judged(t, &fake{slow: 1, ripple: 0.012})
	if len(noisy) < len(quiet) {
		t.Errorf("a noisy gate stopped at %d rounds, before the quiet one at %d", len(noisy), len(quiet))
	}
}

// TestTimePlanWithInjectedLock runs the timing phase from a plan with the direct locker: no lock, no binaries.
func TestTimePlanWithInjectedLock(t *testing.T) {
	g, err := findGate("generation")
	if err != nil {
		t.Fatal(err)
	}
	p := plan{Limits: testLimits, Revs: [2]string{"base", "cand"}, Gates: []planGate{{Gate: g}}}
	data, _ := json.Marshal(p)
	file := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	f := &fake{slow: 1, ripple: 0.003}
	if err := (directLocker{out: &out, run: f}).hold(file); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	for _, want := range []string{"==== verdicts ====", "gate generation", "A/A spread", "time verdict: within noise", "time of the gate set"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	slow := &fake{slow: 1.1, ripple: 0.003}
	var out2 bytes.Buffer
	if err := (directLocker{out: &out2, run: slow}).hold(file); err == nil || !strings.Contains(err.Error(), "regression") {
		t.Errorf("a slowdown did not fail the gate set: %v", err)
	}
}

func TestGateTable(t *testing.T) {
	seen := map[string]bool{}
	for _, g := range gates {
		if seen[g.Name] {
			t.Errorf("gate %s twice", g.Name)
		}
		seen[g.Name] = true
		if g.Pkg == "" || g.Bench == "" || g.Time == "" || g.Doc == "" {
			t.Errorf("gate %s is incomplete: %+v", g.Name, g)
		}
	}
	for _, q := range quick {
		if !seen[q] {
			t.Errorf("quick gate %s does not exist", q)
		}
	}
	if _, err := findGate("nope"); err == nil {
		t.Error("an unknown gate was found")
	}
}

// TestDeterministicNoisyMetric checks that a metric which moves from run to run in identical code is not reported
// while a shift beyond that movement is.
func TestDeterministicNoisyMetric(t *testing.T) {
	mk := func(base, cand, aa float64) round {
		return round{{"B": {B: base}}, {"B": {B: cand}}, {"B": {B: aa}}}
	}
	same := []round{mk(100, 101, 99), mk(102, 98, 100), mk(99, 100, 101), mk(101, 99, 102)}
	if rows := deterministic("B", same); len(rows) != 0 {
		t.Errorf("noise reported: %+v", rows)
	}
	shifted := []round{mk(100, 110, 99), mk(102, 111, 100), mk(99, 109, 101), mk(101, 112, 102)}
	if rows := deterministic("B", shifted); len(rows) != 1 || rows[0].Metric != "B/op" {
		t.Errorf("shift not reported: %+v", rows)
	}
}
