// Package linearity measures whether the cost of some work grows linearly with its input, with
// measures that do not depend on the time or the load of the machine, so that a test that uses
// them gives the same verdict on every run, also while other tests run beside it.
//
// The measures are counts: the compute units that a parser charges (LeastLimit), the objects and
// bytes that the work allocates with the collector off (Allocs, exact under the conditions that it
// states), and any counter of the work that a test reads. Linear compares two such counts of the
// same work at two sizes.
package linearity

import (
	"runtime"
	"runtime/debug"
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

// Allocs returns the number of objects and of bytes that a run of work allocates. The work runs
// once first, so that tables built on first use are not counted. Then it runs with the collector
// off, after two collections, so that no freed buffer or pooled value is allocated again: every
// allocation of the run stays counted, and the run frees nothing that a later allocation reuses.
//
// A collection only queues finalizers; the goroutine that runs them is scheduled later, and under
// load it can run in the middle of the measured work and allocate there. So Allocs waits until the
// finalizers that are queued have run before it measures (drainFinalizers). It does not wait for the
// cleanups of runtime.AddCleanup, which the measured code must not use.
//
// The counts are exact, the same on every run whatever the load, only for work that allocates
// nothing that depends on the runtime's random choices or its other goroutines. The runtime breaks
// that in these ways, each of which only adds allocations to a run, and the variation they cause is
// a few objects, too small to change a verdict of Linear:
//
//   - A type assertion or a type switch to an interface type that misses the cache of its call site
//     rebuilds the cache in about one of 1024 misses, chosen at random (typeAssert and
//     interfaceSwitch in runtime/iface.go), and the rebuild allocates. One warm run does not fill
//     the caches: in a parse into typed values the first ten runs rebuilt one in about half the
//     runs, and later runs still in about one in ten.
//   - A map that grows past one table of 1024 slots splits its tables at points that depend on the
//     map's random hash seed, so a run allocates a few more or fewer tables, and their bytes.
//   - The runtime's background goroutines allocate now and then, such as a timer of the scavenger,
//     and the scheduler allocates when it starts a thread (newm), which work on more than one
//     processor can make it do.
//
// The collections before the run also empty every sync.Pool, so the first Get of each pool in the
// run allocates the pool's per-processor array again: one object, with bytes in proportion to
// GOMAXPROCS, so the bytes differ between machines but not between runs on one.
//
// So a measurement that must be exact, such as a metric compared across commits, takes the least of
// several calls after warm runs of the work, with GOMAXPROCS set to 1: the cache rebuilds and the
// background allocations only add to a count, so the least count is that of the work alone. Work
// whose maps split varies in both directions, and no number of calls makes it exact; its comparison
// needs a tolerance.
//
// The counters are those of the process, so the caller must not run in parallel with other tests,
// and the work must not leave goroutines that allocate after it returns. Allocs does not see work
// that allocates nothing, such as a loop that rescans its input; a counter of that work must cover
// it.
func Allocs(work func()) (objects, bytes uint64) {
	work()
	runtime.GC()
	drainFinalizers()
	runtime.GC()
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	work()
	runtime.ReadMemStats(&after)
	return after.Mallocs - before.Mallocs, after.TotalAlloc - before.TotalAlloc
}

// drainFinalizers returns when every finalizer queued by a collection before the call has run.
//
// The runtime guarantees little about the order: finalizers run on one goroutine, which takes the
// whole list of the queued ones at once and runs it, and a list that is queued while it runs is taken
// after it. Within a list the order is not the order of queueing (newer blocks run first), so a
// marker that is queued while the garbage's finalizers are still in the list can run before them.
// So the wait is for two markers in sequence. When the first has run, the goroutine has taken the
// list that held it, and the second is queued after that, so it is in a later list and runs only
// after the goroutine has finished the first one, with every finalizer that the first collection
// queued.
//
// Cleanups of runtime.AddCleanup are not drained: they run on goroutines of their own, concurrently
// and in no order, so no marker can tell that the others are done. The measured code must not use
// them.
func drainFinalizers() {
	runtime.GC() // queues the finalizers of everything that is garbage now
	for range 2 {
		done := make(chan struct{})
		marker := new([16]byte)
		runtime.SetFinalizer(marker, func(*[16]byte) { close(done) })
		marker = nil
		runtime.GC() // queues the marker
		<-done
	}
}
