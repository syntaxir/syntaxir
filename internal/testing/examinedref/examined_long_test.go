//go:build sxir_long

package examinedref_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/examinedref"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// oracleCase is a grammar with inputs, from the generated corpus or codegentest.ExaminedShapes.
type oracleCase struct {
	name, src string
	inputs    []string
}

// oneByte returns one byte per Read, so that a stream parse loads its input as late as it can.
type oneByte struct{ s string }

func (r *oneByte) Read(b []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	b[0], r.s = r.s[0], r.s[1:]
	return 1, nil
}

// observedParse is one parse of a route: the text parsed, the calls reported, the error, whether
// tokens were inserted and, for a prefix parse, the cursor it reported.
type observedParse struct {
	text     string
	calls    []examinedref.Observed
	err      error
	missing  bool
	prefix   bool
	consumed int
}

// route is a way to run an observed parse. refusal is the start of the error with which the route
// refuses a grammar, given what the grammar uses, before any call, or "" when it runs the grammar;
// actions tells which Value Actions it runs, and stream that it is a stream parse, which builds no
// value of the start rule at its top call.
type route struct {
	name    string
	run     func(p *engine.Parser, in string, u input.PositionUnit) []observedParse
	refusal func(uses grammarUses) string
	actions examinedref.Actions
	stream  bool
}

func traced(rec *examinedref.Recorder, u input.PositionUnit) []engine.ParseOption {
	return []engine.ParseOption{engine.WithUnit(u), engine.WithTrace(rec.Trace)}
}

func once(text string, rec *examinedref.Recorder, err error, missing bool) []observedParse {
	return []observedParse{{text: text, calls: append([]examinedref.Observed(nil), rec.Calls...), err: err, missing: missing}}
}

// routes are the routes of every backend: Parse with expectations, Recognize, ParseStream, a
// Document parsed before and after edits and ParsePrefix, and each of Parse, ParseStream, the
// Document and ParsePrefix with token insertion.
func routes() []route {
	var out []route
	every := func(grammarUses) string { return "" }
	streams := func(uses grammarUses) string {
		if !uses.stream {
			return "rule main has no #stream repetition"
		}
		return ""
	}
	insertion := []engine.ParseOption{engine.WithMissingTokens(missingLimit)}
	for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		name := b.String()
		out = append(out,
			route{name + "/parse", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
				var rec examinedref.Recorder
				_, err := p.OnBackend(b).Parse(in, append(traced(&rec, u), engine.WithExpectations())...)
				return once(in, &rec, err, false)
			}, every, examinedref.EveryAction, false},
			route{name + "/recognize", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
				var rec examinedref.Recorder
				err := p.OnBackend(b).Recognize(in, traced(&rec, u)...)
				return once(in, &rec, err, false)
			}, every, examinedref.ReadActions, false},
			route{name + "/stream", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
				var rec examinedref.Recorder
				err := p.OnBackend(b).ParseStream(&oneByte{in}, func(*value.Node) error { return nil }, traced(&rec, u)...)
				return once(in, &rec, err, false)
			}, streams, examinedref.EveryAction, true},
			route{name + "/document", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
				return documentParses(p.OnBackend(b), in, u, false)
			}, every, examinedref.EveryAction, false},
			route{name + "/insertion", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
				var rec examinedref.Recorder
				_, err := p.OnBackend(b).Parse(in, append(traced(&rec, u), insertion...)...)
				return once(in, &rec, err, true)
			}, every, examinedref.EveryAction, false},
			route{name + "/stream-insertion", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
				var rec examinedref.Recorder
				err := p.OnBackend(b).ParseStream(&oneByte{in}, func(*value.Node) error { return nil }, append(traced(&rec, u), insertion...)...)
				return once(in, &rec, err, true)
			}, streams, examinedref.EveryAction, true},
			route{name + "/document-insertion", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
				return documentParses(p.OnBackend(b), in, u, true)
			}, every, examinedref.EveryAction, false},
			route{name + "/prefix", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
				return prefixParse(p.OnBackend(b), in, u, false)
			}, every, examinedref.EveryAction, false},
			route{name + "/prefix-insertion", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
				return prefixParse(p.OnBackend(b), in, u, true)
			}, every, examinedref.EveryAction, false},
		)
	}
	return out
}

