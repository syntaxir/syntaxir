//go:build sxir_long

package examinedref_test

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/examinedref"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// perturbProgram is a compiled case of the engine tests.
type perturbProgram struct {
	name, alpha  string
	vars, stream bool
	prog         *engine.Program
	inputs       []string
}

// perturbPrograms compiles the cases of the perturbation and high-water tests.
func perturbPrograms(t *testing.T) []perturbProgram {
	t.Helper()
	var out []perturbProgram
	for _, c := range perturbCases(t) {
		g, err := enginetest.ParseGrammar(c.Src)
		if err != nil {
			t.Fatalf("%s: parse: %v", c.Name, err)
		}
		prog := enginetest.CompileBare(t, c.Src)
		out = append(out, perturbProgram{name: c.Name, alpha: examinedref.GrammarAlphabet(g), vars: examinedref.ReadsVariables(g),
			stream: hasStreamStart(prog), prog: prog, inputs: c.Inputs})
	}
	return out
}

// engineRun returns the function that parses an input on a route of the engine and returns the trace.
func engineRun(prog *engine.Program, b engine.Backend, unit input.PositionUnit, entry string) func(string) []examinedref.Event {
	return func(in string) []examinedref.Event {
		var tr []examinedref.Event
		o := engine.ParseOptions{Expectations: true, Backend: b, Unit: unit, Recognize: entry == "recognize"}
		o.Trace = func(e engine.TraceEvent) { tr = append(tr, traceEventOf(e)) }
		// The result and the error are not compared: the trace has every call that the result depends on.
		if entry == "stream" {
			_ = prog.ParseStreamWith("main", strings.NewReader(in), func(*value.Node) error { return nil }, o)
		} else {
			_, _ = prog.ParseWith("main", in, o)
		}
		return tr
	}
}

// TestEnginePerturbation checks the examined ends of the engine by perturbation (examinedref.Perturb) on
// every backend, in both position units, on the parse, recognize and stream routes: for each examined end of
// an observed parse, the parses of the inputs changed from it on must keep every call that does not examine
// beyond it, as the lockstep comparison of their traces shows. Its messages start with the backend, the unit
// and the route, which the engine's mutants (TestEngineExaminedMutants) look for.
func TestEnginePerturbation(t *testing.T) {
	t.Parallel()
	progs := perturbPrograms(t)
	for _, b := range perturbBackends {
		t.Run(b.String(), func(t *testing.T) {
			t.Parallel()
			for _, u := range perturbUnits {
				bytes := u.unit == input.Bytes
				totals := map[string]*examinedref.Report{}
				for _, entry := range []string{"parse", "recognize", "stream"} {
					totals[entry] = &examinedref.Report{}
				}
				skipped := map[string]int{}
				for _, p := range progs {
					for _, entry := range []string{"parse", "recognize", "stream"} {
						if entry == "stream" && !p.stream {
							continue
						}
						run := engineRun(p.prog, b, u.unit, entry)
						for _, in := range p.inputs {
							if len(examinedref.Units(in, bytes)) > perturbMaxUnits {
								skipped[entry]++
								continue
							}
							var invalid []string
							r := examinedref.Perturb(run, in, bytes, p.vars, p.alpha, func(q examinedref.Perturbation) {
								if err := validPerturbation(in, bytes, q); err != nil {
									invalid = append(invalid, err.Error())
								}
							})
							prefix := fmt.Sprintf("%s %s %s: %s", b, u.name, entry, p.name)
							reportPerturb(t, prefix+" (perturbation)", invalid, 3)
							reportPerturb(t, prefix, r.Violations, 5)
							totals[entry].Add(r)
						}
					}
				}
				for _, entry := range []string{"parse", "recognize", "stream"} {
					r := totals[entry]
					t.Logf("%s %s: %d inputs (%d too long), %d perturbations (%d dropped): %d identical, %d diverged in a call (%d exits checked, %d exempt), %d diverged outside calls",
						u.name, entry, r.Parses, skipped[entry], r.Perturbed, r.Dropped, r.Identical, r.Diverged, r.Checked, r.Exempt, r.Outside)
					// A check that never sees a difference, or never sees none, cannot tell an exact examined end
					// from one that is too small.
					if r.Diverged == 0 || r.Identical == 0 || r.Checked == 0 {
						t.Errorf("%s %s %s: the perturbations never diverged or never kept the trace", b, u.name, entry)
					}
				}
			}
		})
	}
}
