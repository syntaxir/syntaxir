package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"syntaxir.dev/sxir/internal/bench/stat"
)

// benchRow is the time of one benchmark across the rounds.
type benchRow struct {
	Name    string
	Change  float64 // median of candidate/base
	AAMed   float64 // median of base again/base
	Verdict stat.Verdict
}

// detRow is a deterministic metric of one benchmark that the change moved beyond what the A/A pair moved.
type detRow struct {
	Name, Metric       string
	Base, Cand, AABase float64
}

// result is the judgment of one gate.
type result struct {
	Gate   gate
	Rounds int
	Why    string
	Took   time.Duration
	Time   stat.Judgment
	Rows   []benchRow // benchmarks whose time stands out from the spread, largest change first
	Total  int        // benchmarks compared
	Det    []detRow
	Busy   string // why the machine was too busy to judge on, or empty
}

// judge turns the rounds of a gate into its judgment.
func judge(g gate, rounds []round, lim limits) result {
	res := result{Gate: g, Rounds: len(rounds)}
	var change, aa []float64
	names := map[string]bool{}
	for _, rd := range rounds {
		change, aa = append(change, roundRatio(rd, varCand)), append(aa, roundRatio(rd, varAA))
	}
	if len(rounds) > 0 {
		for n := range rounds[0][varBase] {
			names[n] = true
		}
	}
	res.Time = stat.Judge(change, aa, lim.MaxSpread)
	for _, name := range slices.Sorted(maps.Keys(names)) {
		var c, a []float64
		for _, rd := range rounds {
			b, ok1 := rd[varBase][name]
			x, ok2 := rd[varCand][name]
			y, ok3 := rd[varAA][name]
			if ok1 && ok2 && ok3 && b.NS > 0 {
				c, a = append(c, x.NS/b.NS), append(a, y.NS/b.NS)
			}
		}
		if len(c) == 0 {
			continue
		}
		res.Total++
		j := stat.Judge(c, a, 1) // a per-benchmark spread has no validity limit: it only bounds the noise
		if j.Verdict != stat.Within {
			res.Rows = append(res.Rows, benchRow{name, j.Change, stat.Median(a), j.Verdict})
		}
		res.Det = append(res.Det, deterministic(name, rounds)...)
	}
	slices.SortFunc(res.Rows, func(x, y benchRow) int {
		return cmpFloat(abs(y.Change-1), abs(x.Change-1))
	})
	return res
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// deterministic compares the allocated bytes and the allocation counts of one benchmark. They do not depend on
// timing, so the medians over the rounds are compared as they are, with no interval. A metric changed when the
// candidate's median lies outside the range that the base and the base again took over the rounds: identical code
// that already moves it (a pool, a map, the garbage collector) sets that range, and when it does not move, any
// difference counts.
func deterministic(name string, rounds []round) []detRow {
	var rows []detRow
	for _, m := range []struct {
		name string
		pick func(sample) float64
	}{{"B/op", func(s sample) float64 { return s.B }}, {"allocs/op", func(s sample) float64 { return s.Allocs }}} {
		series := func(v int) []float64 {
			var xs []float64
			for _, rd := range rounds {
				if s, ok := rd[v][name]; ok {
					xs = append(xs, m.pick(s))
				}
			}
			return xs
		}
		base, cand, aa := series(varBase), series(varCand), series(varAA)
		if len(base) == 0 || len(cand) == 0 {
			continue
		}
		both := append(slices.Clone(base), aa...)
		c := stat.Median(cand)
		if c < slices.Min(both) || c > slices.Max(both) {
			rows = append(rows, detRow{name, m.name, stat.Median(base), c, stat.Median(aa)})
		}
	}
	return rows
}

// write prints the judgment: the time verdict with its spread, then the benchmarks that stand out, then the
// deterministic metrics in a section of their own, so that "within noise" for the time never hides a change of
// allocations.
func (r result) write(w io.Writer, lim limits) {
	fmt.Fprintf(w, "gate %s (%s)\n", r.Gate.Name, r.Gate.Doc)
	fmt.Fprintf(w, "  %d rounds in %v; stopped at: %s\n", r.Rounds, r.Took.Round(time.Second), r.Why)
	j := r.Time
	fmt.Fprintf(w, "  time, geomean of %d benchmarks: candidate/base %.4f (95%% interval %.4f to %.4f)\n", r.Total, j.Change, j.CI.Lo, j.CI.Hi)
	fmt.Fprintf(w, "  A/A spread: %.2f%% (limit %.2f%%)\n", 100*j.Spread, 100*lim.MaxSpread)
	fmt.Fprintf(w, "  time verdict: %s\n", j.Verdict)
	if r.Busy != "" {
		fmt.Fprintf(w, "  invalid because the machine was busy: %s\n", r.Busy)
	}
	if j.Verdict != stat.Invalid && len(r.Rows) > 0 {
		fmt.Fprintf(w, "  benchmarks beyond their own A/A spread (%d of %d):\n", len(r.Rows), r.Total)
		for i, row := range r.Rows {
			if i == 10 {
				fmt.Fprintf(w, "    ... %d more\n", len(r.Rows)-i)
				break
			}
			fmt.Fprintf(w, "    %-60s %.3f  (%s)\n", row.Name, row.Change, row.Verdict)
		}
	}
	fmt.Fprintf(w, "  deterministic metrics (B/op, allocs/op), changed when the candidate's median is outside the range of the base and the base again over the rounds:\n")
	if len(r.Det) == 0 {
		fmt.Fprintf(w, "    no change\n")
		return
	}
	for _, d := range r.Det {
		fmt.Fprintf(w, "    %-60s %-9s %s -> %s (base again %s): %s\n", d.Name, d.Metric, num(d.Base), num(d.Cand), num(d.AABase), d.word())
	}
}

// Regression is whether the candidate allocates more than the base: that fails the gate, as a slowdown does.
func (d detRow) Regression() bool { return d.Cand > d.Base }

func (d detRow) word() string {
	if d.Regression() {
		return "regression"
	}
	return "improvement"
}

// detRegressions counts the deterministic metrics that rose.
func (r result) detRegressions() int {
	n := 0
	for _, d := range r.Det {
		if d.Regression() {
			n++
		}
	}
	return n
}

func num(x float64) string {
	s := fmt.Sprintf("%.1f", x)
	return strings.TrimSuffix(s, ".0")
}
