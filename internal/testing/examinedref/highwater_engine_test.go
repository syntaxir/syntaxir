package examinedref_test

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/examinedref"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// TestEngineStreamHighWater checks the examined ends of the engine's stream parses against what the parse
// read (examinedref.CheckHighWater) on every backend, in both position units, with a reader that delivers
// one byte per read: no exit examines input that the reader has not delivered by its event, and the start
// rule, with the whole-input check, examined exactly the input that the parse read. Its messages start with
// the backend and the unit, which the engine's mutants (TestEngineExaminedMutants) look for.
func TestEngineStreamHighWater(t *testing.T) {
	t.Parallel()
	progs := perturbPrograms(t)
	for _, b := range perturbBackends {
		t.Run(b.String(), func(t *testing.T) {
			t.Parallel()
			for _, u := range perturbUnits {
				parses, ended := 0, 0
				for _, p := range progs {
					if !p.stream {
						continue
					}
					for _, in := range p.inputs {
						r := examinedref.NewCountingReader(in)
						var tr []examinedref.Event
						var reads []examinedref.Read
						trace := engine.WithTrace(func(e engine.TraceEvent) {
							tr = append(tr, traceEventOf(e))
							reads = append(reads, examinedref.Read{Delivered: r.Delivered, EOF: r.EOF})
						})
						_ = p.parser.OnBackend(b).ParseStream(r, func(*value.Node) error { return nil }, engine.WithExpectations(), engine.WithUnit(u.unit), trace)
						parses++
						if r.EOF {
							ended++
						}
						diffs := examinedref.CheckHighWater(in, u.unit == input.Bytes, tr, reads, examinedref.Read{Delivered: r.Delivered, EOF: r.EOF})
						reportPerturb(t, fmt.Sprintf("%s %s: %s %q", b, u.name, p.name, in), diffs, 5)
					}
				}
				t.Logf("%s: %d stream parses, %d read to the end of the input", u.name, parses, ended)
				// The shapes stop reading before the end of some inputs and read to it on others.
				if parses == 0 || ended == 0 || ended == parses {
					t.Errorf("%s %s: %d stream parses, %d of which read to the end", b, u.name, parses, ended)
				}
			}
		})
	}
}
