// Package allocs counts the objects and bytes that some work allocates, exactly under the conditions
// that Count states: the measure of allocation that the tests and the deterministic metrics share, and
// that the measuring programs of bench/metrics link without the testing packages.
package allocs

import (
	"math"
	"runtime"
	"runtime/debug"
)

// Count returns the number of objects and of bytes that a run of work allocates. The work runs
// once first, so that tables built on first use are not counted. Then it runs with the collector
// off, after two collections, so that no freed buffer or pooled value is allocated again: every
// allocation of the run stays counted, and the run frees nothing that a later allocation reuses.
//
// A collection only queues finalizers; the goroutine that runs them is scheduled later, and under
// load it can run in the middle of the measured work and allocate there. So Count waits until the
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
// and the work must not leave goroutines that allocate after it returns. Count does not see work
// that allocates nothing, such as a loop that rescans its input; a counter of that work must cover
// it.
func Count(work func()) (objects, bytes uint64) {
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

// Least returns the objects and bytes that a run of work allocates (Count): the least count
// of rounds measurements, with the bytes of that measurement, after warm runs of work.
//
// The runtime adds allocations of its own to a run now and then, at random (Count lists them). Each
// only adds to a count, so with GOMAXPROCS at 1 the least count of several measurements after the warm
// runs is the count of the work alone. The caches fill slowly: in 30 parses of the Minilang workload
// into typed values, the first ten rebuilt a cache in about half the runs and the last ten in about
// one in ten, hence the warm runs.
func Least(warm, rounds int, work func()) (objects, bytes uint64) {
	for range warm {
		work()
	}
	objects = math.MaxUint64
	for range rounds {
		if o, b := Count(work); o < objects {
			objects, bytes = o, b
		}
	}
	return objects, bytes
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
