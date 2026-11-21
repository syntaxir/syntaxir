package tsconformtest

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
)

// Family binds a family of operations of the suite to the library: the Go side that records and
// replays calls, and the name of the bindings in harness.ts.
type Family struct {
	// Name is the family of the bindings in harness.ts.
	Name string
	// Controlled makes the library's parser a parse with budget controls, so that the harness checks the
	// cost bound of every call.
	Controlled bool
	// Options are the options of the family's parser that the bindings read.
	Options map[string]any
	// Record returns the implementation that records the calls of a case, a second copy of the model
	// with the optional interfaces that the family's checks use.
	Record func(*Recorder) conform.Impl
	// Pending are operations whose cases the runner does not run on the library yet: it records the
	// case and reports it as Pending, so that they are counted and not silently dropped.
	Pending []string
	// Unsupported reports a configuration that the library's parser does not run: the case is skipped.
	Unsupported func(ref.Config) bool
	// Replay returns the implementation that answers the harness with what the library did, with the
	// same interfaces as Record and, for a controlled family, conform.Metered.
	Replay func(*Replay) conform.Impl
}

// Runner runs cases on the library of a session.
type Runner struct {
	Session *Session
}

// Status is what became of a case.
type Status int

const (
	// Ran is a case that ran on the library.
	Ran Status = iota
	// Skipped is a case that calls an operation the suite does not provide.
	Skipped
	// Pending is a case that calls an operation of Family.Pending: it did not run on the library.
	Pending
)

// Run runs the case c on the library as the suite s checks it (s.New is not used) and returns a
// *conform.Failure for a difference between the model and the library.
func (r Runner) Run(f Family, s conform.Suite, c conform.Case) (Status, error) {
	rec := recorderFor(s, f)
	skipped, err := conform.Run(rec.suite, c)
	if skipped || err != nil {
		if err != nil {
			err = fmt.Errorf("the case %s fails against its own model, which is a fault of the harness: %w", c.Func.Name, err)
			return Ran, err
		}
		return Skipped, nil
	}
	if f.Unsupported != nil && f.Unsupported(rec.cfg) {
		return Skipped, nil
	}
	trace := rec.r.Trace()
	for _, e := range trace {
		if slices.Contains(f.Pending, e.Op) {
			return Pending, nil
		}
	}
	resp, err := r.Session.Run(Request{Family: f.Name, Config: ConfigOf(rec.cfg), Controlled: f.Controlled, Options: f.Options, Entries: trace})
	if err != nil {
		return Ran, fmt.Errorf("case %s: %w", c.Func.Name, err)
	}
	replay := s
	replay.New = func(ref.Config) conform.Impl { return f.Replay(NewReplay(trace, resp.Answers)) }
	_, err = conform.Run(replay, c)
	return Ran, err
}

// recording is the first pass of a case.
type recording struct {
	suite conform.Suite
	r     *Recorder
	cfg   ref.Config
}

func recorderFor(s conform.Suite, f Family) *recording {
	rec := &recording{}
	rec.suite = s
	rec.suite.Exact = nil
	rec.suite.New = func(cfg ref.Config) conform.Impl {
		rec.cfg = cfg
		rec.r = NewRecorder(cfg)
		return f.Record(rec.r)
	}
	return rec
}
