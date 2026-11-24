// Package stat holds the statistics of the benchmark tools: the geometric mean of ratios, the median, the
// bootstrap interval of a statistic of paired rounds, the rotation of the order in which the variants of a
// round are measured, and the judge that decides whether a change stands out from the A/A spread.
//
// Every tool that compares a change with a base (bench/gate, bench/profilematrix, bench/report) uses
// these, so that one definition of the spread and of the verdict applies to all.
package stat

import (
	"math"
	"math/rand/v2"
	"slices"
)

// Geomean returns the geometric mean of the ratios. It is 1 for no ratios.
func Geomean(r []float64) float64 {
	if len(r) == 0 {
		return 1
	}
	var sum float64
	for _, x := range r {
		sum += math.Log(x)
	}
	return math.Exp(sum / float64(len(r)))
}

// Median returns the median of xs, or 0 for none. It does not change xs.
func Median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	if n := len(s); n%2 == 1 {
		return s[n/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// Rotate returns the items starting at index round modulo their number, so that each item is in every
// position over the rounds.
func Rotate[T any](items []T, round int) []T {
	if len(items) == 0 {
		return nil
	}
	k := round % len(items)
	return append(slices.Clone(items[k:]), items[:k]...)
}

// Interval is a confidence interval of a statistic.
type Interval struct{ Lo, Hi float64 }

// Contains reports whether x lies in the interval.
func (i Interval) Contains(x float64) bool { return i.Lo <= x && x <= i.Hi }

// HalfWidth is half the width of the interval.
func (i Interval) HalfWidth() float64 { return (i.Hi - i.Lo) / 2 }

// resamples is the number of bootstrap resamples. The seed is fixed, so that the same rounds always give the
// same interval.
const resamples = 2000

// Bootstrap returns the 95% percentile bootstrap interval of the median of xs, which are per-round values (a
// ratio of paired measurements, for example). With fewer than two values it is the degenerate interval of the
// value.
func Bootstrap(xs []float64) Interval {
	switch len(xs) {
	case 0:
		return Interval{1, 1}
	case 1:
		return Interval{xs[0], xs[0]}
	}
	rng := rand.New(rand.NewPCG(1, 2))
	meds := make([]float64, resamples)
	sample := make([]float64, len(xs))
	for i := range meds {
		for j := range sample {
			sample[j] = xs[rng.IntN(len(xs))]
		}
		meds[i] = Median(sample)
	}
	slices.Sort(meds)
	return Interval{meds[int(0.025*resamples)], meds[int(0.975*resamples)-1]}
}

// Verdict is the judgment of a change against its A/A spread.
type Verdict int

const (
	// Within means the change does not stand out from the noise of identical runs.
	Within Verdict = iota
	// Regression means the candidate is slower (or larger) beyond the spread.
	Regression
	// Improvement means the candidate is faster (or smaller) beyond the spread.
	Improvement
	// Invalid means the A/A spread is over the limit, so no verdict can be given.
	Invalid
)

func (v Verdict) String() string {
	return [...]string{"within noise", "regression", "improvement", "invalid (A/A spread too large)"}[v]
}

// Judgment is what Judge reports for one gate.
type Judgment struct {
	Change  float64  // the median per-round geomean of candidate/base, as a ratio
	CI      Interval // its bootstrap interval
	Spread  float64  // the A/A spread: the larger distance of the A/A interval from 1, as a fraction
	Verdict Verdict
}

// Judge decides whether the candidate differs from the base, from the per-round ratios candidate/base (change)
// and base'/base (aa), where base' is a second run of the identical base binary, both measured in the same
// rounds. The spread is the half-width of the interval of the A/A ratios and its distance from 1: the largest
// deviation that identical code showed. A change counts only if its whole interval lies beyond 1 by more than
// the spread (then it is a Regression above 1 and an Improvement below), so that noise that identical code
// also showed cannot pass. If the spread is over maxSpread (a fraction, 0.015 is the landing rule), the
// machine was too noisy and the verdict is Invalid.
func Judge(change, aa []float64, maxSpread float64) Judgment {
	ci := Bootstrap(change)
	aaCI := Bootstrap(aa)
	spread := max(aaCI.HalfWidth(), math.Abs(aaCI.Lo-1), math.Abs(aaCI.Hi-1))
	j := Judgment{Change: Median(change), CI: ci, Spread: spread}
	switch {
	case spread > maxSpread:
		j.Verdict = Invalid
	case ci.Lo > 1+spread:
		j.Verdict = Regression
	case ci.Hi < 1-spread:
		j.Verdict = Improvement
	}
	return j
}

// Precision is the half-width of the interval of the change, relative to the change, at which Enough stops: the
// change is then known to within half a percent.
const Precision = 0.005

// Enough reports whether the rounds so far suffice. It is a rule on the width of the interval, never on where the
// change lies: with at least minRounds rounds, it holds when the half-width of the interval of the change,
// relative to the change, is at most Precision. The rule does not look at whether the change is beyond 1 or the
// spread, so it is a precision rule and not a significance test that stops when the result happens to look
// significant: for per-round ratios with similar relative scatter whatever their location, the relative width does
// not depend on the location, so stopping on it does not bias the verdict toward a change. It does not stop on an
// invalid A/A spread either: the spread of a few rounds is itself noisy (with 1% scatter per process, about half of
// the 8-round runs of a quiet machine would look invalid), so validity is decided at the end of the rounds.
func Enough(change, aa []float64, minRounds int, maxSpread float64) bool {
	if len(change) < minRounds || len(aa) < minRounds {
		return false
	}
	j := Judge(change, aa, maxSpread)
	return j.CI.HalfWidth()/j.Change <= Precision
}
