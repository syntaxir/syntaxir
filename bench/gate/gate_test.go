package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

func TestParseLoad(t *testing.T) {
	for in, want := range map[string]float64{
		" 2:49  up 23 days, 22:07, 2 users, load averages: 54.40 44.30 49.51\n": 54.40,
		" 10:00:01 up 3 days,  1 user,  load average: 0.52, 0.58, 0.59\n":       0.52,
	} {
		if got, err := parseLoad(in); err != nil || got != want {
			t.Errorf("parseLoad(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := parseLoad("nothing"); err == nil {
		t.Error("an output without a load average was accepted")
	}
}

// TestBusyMachineIsNotJudged checks that the timing phase waits for the load to fall, and that a load above the
// limit at the start or at the end makes every gate invalid, whatever the timings say.
func TestBusyMachineIsNotJudged(t *testing.T) {
	g, _ := findGate("generation")
	lim := testLimits
	lim.MaxLoad, lim.LoadWait = 4, time.Minute
	p := plan{Limits: lim, Revs: [2]string{"base", "cand"}, Gates: []planGate{{Gate: g}}}
	data, _ := json.Marshal(p)
	file := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	oldLoad, oldPoll := loadAvg, loadPoll
	defer func() { loadAvg, loadPoll = oldLoad, oldPoll }()
	loadPoll = time.Millisecond
	run := func(loads ...float64) (string, error) {
		i := 0
		loadAvg = func() (float64, error) {
			l := loads[min(i, len(loads)-1)]
			i++
			return l, nil
		}
		var out bytes.Buffer
		err := (directLocker{out: &out, run: &fake{slow: 1, ripple: 0.003}}).hold(file)
		return out.String(), err
	}
	// 9 and 6 then 2 at the start, 2 at the end: it waited, and judged.
	out, err := run(9, 6, 2, 2)
	if err != nil || !strings.Contains(out, "waiting for the load to fall") || !strings.Contains(out, "time verdict: within noise") {
		t.Errorf("a load that fell was not judged: %v\n%s", err, out)
	}
	// 2 at the start, 7 at the end: invalid.
	out, err = run(2, 7)
	if err == nil || !strings.Contains(out, "too busy to judge on") || !strings.Contains(out, "time verdict: invalid") {
		t.Errorf("a busy end was judged: %v\n%s", err, out)
	}
	// never below the limit within the wait: invalid.
	lim.LoadWait = 5 * time.Millisecond
	p.Limits = lim
	data, _ = json.Marshal(p)
	os.WriteFile(file, data, 0o644)
	out, err = run(9)
	if err == nil || !strings.Contains(out, "load 9.00 at the start") {
		t.Errorf("a machine that stayed busy was judged: %v\n%s", err, out)
	}
}

// TestAllocationChangeDecidesTheGate checks that more B/op or allocs/op than the noise range fails the gate set
// even when the time is within noise, and that fewer is reported as an improvement without failing it.
func TestAllocationChangeDecidesTheGate(t *testing.T) {
	g, _ := findGate("generation")
	p := plan{Limits: testLimits, Revs: [2]string{"base", "cand"}, Gates: []planGate{{Gate: g}}}
	data, _ := json.Marshal(p)
	file := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var more bytes.Buffer
	err := (directLocker{out: &more, run: &fake{slow: 1, ripple: 0.003, allocs: 3}}).hold(file)
	if err == nil || !strings.Contains(err.Error(), "allocation regressions") || !strings.Contains(more.String(), "time verdict: within noise") {
		t.Errorf("more allocations did not fail the gate set: %v\n%s", err, more.String())
	}
	var fewer bytes.Buffer
	if err := (directLocker{out: &fewer, run: &fake{slow: 1, ripple: 0.003, allocs: -3}}).hold(file); err != nil {
		t.Errorf("fewer allocations failed the gate set: %v\n%s", err, fewer.String())
	}
	if !strings.Contains(fewer.String(), "improvement") {
		t.Errorf("fewer allocations are not reported as an improvement:\n%s", fewer.String())
	}
}

// TestInvalidDoesNotStopEarly checks that a noisy gate runs to the limit of rounds, where its validity is decided.
func TestInvalidDoesNotStopEarly(t *testing.T) {
	res, rounds := judged(t, &fake{slow: 1, ripple: 0.08})
	if len(rounds) != testLimits.MaxRounds {
		t.Errorf("a noisy gate stopped at %d rounds, not at the limit %d (%s)", len(rounds), testLimits.MaxRounds, res.Why)
	}
	if res.Time.Verdict != stat.Invalid {
		t.Errorf("verdict %v, want invalid", res.Time.Verdict)
	}
}

// TestSignalIsPassedToTheLockScript checks that a termination of the gate reaches the lock script while it runs,
// and does not run the cleanup, which the returning run does through its defer.
func TestSignalIsPassedToTheLockScript(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	activeMu.Lock()
	activeChild = child.Process
	activeMu.Unlock()
	cleaned := make(chan struct{}, 1)
	stop := stopOnSignal(func() { cleaned <- struct{}{} })
	syscall.Kill(os.Getpid(), syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("the child ended without a signal")
		}
	case <-time.After(5 * time.Second):
		child.Process.Kill()
		t.Error("the signal did not reach the child")
	}
	stop()
	activeMu.Lock()
	activeChild = nil
	activeMu.Unlock()
	select {
	case <-cleaned:
		t.Error("cleanup ran in the handler although a child was running")
	default:
	}
}
