// Package pipetest holds test helpers of the pass manager (internal/pass/pipe).
package pipetest

import (
	"io"

	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Watch is an observer of the steps of a run for tests (driver.Options.Observers): Before calls Start,
// if set, with the event of each step before it runs, and After calls Fn, if set, with the event and
// the result of each step that ran. With Units the result holds the unit after the step, which turns
// off the sharing of results (pipe.Needs.Units); with Print it holds the dump of the unit, valid
// during the call only.
type Watch struct {
	Units, Print bool
	Start        func(e pipe.Event)
	Fn           func(e pipe.Event, r *pipe.Result)
}

func (w *Watch) Needs() pipe.Needs { return pipe.Needs{Units: w.Units, Print: w.Print} }
func (w *Watch) Before(e pipe.Event, _ func(io.Writer)) bool {
	if w.Start != nil {
		w.Start(e)
	}
	return true
}

func (w *Watch) After(e pipe.Event, r *pipe.Result) {
	if r.Ran && w.Fn != nil {
		w.Fn(e, r)
	}
}
