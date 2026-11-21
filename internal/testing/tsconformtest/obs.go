package tsconformtest

import (
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
)

// Obs is the family of the observation of a parse: the library's Observation, whose events the trace
// function lists as the model lists its own.
func Obs() Family {
	return Family{
		Name:   "obs",
		Record: func(r *Recorder) conform.Impl { return obsRecorder{execRecorder{r}} },
		Replay: func(p *Replay) conform.Impl { return obsReplay{execReplay{p}} },
	}
}

type obsRecorder struct{ execRecorder }

func (r obsRecorder) Observe(component string) []string { return r.Model.Observe(component) }

type obsReplay struct{ execReplay }

func (r obsReplay) Observe(component string) []string { return r.Observation(component) }
