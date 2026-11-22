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
	name, alpha           string
	vars, stream, missing bool
	parser                *engine.Parser
	inputs                []string
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
		started, err := enginetest.Parse(c.Src)
		if err != nil {
			t.Fatalf("%s: parse: %v", c.Name, err)
		}
		p, err := enginetest.Compile(started, "main")
		if err != nil {
			t.Fatalf("%s: compile: %v", c.Name, err)
		}
		uses := usesOf(g)
		out = append(out, perturbProgram{name: c.Name, alpha: examinedref.GrammarAlphabet(g), vars: examinedref.ReadsVariables(g),
			stream: uses.stream, missing: uses.missing, parser: p, inputs: c.Inputs})
	}
	return out
}

// perturbRoute is a route of an observed engine parse. run parses the text s on backend b in unit u for
// the change point change and returns its trace, or false when the route cannot run that parse (an edit
// that the Document refuses). Every route but edit parses s alone and ignores change. runs reports
// whether the route runs a grammar on the backend: the routes refuse grammars with #missing outside the
// closure backend's whole-input parses, and a stream parse needs #stream in the start rule
// (TestExaminedOracle checks those refusals).
type perturbRoute struct {
	name string
	run  func(p *engine.Parser, b engine.Backend, u input.PositionUnit, base string, change int, s string) ([]examinedref.Event, bool)
	runs func(p perturbProgram, b engine.Backend) bool
}

// tracing returns the options of a traced parse in unit u, which append the events to tr.
func tracing(tr *[]examinedref.Event, u input.PositionUnit) []engine.ParseOption {
	return []engine.ParseOption{engine.WithUnit(u), engine.WithTrace(func(e engine.TraceEvent) { *tr = append(*tr, traceEventOf(e)) })}
}

// wholeInputRuns is the refusal of the whole-input parses: only the closure backend runs #missing.
func wholeInputRuns(p perturbProgram, b engine.Backend) bool {
	return !p.missing || b == engine.Closure
}

// perturbRoutes are the routes of TestExaminedOracle: Parse with expectations, Recognize, ParseStream, the
// first parse of a Document, the parse of a Document after an edit, and Parse with token insertion,
// which only the closure backend supports. The result and the error of a parse are not compared: the
// trace has every call that the result depends on.
var perturbRoutes = []perturbRoute{
	{"parse", func(p *engine.Parser, b engine.Backend, u input.PositionUnit, _ string, _ int, s string) ([]examinedref.Event, bool) {
		var tr []examinedref.Event
		_, _ = p.OnBackend(b).Parse(s, append(tracing(&tr, u), engine.WithExpectations())...)
		return tr, true
	}, wholeInputRuns},
	{"recognize", func(p *engine.Parser, b engine.Backend, u input.PositionUnit, _ string, _ int, s string) ([]examinedref.Event, bool) {
		var tr []examinedref.Event
		_ = p.OnBackend(b).Recognize(s, tracing(&tr, u)...)
		return tr, true
	}, wholeInputRuns},
	{"stream", func(p *engine.Parser, b engine.Backend, u input.PositionUnit, _ string, _ int, s string) ([]examinedref.Event, bool) {
		var tr []examinedref.Event
		_ = p.OnBackend(b).ParseStream(strings.NewReader(s), func(*value.Node) error { return nil }, append(tracing(&tr, u), engine.WithExpectations())...)
		return tr, true
	}, func(p perturbProgram, _ engine.Backend) bool { return p.stream && !p.missing }},
	{"document", func(p *engine.Parser, b engine.Backend, u input.PositionUnit, _ string, _ int, s string) ([]examinedref.Event, bool) {
		var tr []examinedref.Event
		d, err := p.OnBackend(b).NewDocument(s, engine.WithUnit(u), engine.WithExpectations())
		if err == nil {
			_, _ = d.Parse(tracing(&tr, u)...)
		}
		return tr, true
	}, func(p perturbProgram, _ engine.Backend) bool { return !p.missing }},
	// The reparse of a Document of base after the edit that replaces base from the change point on with
	// the rest of s, which Perturbations keeps equal to base before the change point: the original's
	// own text and each perturbation's are put there into a Document of the original, so that the edits
	// differ in their text alone and keep the same results of the first parse.
	{"edit", func(p *engine.Parser, b engine.Backend, u input.PositionUnit, base string, change int, s string) ([]examinedref.Event, bool) {
		bytes := u == input.Bytes
		d, err := p.OnBackend(b).NewDocument(base, engine.WithUnit(u), engine.WithExpectations())
		if err != nil {
			return nil, false
		}
		_, _ = d.Parse()
		rest := strings.Join(examinedref.Units(s, bytes)[change:], "")
		if d.Edit(change, len(examinedref.Units(base, bytes)), rest) != nil {
			return nil, false
		}
		var tr []examinedref.Event
		_, _ = d.Parse(tracing(&tr, u)...)
		return tr, true
	}, func(p perturbProgram, _ engine.Backend) bool { return !p.missing }},
	{"insertion", func(p *engine.Parser, b engine.Backend, u input.PositionUnit, _ string, _ int, s string) ([]examinedref.Event, bool) {
		var tr []examinedref.Event
		_, _ = p.OnBackend(b).Parse(s, append(tracing(&tr, u), engine.WithMissingTokens(missingLimit))...)
		return tr, true
	}, func(_ perturbProgram, b engine.Backend) bool { return b == engine.Closure }},
}