// prefixParse parses a prefix of in with main, with expectations, or with token insertion when
// missing, which the reference evaluates without expectations.
func prefixParse(p *engine.Parser, in string, u input.PositionUnit, missing bool) []observedParse {
	var rec examinedref.Recorder
	opts := traced(&rec, u)
	if missing {
		opts = append(opts, engine.WithMissingTokens(missingLimit))
	} else {
		opts = append(opts, engine.WithExpectations())
	}
	r, err := p.ParsePrefix("main", in, opts...)
	op := once(in, &rec, err, missing)
	op[0].prefix, op[0].consumed = true, r.Consumed
	return op
}

// documentParses parses a Document of in with its middle character removed, inserts it, inserts a
// character at the start and removes it again, parsing after each edit, so that the parses after the
// edits reuse the results of the ones before. With missing, the Document inserts missing tokens and
// records no expectations; without, it records them.
func documentParses(p *engine.Parser, in string, u input.PositionUnit, missing bool) []observedParse {
	chars := []string{}
	if u == input.Bytes {
		for i := 0; i < len(in); i++ {
			chars = append(chars, in[i:i+1])
		}
	} else {
		if !utf8.ValidString(in) {
			// A code-point Document stores the replacement characters, so the edits below would not
			// restore in.
			return nil
		}
		for _, r := range in {
			chars = append(chars, string(r))
		}
	}
	mid := len(chars) / 2
	text := strings.Join(chars[:mid], "")
	if mid < len(chars) {
		text += strings.Join(chars[mid+1:], "")
	}
	opts := []engine.ParseOption{engine.WithUnit(u)}
	if missing {
		// The reference fixes the message of an insertion as a parse without expectations writes it.
		opts = append(opts, engine.WithMissingTokens(missingLimit))
	} else {
		opts = append(opts, engine.WithExpectations())
	}
	d, err := p.NewDocument(text, opts...)
	if err != nil {
		// A refusal: a Document makes no calls before its first parse.
		return []observedParse{{text: text, err: err}}
	}
	var out []observedParse
	parse := func(text string) bool {
		var rec examinedref.Recorder
		_, err := d.Parse(engine.WithTrace(rec.Trace))
		out = append(out, once(text, &rec, err, missing)...)
		return !aborted(err)
	}
	if !parse(text) {
		return nil
	}
	if mid < len(chars) {
		if d.Edit(mid, mid, chars[mid]) != nil || !parse(in) {
			return out
		}
	}
	if d.Edit(0, 0, "x") != nil || !parse("x"+in) {
		return out
	}
	if d.Edit(0, 1, "") == nil {
		parse(in)
	}
	return out
}

// referenceRuns caches the reference's evaluations of one grammar.
type referenceRuns struct {
	g    *ast.Grammar
	mu   sync.Mutex
	runs map[examinedref.Options]map[string]referenceRun
}

type referenceRun struct {
	res   examinedref.Result
	exits []examinedref.Exit
	err   error
}

func (r *referenceRuns) get(text string, o examinedref.Options) referenceRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runs[o] == nil {
		r.runs[o] = map[string]referenceRun{}
	}
	run, ok := r.runs[o][text]
	if !ok {
		run.res, run.exits, run.err = examinedref.EvaluateWith(r.g, "main", text, o)
		r.runs[o][text] = run
	}
	return run
}

