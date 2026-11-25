package pipe

import (
	"io"
	"slices"
)

// Stats is an observer that sums the statistics of each pass instance over the units it ran on: the
// counters that the manager keeps for every pass (CounterRuns, CounterChanged and the decisions of
// its patterns) and the counters that the pass declares. It needs nothing of the manager beyond the
// events, so watching a run with it changes neither the run nor its outputs. The replay of a Shared
// result counts as the run it replays, with the counters recorded by the first run.
//
// The statistics depend only on the input, the settings and the pipeline, so they are the same in
// every run of the same compilation: Instances lists them in a fixed order, and no map order shows.
type Stats struct {
	byKey map[statsKey]*InstanceStats
	order []*InstanceStats
}

type statsKey struct{ backend, instance string }

// InstanceStats are the statistics of one pass instance in one backend.
type InstanceStats struct {
	Backend  string // the backend of the label (Label.Backend); empty outside a Fork
	Instance string // the instance of the pass (StepInfo.Instance)
	Runs     int    // the units the pass ran on (CounterRuns)
	Changed  int    // the runs whose pass reported a change (CounterChanged)
	Steps    uint64 // the work of the runs (Result.Steps)
	// Counters holds the counters that the pass declares, in the order of its declaration, each
	// summed over its runs; a counter that no run counted is 0.
	Counters []Counter
	// Patterns holds the decisions of the patterns that the pass declares, in the order of its
	// declaration.
	Patterns []PatternDecisions
}

// Counter is the value of one declared counter.
type Counter struct {
	Name  string
	Value int
}

// PatternDecisions are the decisions of one pattern.
type PatternDecisions struct {
	Pattern string
	Decisions
}

// NewStats returns an observer with no statistics yet.
func NewStats() *Stats { return &Stats{byKey: map[statsKey]*InstanceStats{}} }

// Needs returns what Stats reads: the events and results only.
func (*Stats) Needs() Needs { return Needs{} }

// Before lets every step run.
func (*Stats) Before(Event, func(io.Writer)) bool { return true }

// After adds the result of a step that ran to the statistics of its instance. The events of the
// decisions of patterns are counted in the result of the pass that asked (Result.Patterns).
func (s *Stats) After(e Event, r *Result) {
	if e.Step.Pattern || !r.Ran {
		return
	}
	d := passes[e.Step.Name]
	if d == nil {
		return
	}
	k := statsKey{e.Label.Backend, e.Step.Instance}
	st := s.byKey[k]
	if st == nil {
		st = &InstanceStats{Backend: k.backend, Instance: k.instance}
		for _, c := range d.Counters {
			st.Counters = append(st.Counters, Counter{Name: c})
		}
		for _, p := range d.Patterns {
			st.Patterns = append(st.Patterns, PatternDecisions{Pattern: p})
		}
		s.byKey[k] = st
		s.order = append(s.order, st)
	}
	st.Runs++
	st.Steps += r.Steps
	if r.Changed {
		st.Changed++
	}
	for i := range st.Counters {
		st.Counters[i].Value += r.Counters[st.Counters[i].Name]
	}
	for i := range st.Patterns {
		p := r.Patterns[st.Patterns[i].Pattern]
		st.Patterns[i].Applied += p.Applied
		st.Patterns[i].Skipped += p.Skipped
	}
}

// Instances returns the statistics of the instances that ran, in the order in which each first ran,
// which is the order of the pipeline. Each element is a copy.
func (s *Stats) Instances() []InstanceStats {
	out := make([]InstanceStats, len(s.order))
	for i, st := range s.order {
		out[i] = st.clone()
	}
	return out
}

func (st *InstanceStats) clone() InstanceStats {
	out := *st
	out.Counters = slices.Clone(st.Counters)
	out.Patterns = slices.Clone(st.Patterns)
	return out
}

// Instance returns the statistics of the instance in the backend, and whether it ran.
func (s *Stats) Instance(backend, instance string) (InstanceStats, bool) {
	st := s.byKey[statsKey{backend, instance}]
	if st == nil {
		return InstanceStats{}, false
	}
	return st.clone(), true
}