// TestEnginePerturbation checks the examined ends of the engine by perturbation (examinedref.Perturb and,
// for the reparse after an edit, examinedref.PerturbAt) on every backend, in both position units, on
// every route (perturbRoutes): for each examined end of an observed parse, the parses of the inputs
// changed from it on must keep every call that does not examine beyond it, as the lockstep comparison of
// their traces shows. Its messages start with the backend, the unit and the route, which the engine's
// mutants (TestEngineExaminedMutants) look for.
func TestEnginePerturbation(t *testing.T) {
	t.Parallel()
	progs := perturbPrograms(t)
	for _, b := range perturbBackends {
		t.Run(b.String(), func(t *testing.T) {
			t.Parallel()
			for _, u := range perturbUnits {
				bytes := u.unit == input.Bytes
				totals := map[string]*examinedref.Report{}
				skipped := map[string]int{}
				for _, p := range progs {
					for _, route := range perturbRoutes {
						if !route.runs(p, b) {
							continue
						}
						if totals[route.name] == nil {
							totals[route.name] = &examinedref.Report{}
						}
						for _, in := range p.inputs {
							if len(examinedref.Units(in, bytes)) > perturbMaxUnits {
								skipped[route.name]++
								continue
							}
							var invalid []string
							check := func(q examinedref.Perturbation) {
								if err := validPerturbation(in, bytes, q); err != nil {
									invalid = append(invalid, err.Error())
								}
							}
							run := func(change int, s string) ([]examinedref.Event, bool) {
								return route.run(p.parser, b, u.unit, in, change, s)
							}
							var r examinedref.Report
							if route.name == "edit" {
								r = examinedref.PerturbAt(run, in, bytes, p.vars, p.alpha, check)
							} else {
								r = examinedref.Perturb(func(s string) []examinedref.Event { tr, _ := run(0, s); return tr }, in, bytes, p.vars, p.alpha, check)
							}
							prefix := fmt.Sprintf("%s %s %s: %s", b, u.name, route.name, p.name)
							reportPerturb(t, prefix+" (perturbation)", invalid, 3)
							reportPerturb(t, prefix, r.Violations, 5)
							totals[route.name].Add(r)
						}
					}
				}
				for _, route := range perturbRoutes {
					r := totals[route.name]
					if r == nil {
						if route.name != "insertion" || b == engine.Closure {
							t.Errorf("%s %s %s: no case runs the route", b, u.name, route.name)
						}
						continue
					}
					t.Logf("%s %s: %d inputs (%d too long), %d perturbations (%d dropped, %d refused): %d identical, %d diverged in a call (%d exits checked, %d exempt), %d diverged outside calls",
						u.name, route.name, r.Parses, skipped[route.name], r.Perturbed, r.Dropped, r.Refused, r.Identical, r.Diverged, r.Checked, r.Exempt, r.Outside)
					// A check that never sees a difference, or never sees none, cannot tell an exact examined end
					// from one that is too small.
					if r.Diverged == 0 || r.Identical == 0 || r.Checked == 0 {
						t.Errorf("%s %s %s: the perturbations never diverged or never kept the trace", b, u.name, route.name)
					}
				}
			}
		})
	}
}
