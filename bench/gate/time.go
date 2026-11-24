package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"syntaxir.dev/sxir/internal/bench/stat"
)

// timePlan is the timing phase: it reads the plan and measures every gate with the binaries of the plan. It runs
// under the benchmark lock.
func timePlan(planFile string, out io.Writer) error {
	return timePlanWith(planFile, out, binaryRunner{})
}

// binaryRunner runs the test binaries of the plan, for the gate and the variant: the base and its second run use
// the base binary, in the directory of the base tree.
type binaryRunner struct{ gates map[string]planGate }

func (r binaryRunner) with(p plan) binaryRunner {
	r.gates = map[string]planGate{}
	for _, g := range p.Gates {
		r.gates[g.Gate.Name] = g
	}
	return r
}

func (r binaryRunner) run(g gate, variant int) (string, error) {
	pg := r.gates[g.Name]
	bin, dir := pg.BaseBin, pg.BaseDir
	if variant == varCand {
		bin, dir = pg.CandBin, pg.CandDir
	}
	cmd := exec.Command(bin, "-test.run=^$", "-test.bench="+g.Bench, "-test.benchmem", "-test.benchtime="+g.Time, "-test.count=1", "-test.timeout=0")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v\n%s", err, out)
	}
	return string(out), nil
}

func timePlanWith(planFile string, out io.Writer, r runner) error {
	data, err := os.ReadFile(planFile)
	if err != nil {
		return err
	}
	var p plan
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	if br, ok := r.(binaryRunner); ok {
		r = br.with(p)
	}
	fmt.Fprintf(out, "benchmark gates: base %s, candidate %s\n", p.Revs[0], p.Revs[1])
	startLoad, err := waitForLoad(out, p.Limits)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "started %s, load: %s\n", timestamp(), uptime())
	var results []result
	total := time.Duration(0)
	for _, pg := range p.Gates {
		fmt.Fprintf(out, "\nmeasuring gate %s\n", pg.Gate.Name)
		var rounds []round
		var why string
		took := elapsed(func() { rounds, why, err = collect(pg.Gate, r, p.Limits, out) })
		if err != nil {
			return err
		}
		res := judge(pg.Gate, rounds, p.Limits)
		res.Took, res.Why = took, why
		total += took
		results = append(results, res)
	}
	fmt.Fprintf(out, "\nfinished %s, load: %s\n", timestamp(), uptime())
	endLoad, _ := loadAvg()
	if busy := busyReason(p.Limits, startLoad, endLoad); busy != "" {
		fmt.Fprintf(out, "\nthe machine was too busy to judge on: %s\n", busy)
		for i := range results {
			results[i].Time.Verdict = stat.Invalid
			results[i].Busy = busy
		}
	}
	fmt.Fprintf(out, "\n==== verdicts ====\n")
	var bad []string
	for _, res := range results {
		fmt.Fprintln(out)
		res.write(out, p.Limits)
		if v := res.Time.Verdict; v == stat.Regression || v == stat.Invalid {
			bad = append(bad, res.Gate.Name+": "+v.String())
		}
		if n := res.detRegressions(); n > 0 {
			bad = append(bad, fmt.Sprintf("%s: %d allocation regressions (B/op or allocs/op outside the noise range)", res.Gate.Name, n))
		}
	}
	fmt.Fprintf(out, "\ntime of the gate set: %v\n", total.Round(time.Second))
	if len(bad) > 0 {
		return fmt.Errorf("verdicts that fail the gate set: %s", strings.Join(bad, "; "))
	}
	return nil
}

// loadPoll is how often waitForLoad reads the load; a variable so that tests can shorten it.
var loadPoll = 10 * time.Second

// waitForLoad waits, for at most lim.LoadWait, until the one-minute load average is at most lim.MaxLoad, and
// returns the last reading. The lock is held meanwhile, so the slot jobs that were running when it was taken have
// finished and nothing new starts through testslot.sh: the wait is for the load average, which lags, to follow.
func waitForLoad(out io.Writer, lim limits) (float64, error) {
	if lim.MaxLoad <= 0 {
		return 0, nil
	}
	deadline := time.Now().Add(lim.LoadWait)
	for {
		l, err := loadAvg()
		if err != nil {
			return 0, fmt.Errorf("reading the load average: %w", err)
		}
		if l <= lim.MaxLoad || !time.Now().Before(deadline) {
			fmt.Fprintf(out, "load at the start: %.2f (limit %.2f)\n", l, lim.MaxLoad)
			return l, nil
		}
		fmt.Fprintf(out, "waiting for the load to fall: %.2f, limit %.2f\n", l, lim.MaxLoad)
		time.Sleep(loadPoll)
	}
}

// busyReason is why a run cannot be judged on its load, or "" when it can.
func busyReason(lim limits, start, end float64) string {
	if lim.MaxLoad <= 0 {
		return ""
	}
	if start > lim.MaxLoad || end > lim.MaxLoad {
		return fmt.Sprintf("load %.2f at the start and %.2f at the end, limit %.2f", start, end, lim.MaxLoad)
	}
	return ""
}
