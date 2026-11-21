//go:build sxir_long

package examinedref_test

import (
	"errors"
	"io"
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

// oracleCase is a grammar with inputs, from the generated corpus or the shapes below.
type oracleCase struct {
	name, src string
	inputs    []string
}

// oracleShapes are shapes that the reference must agree with the engine on besides the generated
// corpus: growths of left recursion, Operators Rules, recovery, token insertion, predicates and
// variables, #ignorecase, inline actions and input that is not valid UTF-8.
func oracleShapes() []oracleCase {
	const ops = "rule e = #operators {\n skip \" \"*\n operand [0-9]+ #atomic\n operand \"(\" e \" \"* \")\"\n" +
		" level add { infix left \"+\" / \"-\" }\n level cmp { infix none \"<\" }\n level neg { prefix \"-\" }\n" +
		" level mul { infix left \"*\" }\n level pow { infix right \"**\" }\n level post { postfix \"!\" postfix \"[\" e \"]\" }\n}"
	return []oracleCase{
		{name: "direct growth", src: "rule main = e $$\nrule e = e \"+\" n / n\nrule n = [0-9]+",
			inputs: []string{"1", "1+2", "1+2+3", "1+2+", "1x"}},
		{name: "indirect growth", src: "rule main = a $$\nrule a = b \"1\" / b \"2\" / \"a\"\nrule b = a \"x\" / b \"y\" / \"b\"",
			inputs: []string{"b1", "by2", "ax2", "byy1x2", "b1x1y2", "byyyyyy2x1"}},
		{name: "growth read from a lookahead", src: "rule main = e $$\nrule e = &(e \"x\" t) e \"x\" / t\nrule t = [0-9]+",
			inputs: []string{"1x2", "1x2x3", "1x23x4", "1x2x"}},
		{name: "operators", src: "rule main = e $$\n" + ops,
			inputs: []string{"1", "1+2*3", "1 + 2 * 3 ** 4 ** 5", "-1+2", "2**-1", "1<2<3", "1<2", "(1+2)*3!", "1[2+3]!", "1+", "1 <", "(1", "1+-", ""}},
		{name: "level-restricted calls", src: "rule main = level(e, mul) (\",\" level(e, cmp))* $$\n" + ops,
			inputs: []string{"1*2", "1+2", "1*2,3+4<5", "-1+2", "1,2<3<4", "1*2,"}},
		{name: "operators with juxtaposition and cuts",
			src: "rule main = e $$\nrule e = #operators {\n operand [a-z]\n operand \"\"\n level { infix right \"\" }\n level { prefix \"-\" #cut \"z\" }\n level { postfix \"?\" #cut \"!\" }\n}",
			inputs: []string{"", "a", "ab", "abc", "-za", "-a", "a?!", "a?b", "a?"}},
		{name: "operators in left recursion", src: "rule main = s $$\nrule s = s \";\" e / e\n" + ops,
			inputs: []string{"1;2", "1+2;3*4", "1;", "1;2+"}},
		{name: "recovery", src: "rule main = stmt* $$\nrule stmt = (\"let\" \" \" [a-z]+ \";\") #recover(skip=[^;]* \";\")",
			inputs: []string{"let a;", "let a;let b;", "lex a;let b;", "let ;x;", "let a", ";;"}},
		{name: "recovery in left recursion", src: "rule main = e $$\nrule e = e \"+\" (n #recover(skip=[a-z]+)) / n\nrule n = [0-9]+",
			inputs: []string{"1+2", "1+x+2", "1+x", "1+", "x"}},
		{name: "token insertion", src: "rule main = \"(\" [a-z]* \")\" #missing \";\" #missing $$",
			inputs: []string{"(a);", "(a;", "(a)", "(a", "(", "a"}},
		{name: "token insertion in left recursion", src: "rule main = e $$\nrule e = e \"+\" #missing n / n\nrule n = [0-9]+",
			inputs: []string{"1+2", "12", "1+2 3", "1"}},
		{name: "predicates and variables",
			src:    "var depth int\nrule main = item* $$\nrule item = n:[0-9] { text($n) != \"0\" } { depth := depth + 1 } \"a\" / { depth > 1 } \"b\"",
			inputs: []string{"1a", "1a2ab", "1ab", "0a", "b", "1a2a3ab"}},
		{name: "bindings undone by backtracking",
			src:    "var x int\nrule main = ({ x := 1 } \"a\" \"b\" / \"a\") { x == 0 } \"c\" $$",
			inputs: []string{"ac", "abc", "ab"}},
		{name: "ignorecase", src: "rule main = (\"select\" kw)* $$ #ignorecase\nrule kw = \" \" \"from\"",
			inputs: []string{"select from", "SELECT from", "SeLeCt FROM", "select"}},
		{name: "inline actions", src: "rule main = (e => 1) $$\nrule e = (e \"+\" [0-9] => 2) / ([0-9] => 3)",
			inputs: []string{"1", "1+2", "1+2+", "x"}},
		{name: "invalid UTF-8", src: "rule main = (\"é\" / [a-z] / .)* $$",
			inputs: []string{"é", "\xc3", "\xe2\x82A", "a\xffb", "\xf0\x9f\x98", "\xf0\x9f\x98\x80"}},
		{name: "line anchors", src: "rule main = (^ [a-z]+ $ \"\\n\"?)* $$",
			inputs: []string{"ab\ncd", "ab\n", "ab cd", "\n\n"}},
	}
}

// outsideReference names the grammars of the corpus with inputs whose predicates read the value of
// a user action, which the reference does not compute.
var outsideReference = map[string]bool{}

// oneByte returns one byte per Read, so that a stream parse loads its input as late as it can.
type oneByte struct{ s string }

func (r *oneByte) Read(b []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	b[0], r.s = r.s[0], r.s[1:]
	return 1, nil
}

// observedParse is one parse of a route: the text parsed, the calls reported, the error and whether
// tokens were inserted.
type observedParse struct {
	text    string
	calls   []examinedref.Observed
	err     error
	missing bool
}

// route is a way to run an observed parse; it returns no parse when the grammar does not support it.
type route struct {
	name string
	run  func(p *engine.Parser, in string, u input.PositionUnit) []observedParse
}

// missingLimit is far above the insertions of the inputs, so that no parse reaches it.
const missingLimit = 1 << 20

func traced(rec *examinedref.Recorder, u input.PositionUnit) []engine.ParseOption {
	return []engine.ParseOption{engine.WithUnit(u), engine.WithTrace(rec.Trace)}
}

func once(text string, rec *examinedref.Recorder, err error, missing bool) []observedParse {
	if len(rec.Calls) == 0 {
		// The route refused the grammar before any call.
		return nil
	}
	return []observedParse{{text: text, calls: append([]examinedref.Observed(nil), rec.Calls...), err: err, missing: missing}}
}

// routes are the routes of every backend: Parse with expectations, Recognize, ParseStream, a Document
// parsed before and after edits, and Parse with token insertion, which only the closure backend
// supports.
func routes() []route {
	var out []route
	for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		name := b.String()
		out = append(out,
			route{name + "/parse", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
				var rec examinedref.Recorder
				_, err := p.OnBackend(b).Parse(in, append(traced(&rec, u), engine.WithExpectations())...)
				return once(in, &rec, err, false)
			}},
			route{name + "/recognize", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
				var rec examinedref.Recorder
				err := p.OnBackend(b).Recognize(in, traced(&rec, u)...)
				return once(in, &rec, err, false)
			}},
			route{name + "/stream", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
				var rec examinedref.Recorder
				err := p.OnBackend(b).ParseStream(&oneByte{in}, func(*value.Node) error { return nil }, traced(&rec, u)...)
				return once(in, &rec, err, false)
			}},
			route{name + "/document", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
				return documentParses(p.OnBackend(b), in, u)
			}},
		)
	}
	out = append(out, route{"closure/insertion", func(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
		var rec examinedref.Recorder
		_, err := p.OnBackend(engine.Closure).Parse(in, append(traced(&rec, u), engine.WithMissingTokens(missingLimit))...)
		return once(in, &rec, err, true)
	}})
	return out
}

