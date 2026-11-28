package allocs

import (
	"runtime"
	"sync/atomic"
	"testing"
)

var sink []*[64]byte

var finalizerSink atomic.Pointer[[64]byte]

// TestCountIsExact checks that the counts are those of the work, and the same on every run.
func TestCountIsExact(t *testing.T) {
	work := func() {
		sink = make([]*[64]byte, 0, 100)
		for range 100 {
			sink = append(sink, new([64]byte))
		}
	}
	objects, bytes := Count(work)
	if objects != 101 || bytes < 100*64 {
		t.Errorf("%d objects, %d bytes; want 101 objects and at least %d bytes", objects, bytes, 100*64)
	}
	for range 3 {
		if o, b := Count(work); o != objects || b != bytes {
			t.Errorf("a run counted %d objects and %d bytes, the first %d and %d", o, b, objects, bytes)
		}
	}
}

// TestCountIgnoresPendingFinalizers checks that finalizers queued before the measurement, which
// allocate when they run, do not run inside it. Without the wait for them, the finalizer goroutine
// runs them during the measured work and its allocations are counted as the work's.
func TestCountIgnoresPendingFinalizers(t *testing.T) {
	var ran atomic.Int64
	const garbage = 2000
	for range garbage {
		o := new([32]byte)
		runtime.SetFinalizer(o, func(*[32]byte) {
			finalizerSink.Store(new([64]byte)) // allocates, as a finalizer that closes a file does
			ran.Add(1)
		})
	}
	work := func() {
		sink = make([]*[64]byte, 0, 100)
		for range 100 {
			sink = append(sink, new([64]byte))
		}
	}
	if objects, _ := Count(work); objects != 101 {
		t.Errorf("%d objects with finalizers pending; want 101", objects)
	}
	if n := ran.Load(); n != garbage {
		t.Errorf("%d of %d finalizers had run when Count returned", n, garbage)
	}
}

// TestDrainFinalizersWaitsForEveryFinalizer checks that drainFinalizers returns only after all the
// finalizers of the garbage have run, however the finalizer goroutine is scheduled. The runtime does
// not run a list of finalizers in the order of queueing, so a single marker finalizer can return
// while the batch is still pending; many short trials make that visible.
func TestDrainFinalizersWaitsForEveryFinalizer(t *testing.T) {
	const garbage = 2000
	trials := 200
	if testing.Short() {
		trials = 20
	}
	for trial := range trials {
		var ran atomic.Int64
		for range garbage {
			o := new([32]byte)
			runtime.SetFinalizer(o, func(*[32]byte) {
				finalizerSink.Store(new([64]byte))
				ran.Add(1)
			})
		}
		drainFinalizers()
		if n := ran.Load(); n != garbage {
			t.Fatalf("trial %d: drainFinalizers returned with %d of %d finalizers run", trial, n, garbage)
		}
	}
}

var moreKeep *[64]byte

// TestCountSeesMoreWork checks that work which allocates one object more on every run adds to the counts:
// the counts are those of the work, so more work gives more counts.
func TestCountSeesMoreWork(t *testing.T) {
	base := func() {
		sink = make([]*[64]byte, 0, 100)
		for range 100 {
			sink = append(sink, new([64]byte))
		}
	}
	wantO, wantB := Count(base)
	o, b := Count(func() {
		base()
		moreKeep = new([64]byte)
	})
	if o <= wantO || b <= wantB {
		t.Errorf("work that allocates more: %d objects, %d bytes; want more than %d and %d", o, b, wantO, wantB)
	}
}

// Least takes the least count of its rounds after the warm runs: work that allocates an extra object
// in some rounds counts as the work without it, and the warm runs come before every round.
func TestLeast(t *testing.T) {
	runs := 0
	work := func() {
		runs++
		sink = make([]*[64]byte, 0, 10)
		for range 10 {
			sink = append(sink, new([64]byte))
		}
		if runs%2 == 0 {
			moreKeep = new([64]byte)
		}
	}
	objects, bytes := Least(3, 4, work)
	if objects != 11 || bytes < 10*64 {
		t.Errorf("%d objects, %d bytes; want the 11 objects of the rounds without the extra one", objects, bytes)
	}
	// 3 warm runs, then each round of Count runs the work twice.
	if runs != 3+4*2 {
		t.Errorf("the work ran %d times, want %d", runs, 3+4*2)
	}
}
