// Package linearity measures whether the cost of some work grows linearly with its input, with
// measures that do not depend on the time or the load of the machine, so that a test that uses
// them gives the same verdict on every run, also while other tests run beside it.
//
// The measures are counts: the compute units that a parser charges (LeastLimit), the objects and
// bytes that the work allocates with the collector off (Count of package internal/bench/allocs, exact
// under the conditions that it states), and any counter of the work that a test reads. Linear compares two such counts of the
// same work at two sizes.
package linearity

import (
	"testing"
)

// Factor is how many times larger the large input of a comparison is than the small one, and
// Limit is the largest ratio of their costs that counts as linear: a linear cost grows Factor times
// and a quadratic one Factor squared times, and Limit is near the middle of the two on a
// logarithmic scale.
const (
	Factor = 8
	Limit  = 24
)

// Linear reports the ratio of large, the cost of the work at Factor times the size, to small, the
// cost at the size, and whether it is at most Limit. A cost of 0 at the small size counts as 1.
func Linear(small, large uint64) (ratio float64, ok bool) {
	ratio = float64(large) / float64(max(small, 1))
	return ratio, ratio <= Limit
}

// LeastLimit returns the least limit under which completes reports that the work completed, for
// work that a parser charges to one counter and rejects at the charge that passes a limit, such as
// the Compute limit of a parse. The count is exact: it is the same on every run and under any load.
//
// completes runs the work under limit and reports whether it completed or stopped at the limit; it
// must end the test on any other outcome, since work that ends early for another reason would look
// complete at every limit. The search verifies the failing side of the interval (the limit 1 must
// fail, or the result is 1, and every lower bound that it keeps was run and failed) and stops the
// test at a limit that no run completes under instead of doubling for ever.
func LeastLimit(t testing.TB, completes func(limit uint64) bool) uint64 {
	t.Helper()
	lo := uint64(1) // lo fails; hi completes
	if completes(lo) {
		return lo
	}
	hi := uint64(1 << 10)
	for !completes(hi) {
		if hi >= 1<<50 {
			t.Fatalf("no run completes under the limit %d", hi)
		}
		lo, hi = hi, hi*2
	}
	for hi-lo > 1 {
		if mid := lo + (hi-lo)/2; completes(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}
