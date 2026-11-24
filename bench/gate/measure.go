package main

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"syntaxir.dev/sxir/internal/bench/stat"
)

// The three variants that every round measures, in fresh processes: the base, the candidate and a second run of
// the base. The second run of the base is the A/A pair, whose difference from the first is what identical code
// does on this machine, in these rounds.
const (
	varBase = iota
	varCand
	varAA
	variants
)

var variantNames = [variants]string{"base", "candidate", "base again (A/A)"}

// sample is what one benchmark measured in one process.
type sample struct{ NS, B, Allocs float64 }

// round holds the samples of the three variants of one round, by benchmark name.
type round [variants]map[string]sample

// runner runs the benchmark binary of a variant for a gate and returns its output.
type runner interface {
	run(g gate, variant int) (string, error)
}

// parseOutput reads the benchmark lines of `go test -bench -benchmem`: the name without the GOMAXPROCS suffix, the
// iterations, and the pairs of value and unit. A benchmark that appears more than once keeps the last line.
func parseOutput(out string) (map[string]sample, error) {
	res := map[string]sample{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || !strings.HasPrefix(f[0], "Benchmark") {
			continue
		}
		if _, err := strconv.Atoi(f[1]); err != nil {
			continue
		}
		name := f[0]
		if i := strings.LastIndex(name, "-"); i > 0 {
			if _, err := strconv.Atoi(name[i+1:]); err == nil {
				name = name[:i]
			}
		}
		var s sample
		have := false
		for i := 2; i+1 < len(f); i += 2 {
			v, err := strconv.ParseFloat(f[i], 64)
			if err != nil {
				continue
			}
			switch f[i+1] {
			case "ns/op":
				s.NS, have = v, true
			case "B/op":
				s.B = v
			case "allocs/op":
				s.Allocs = v
			}
		}
		if !have {
			return nil, fmt.Errorf("benchmark line without ns/op: %q", sc.Text())
		}
		if s.NS <= 0 || math.IsNaN(s.NS) || math.IsInf(s.NS, 0) {
			return nil, fmt.Errorf("benchmark line with a bad time: %q", sc.Text())
		}
		res[name] = s
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("no benchmark ran:\n%s", out)
	}
	return res, nil
}

// limits are the rounds and the noise limit of a run.
type limits struct {
	MinRounds, MaxRounds int
	MaxSpread            float64
	// MaxLoad is the one-minute load average above which the machine is too busy to judge on: the timing phase
	// waits up to LoadWait for the load to fall to it before it starts, and judges nothing if the load at the end is
	// above it. Zero disables the check.
	MaxLoad  float64
	LoadWait time.Duration
}

// collect measures rounds of a gate until stat.Enough says so or MaxRounds is reached. The order of the
// variants rotates in every round, so that no variant always runs first (after a pause) or last.
func collect(g gate, r runner, lim limits, log io.Writer) ([]round, string, error) {
	var rounds []round
	var change, aa []float64
	for i := 0; i < lim.MaxRounds; i++ {
		var rd round
		for _, v := range stat.Rotate([]int{varBase, varCand, varAA}, i) {
			out, err := r.run(g, v)
			if err != nil {
				return rounds, "", fmt.Errorf("gate %s, round %d, %s: %w", g.Name, i+1, variantNames[v], err)
			}
			rd[v], err = parseOutput(out)
			if err != nil {
				return rounds, "", fmt.Errorf("gate %s, round %d, %s: %w", g.Name, i+1, variantNames[v], err)
			}
		}
		rounds = append(rounds, rd)
		c, a := roundRatio(rd, varCand), roundRatio(rd, varAA)
		change, aa = append(change, c), append(aa, a)
		fmt.Fprintf(log, "  round %d: candidate/base %.4f, base again/base %.4f\n", i+1, c, a)
		if stat.Enough(change, aa, lim.MinRounds, lim.MaxSpread) {
			if stat.Judge(change, aa, lim.MaxSpread).Verdict == stat.Invalid {
				return rounds, "the A/A spread is above the limit, which more rounds do not lower", nil
			}
			return rounds, "the interval of the change is narrow enough", nil
		}
	}
	return rounds, fmt.Sprintf("the %d rounds of the limit", lim.MaxRounds), nil
}

// roundRatio is the geometric mean over the benchmarks of the time of variant v divided by the time of the base,
// in one round. It is over the benchmarks that all three variants measured.
func roundRatio(rd round, v int) float64 {
	var rs []float64
	for name, b := range rd[varBase] {
		c, ok1 := rd[v][name]
		_, ok2 := rd[varAA][name]
		_, ok3 := rd[varCand][name]
		if ok1 && ok2 && ok3 && b.NS > 0 {
			rs = append(rs, c.NS/b.NS)
		}
	}
	return stat.Geomean(rs)
}

// elapsed is the time a function takes.
func elapsed(f func()) time.Duration {
	t := time.Now()
	f()
	return time.Since(t)
}