// documentParses parses a Document of in with its middle character removed, inserts it, inserts a
// character at the start and removes it again, parsing after each edit, so that the parses after the
// edits reuse the results of the ones before.
func documentParses(p *engine.Parser, in string, u input.PositionUnit) []observedParse {
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
	d, err := p.NewDocument(text, engine.WithUnit(u))
	if err != nil {
		return nil
	}
	var out []observedParse
	parse := func(text string) bool {
		var rec examinedref.Recorder
		_, err := d.Parse(engine.WithTrace(rec.Trace), engine.WithExpectations())
		got := once(text, &rec, err, false)
		out = append(out, got...)
		return len(got) > 0
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
// error, such as a runtime error of a user action, which the reference does not run.
func aborted(err error) bool {
	if err == nil {
		return false
	}
	var errs value.SyntaxErrors
	var truncated *value.TruncatedSyntaxErrors
	return !errors.As(err, &errs) && !errors.As(err, &truncated)
}

// TestExaminedOracle checks the calls of the engine's observed parses against the reference on the
// generated corpus and the shapes, on every route, backend and position unit: every reported exit is
// an exit of the reference with the same match, end and examined end, every skipped call fails in the
// reference, and the start call returns exactly what the reference's does.
func TestExaminedOracle(t *testing.T) {
	t.Parallel()
	var cases []oracleCase
	for _, c := range codegentest.Representative(codegentest.GenCorpus(t)) {
		cases = append(cases, oracleCase{name: c.Name, src: c.Src, inputs: c.Inputs})
	}
	cases = append(cases, oracleShapes()...)

	var mu sync.Mutex
	compared := map[string]int{}
	var outside []string
	t.Run("cases", func(t *testing.T) {
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				g, err := enginetest.ParseGrammar(c.src)
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				p, err := enginetest.CompileSource(c.src, "main")
				if err != nil {
					t.Fatalf("compile: %v", err)
				}
				refs := &referenceRuns{g: g, runs: map[examinedref.Options]map[string]referenceRun{}}
				unsupported := false
				for _, in := range c.inputs {
					for _, u := range []input.PositionUnit{input.CodePoints, input.Bytes} {
						for _, r := range routes() {
							for _, op := range r.run(p, in, u) {
								ref := refs.get(op.text, examinedref.Options{Unit: u, MissingTokens: op.missing})
								var uns *examinedref.UnsupportedError
								if errors.As(ref.err, &uns) {
									unsupported = true
									continue
								}
								if ref.err != nil {
									t.Fatalf("%q: reference: %v", op.text, ref.err)
								}
								diffs := examinedref.CompareCalls(ref.exits, op.calls)
								if !aborted(op.err) {
									diffs = append(diffs, examinedref.CompareStart(ref.res, "main", op.calls)...)
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
				if unsupported != outsideReference[c.name] {
					if unsupported {
						t.Errorf("outside the reference, but not listed in outsideReference")
					} else {
						t.Errorf("listed in outsideReference, but the reference evaluates every input")
					}
				}
				if unsupported {
					mu.Lock()
					outside = append(outside, c.name)
					mu.Unlock()
				}
			})
		}
	})
	t.Logf("parses compared by route: %v; grammars outside the reference: %q", compared, outside)
	for _, r := range routes() {
		if compared[r.name] == 0 {
			t.Errorf("no parse of %s compared", r.name)
		}
	}
}