// aborted reports an error that ends a parse before the start call returns: one that is not a syntax
// error, which without limits is a runtime error of a Value Action.
func aborted(err error) bool {
	if err == nil {
		return false
	}
	var one *value.SyntaxError
	var errs value.SyntaxErrors
	var truncated *value.TruncatedSyntaxErrors
	return !errors.As(err, &one) && !errors.As(err, &errs) && !errors.As(err, &truncated)
}

// shapesOnly is the environment variable that limits TestExaminedOracle to codegentest.ExaminedShapes,
// as the mutants of the reference run it.
const shapesOnly = "SXIR_EXAMINED_SHAPES_ONLY"

// TestExaminedOracle checks the calls of the engine's observed parses against the reference on the
// generated corpus and the shapes, on every route, backend and position unit: every reported exit is
// an exit of the reference with the same match, end and examined end, every skipped call fails in the
// reference, and the start call returns exactly what the reference's does.
func TestExaminedOracle(t *testing.T) {
	t.Parallel()
	var cases []oracleCase
	if os.Getenv(shapesOnly) == "" {
		for _, c := range codegentest.Representative(codegentest.GenCorpus(t)) {
			cases = append(cases, oracleCase{name: c.Name, src: c.Src, inputs: c.Inputs})
		}
	}
	for _, c := range codegentest.ExaminedShapes() {
		cases = append(cases, oracleCase{name: c.Name, src: c.Src, inputs: c.Inputs})
	}

	var mu sync.Mutex
	compared := map[string]int{}
	t.Run("cases", func(t *testing.T) {
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				g, err := enginetest.ParseGrammar(c.src)
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				started, err := enginetest.Parse(c.src)
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				p, err := enginetest.Compile(started, "main")
				if err != nil {
					t.Fatalf("compile: %v", err)
				}
				refs := &referenceRuns{g: g, runs: map[examinedref.Options]map[string]referenceRun{}}
				uses := usesOf(g)
				for _, in := range c.inputs {
					for _, u := range []input.PositionUnit{input.CodePoints, input.Bytes} {
						for _, r := range routes() {
							refusal := r.refusal(uses)
							for _, op := range r.run(p, in, u) {
								if refusal != "" {
									// The route must refuse the grammar, before any call, and with its error.
									if len(op.calls) > 0 || op.err == nil || !strings.HasPrefix(op.err.Error(), refusal) {
										t.Errorf("%s %v %q: want a refusal %q, got %d calls and error %v", r.name, u, op.text, refusal, len(op.calls), op.err)
									}
									continue
								}
								ref := refs.get(op.text, examinedref.Options{Unit: u, MissingTokens: op.missing, NoStartValue: r.stream})
								if ref.err != nil {
									t.Fatalf("%q: reference: %v", op.text, ref.err)
								}
								diffs := examinedref.CompareParse(ref.res, ref.exits, "main", op.calls, aborted(op.err), r.actions)
								// A prefix parse reports the end of the start call, or nothing when it fails.
								if op.prefix && !aborted(op.err) {
									if want := ref.res.End; ref.res.Matched && op.consumed != want {
										diffs = append(diffs, fmt.Sprintf("consumed %d: reference end %d", op.consumed, want))
									} else if !ref.res.Matched && op.consumed != 0 {
										diffs = append(diffs, fmt.Sprintf("consumed %d: reference fails", op.consumed))
									}
								}
								if len(diffs) > 0 && aborted(op.err) {
									diffs = append(diffs, fmt.Sprintf("aborted by %v", op.err))
								}
								for i, d := range diffs {
									if i == 5 {
										t.Errorf("%s %v %q: %d more differences", r.name, u, op.text, len(diffs)-i)
										break
									}
									t.Errorf("%s %v %q: %s", r.name, u, op.text, d)
								}
								mu.Lock()
								compared[r.name]++
								mu.Unlock()
							}
						}
					}
				}
			})
		}
	})
	t.Logf("parses compared by route: %v", compared)
	for _, r := range routes() {
		if compared[r.name] == 0 {
			t.Errorf("no parse of %s compared", r.name)
		}
	}
}
