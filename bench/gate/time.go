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
	fmt.Fprintf(out, "\n==== verdicts ====\n")
	var bad []string
	for _, res := range results {
		fmt.Fprintln(out)
		res.write(out, p.Limits)
		if v := res.Time.Verdict; v == stat.Regression || v == stat.Invalid {
			bad = append(bad, res.Gate.Name+": "+v.String())
		}
	}
	fmt.Fprintf(out, "\ntime of the gate set: %v\n", total.Round(time.Second))
	if len(bad) > 0 {
		return fmt.Errorf("verdicts that fail the gate set: %s", strings.Join(bad, "; "))
	}
	return nil
}
