//go:build !sxir_budget_controls_reference

package state

import "testing"

// resyncPolls returns the steps (budget.poll) of each call of RunResync on r, which a parse makes
// before each element that it evaluates after the edit, at the positions at: each step is an old
// element that the search or the scan of the call passes.
func resyncPolls(t *testing.T, r *RunState, at []int) (polls []int, resynced bool) {
	t.Helper()
	p := docParser()
	for _, pos := range at {
		p.Pos = pos
		p.Control.SinceCheck = 0
		ok := p.RunResync(r)
		polls = append(polls, int(p.Control.SinceCheck))
		if ok {
			return polls, true
		}
	}
	return polls, false
}

// Looking for the old element after the edit to resync at charges nothing, so its work must be bounded
// by the old run (spec/runtime/documents.md, "Work of a Document Parse"): over a parse, the search and
// the scan each pass an old element at most once, however many evaluated elements look again. Here
// every old element after the edit starts where an evaluated element would, and only the last examined
// input before the end of the edit, so each look would scan the rest of the old run again.
func TestRunResyncScansEachOldElementOnce(t *testing.T) {
	t.Parallel()
	const n = 100
	run := func() *RunState {
		r := &RunState{Old: oldRun(n, nil), J: -1, Max: -1, Shiftable: true, EditStart: 0, EditEnd: 1}
		for i := range r.Old {
			r.Old[i].Start += 10
			r.Old[i].From += 10
		}
		return r
	}
	at := make([]int, n)
	for i := range at {
		at[i] = 10 + 2*i
	}

	// Barred by the last old element: no look resyncs, and the steps of all of them are at most two
	// for each old element (one search and one scan), not one scan of the rest for each look.
	r := run()
	r.Old[n-1].From = 0
	polls, resynced := resyncPolls(t, r, at)
	total := 0
	for _, k := range polls {
		total += k
	}
	if resynced || total > 2*n {
		t.Fatalf("resynced %t, %d steps over %d looks of an old run of %d elements: %v", resynced, total, len(polls), n, polls)
	}
	if polls[0] != n {
		t.Fatalf("the first look scanned %d old elements, want all %d: %v", polls[0], n, polls)
	}

	// Not barred: the first look resyncs at the first old element, after scanning the old run once.
	r = run()
	polls, resynced = resyncPolls(t, r, at)
	if !resynced || len(polls) != 1 || polls[0] != n || r.J != 0 {
		t.Fatalf("resynced %t at %d after the steps %v", resynced, r.J, polls)
	}

	// Barred in the middle: the looks before the bar fail without a second scan; the first look past
	// it scans the rest once and resyncs there.
	r = run()
	r.Old[n/2].From = 0
	polls, resynced = resyncPolls(t, r, at)
	total = 0
	for _, k := range polls {
		total += k
	}
	if !resynced || r.J != n/2+1 || total > 2*n {
		t.Fatalf("resynced %t at %d, %d steps: %v", resynced, r.J, total, polls)
	}
}
