package print_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// streamOutcome is the outcome of a stream parse: the JSON of each element it delivered, in order, its
// error (empty for none), its compute units, the JSON of each recovered error that its diagnostic
// callback received, in order, and the JSON of each recovered error of its result.
type streamOutcome struct {
	Elements    []string `json:"elements"`
	Error       string   `json:"error"`
	Units       uint64   `json:"units"`
	Diagnostics []string `json:"diagnostics"`
	Recovered   []string `json:"recovered"`
}

// streamOptions are the options of the streams of an oracle run: a diagnostic limit if Limit is not
// nil, a diagnostic callback if Callback is set, which returns an error at its FailAt-th error if
// FailAt is positive, and the compute, input and memory limits Compute, Input and Memory (zero for the
// default). Memory counts depend on the backend (spec/runtime/parsing.md, Memory Limit), so the runs
// set a memory limit that no stream reaches: the streams count their memory, and the oracle checks that
// the count changes no outcome; TestStreamMemoryWindow checks the count itself. Observable generates
// the parser with observability, and Document generates it with a Document as well.
type streamOptions struct {
	Limit      *int   `json:"limit"`
	Callback   bool   `json:"callback"`
	FailAt     int    `json:"failAt"`
	Compute    uint64 `json:"compute"`
	Input      uint64 `json:"input"`
	Memory     uint64 `json:"memory"`
	Observable bool   `json:"observable"`
	Document   bool   `json:"document"`
}

// interruptionText describes the interruption of a parse by a limit in the same words for the engine
// and the generated TypeScript, whose messages differ: its kind, limit, units used and units requested.
// The units used at a compute limit are left out: a parse stops at the first check that finds the
// counter above the limit, and backends place their checks differently (spec/runtime/parsing.md,
// Overshoot), so only the decision is the same; the engine checks at every unit.
func interruptionText(kind string, limit, used, requested uint64) string {
	if kind == "compute" {
		return fmt.Sprintf("interrupted: compute limit %d, requested %d", limit, requested)
	}
	return fmt.Sprintf("interrupted: %s limit %d, used %d, requested %d", kind, limit, used, requested)
}

// diagnosticJSON is the JSON of a recovered error that a diagnostic callback receives.
func diagnosticJSON(e *value.SyntaxError) string {
	data, _ := json.Marshal(struct {
		Pos      int      `json:"pos"`
		Line     int      `json:"line"`
		Col      int      `json:"col"`
		Expected []string `json:"expected"`
		Messages []string `json:"messages"`
	}{e.Pos, e.Line, e.Col, append([]string{}, e.Expected...), append([]string{}, e.Messages...)})
	return string(data)
}

// engineStream returns the outcome of the engine's stream parse of in from the rule main of the
// program prog, in the position unit, with expectations and the options d.
func engineStream(prog *engine.Program, in string, unit input.PositionUnit, d streamOptions) streamOutcome {
	out := streamOutcome{Diagnostics: []string{}, Recovered: []string{}}
	o := engine.ParseOptions{Unit: unit, Expectations: true, ComputeReport: &out.Units}
	o.Limits.Compute, o.Limits.Input, o.Limits.Memory = d.Compute, d.Input, d.Memory
	if d.Limit != nil {
		o.DiagnosticLimit, o.DiagnosticLimitSet = *d.Limit, true
	}
	if d.Callback {
		o.DiagnosticCallback = func(e *value.SyntaxError) error {
			out.Diagnostics = append(out.Diagnostics, diagnosticJSON(e))
			// The callback owns its copy: changing it changes nothing.
			e.Expected = append(e.Expected, "changed")
			if len(out.Diagnostics) == d.FailAt {
				return errors.New(fmt.Sprint("stop at ", d.FailAt))
			}
			return nil
		}
	}
	err := prog.ParseStreamWith("main", strings.NewReader(in), func(n *value.Node) error {
		data, err := json.Marshal(n)
		if err != nil {
			return err
		}
		out.Elements = append(out.Elements, string(data))
		return nil
	}, o)
	if err != nil {
		out.Error = err.Error()
	}
	var limit *runtime.LimitError
	if errors.As(err, &limit) {
		kind := map[runtime.LimitKind]string{runtime.LimitCompute: "compute", runtime.LimitInput: "input", runtime.LimitDepth: "depth", runtime.LimitMemory: "memory"}[limit.Kind]
		out.Error = interruptionText(kind, limit.Limit, limit.Used, limit.Requested)
	}
	var recovered value.SyntaxErrors
	if errors.As(err, &recovered) {
		for _, e := range recovered {
			out.Recovered = append(out.Recovered, diagnosticJSON(e))
		}
	}
	return out
}

// tsStreamOracle is the script that feeds each input to createStream in each position unit, split
// into chunks in each chunking of chunkings, and prints a line for each input and unit: the outcome
// of the first chunking (the whole input in one final chunk), and the problems of the others: an
// outcome that differs from it, or a feed that breaks the protocol of StreamResult.
const tsStreamOracle = `
import { createStream, marshal, CodePoints, Bytes, SyntaxErrors, ParseInterruptionError } from "./parser.ts";
import type { Unit, StreamResult, StreamOptions } from "./parser.ts";

const inputs: string[] = JSON.parse(INPUTS);
const opts: { limit: number | null; callback: boolean; failAt: number; compute: number; input: number; memory: number } = JSON.parse(OPTIONS);

// chunkings returns the chunkings of n bytes, each a list of chunk ends, final on the last: the whole
// input, every chunk size up to 16 and some larger ones, and every split of a short input.
function chunkings(n: number): number[][] {
  const out: number[][] = [[n]];
  const sizes = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 31, 64, 257, 1000];
  for (const s of sizes) {
    if (s >= n) break;
    const ends: number[] = [];
    for (let at = s; at < n; at += s) ends.push(at);
    ends.push(n);
    out.push(ends);
  }
  if (n <= 10) {
    for (let mask = 1; mask < 1 << (n - 1); mask++) {
      const ends: number[] = [];
      for (let k = 1; k < n; k++) if ((mask & (1 << (k - 1))) !== 0) ends.push(k);
      ends.push(n);
      out.push(ends);
    }
  }
  return out;
}

// run feeds bytes split at ends, with an empty chunk after each one if empties is set and the end of
// the input in an empty final chunk if late is set, and returns the outcome and the problems.
function run(bytes: Uint8Array, unit: Unit, ends: number[], empties: boolean, late: boolean): { outcome: string; problems: string[] } {
  const report = { units: 0 };
  const diagnostics: string[] = [];
  const options: StreamOptions = { unit, expectations: true, computeReport: report, limits: { compute: opts.compute, input: opts.input, memory: opts.memory } };
  if (opts.limit !== null) options.diagnosticLimit = opts.limit;
  if (opts.callback) {
    options.diagnostic = (e) => {
      diagnostics.push(JSON.stringify({ pos: e.pos, line: e.line, col: e.col, expected: e.expected, messages: e.messages }));
      // The callback owns its copy: changing it changes nothing.
      e.expected.push("changed");
      return diagnostics.length === opts.failAt ? new Error("stop at " + opts.failAt) : null;
    };
  }
  const { stream, error } = createStream(options);
  if (stream === null) throw error;
  const elements: string[] = [];
  const problems: string[] = [];
  let last: StreamResult | null = null;
  const feed = (chunk: Uint8Array, final: boolean) => {
    const at = "feed of " + chunk.length + " bytes" + (final ? ", final" : "");
    const r = stream.feed(chunk, final);
    // The stream copies the chunk: changing it after the feed changes nothing.
    chunk.fill(0x3f);
    for (const e of r.elements) elements.push(e === null ? "null" : marshal(e));
    if (last !== null && last.state !== "waiting") {
      if (r.state !== last.state || r.error === null || r.elements.length !== 0) problems.push(at + " after the end: " + r.state + " " + String(r.error));
      return;
    }
    if (r.state === "waiting" && (final || r.error !== null)) problems.push(at + ": waiting with " + String(r.error));
    if (r.state === "done" && r.error !== null && r.error.name !== "SyntaxErrors" && r.error.name !== "TruncatedSyntaxErrors") problems.push(at + ": done with " + String(r.error));
    if (r.state === "failed" && r.error === null) problems.push(at + ": failed without an error");
    last = r;
  };
  let from = 0;
  for (let i = 0; i < ends.length; i++) {
    const to = ends[i]!;
    feed(bytes.slice(from, to), i === ends.length - 1 && !late);
    from = to;
    if (empties && i < ends.length - 1) feed(new Uint8Array(0), false);
  }
  if (late) feed(new Uint8Array(0), true);
  if (last === null || (last as StreamResult).state === "waiting") problems.push("the stream did not end");
  // A feed after the end changes nothing.
  feed(new Uint8Array([0x61]), true);
  const err = last === null ? null : (last as StreamResult).error;
  const kinds: { [k: string]: string } = { "compute-limit": "compute", "input-limit": "input", "depth-limit": "depth", "memory-limit": "memory" };
  let message = err === null ? "" : err.message;
  if (err instanceof ParseInterruptionError && err.kind === "compute-limit") {
    // The units used are past the limit, and counted as the compute units of the stream (see interruptionText).
    if (err.used <= err.limit || err.used !== report.units) problems.push("interrupted at " + err.used + " units of a limit of " + err.limit + ", " + report.units + " reported");
    message = "interrupted: compute limit " + err.limit + ", requested " + err.requested;
  } else if (err instanceof ParseInterruptionError && err.kind in kinds) {
    message = "interrupted: " + kinds[err.kind] + " limit " + err.limit + ", used " + err.used + ", requested " + err.requested;
  }
  const recovered = err instanceof SyntaxErrors ? err.errors.map((e) => JSON.stringify({ pos: e.pos, line: e.line, col: e.col, expected: e.expected, messages: e.messages })) : [];
  return { outcome: JSON.stringify({ elements, error: message, units: report.units, diagnostics, recovered }), problems };
}

// guarded is run that returns what a run throws as its outcome.
function guarded(bytes: Uint8Array, unit: Unit, ends: number[], empties: boolean, late: boolean): { outcome: string; problems: string[] } {
  try {
    return run(bytes, unit, ends, empties, late);
  } catch (x) {
    return { outcome: JSON.stringify({ elements: [], error: "thrown: " + String(x), units: 0, diagnostics: [], recovered: [] }), problems: [] };
  }
}

const lines: string[] = [];
for (const b64 of inputs) {
  const bytes = new Uint8Array(Buffer.from(b64, "base64"));
  for (const unit of [CodePoints, Bytes]) {
    const whole = guarded(bytes, unit, [bytes.length], false, false);
    const problems = whole.problems.slice();
    for (const ends of chunkings(bytes.length)) {
      for (const [empties, late] of [[false, false], [true, false], [false, true]] as const) {
        const r = guarded(bytes, unit, ends, empties, late);
        for (const p of r.problems) problems.push(JSON.stringify(ends) + ": " + p);
        if (r.outcome !== whole.outcome) problems.push(JSON.stringify(ends) + (empties ? " with empty chunks" : "") + (late ? " ending late" : "") + ": " + r.outcome);
      }
    }
    lines.push(JSON.stringify({ outcome: whole.outcome, problems }));
  }
}
console.log(lines.join("\n"));
`

// streamOracleMismatches generates the TypeScript stream of the grammar src, whose stream start rule
// is main, with expectations, changes its bundle with mutate if not nil, and returns how its outcomes
// on the inputs, in every chunking and in both position units, with the options d, differ
// from the engine's stream parse (ParseStream) of each input: in the elements, their positions, the
// error with its position, the compute units or the errors that the diagnostic callback receives. It
// also returns the feeds that break the protocol of StreamResult.
func streamOracleMismatches(t *testing.T, src string, inputs []string, d streamOptions, mutate func(string) string) []string {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	prog := compileBare(t, src, check.Options{NoProjections: true})
	encoded := make([]string, len(inputs))
	for i, in := range inputs {
		encoded[i] = base64.StdEncoding.EncodeToString([]byte(in))
	}
	data, _ := json.Marshal(encoded)
	quoted, _ := json.Marshal(string(data))
	script := strings.Replace(tsStreamOracle, "INPUTS", string(quoted), 1)
	opts, _ := json.Marshal(d)
	quotedOpts, _ := json.Marshal(string(opts))
	script = strings.Replace(script, "OPTIONS", string(quotedOpts), 1)
	out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main", Expectations: true, Observability: d.Observable, Document: d.Document}, func(g *ast.Grammar, opts common.GenOptions) ([]byte, error) {
		b, err := codegentest.GenerateTSStreamBundle(g, opts)
		if err != nil || mutate == nil {
			return b, err
		}
		mutated := mutate(string(b))
		if mutated == string(b) {
			return nil, fmt.Errorf("the mutation changes nothing")
		}
		return []byte(mutated), nil
	})
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	var mismatches []string
	k := 0
	for _, in := range inputs {
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			if k >= len(lines) {
				t.Fatalf("%d lines of results, want more", len(lines))
			}
			var got struct {
				Outcome  string
				Problems []string
			}
			if err := json.Unmarshal([]byte(lines[k]), &got); err != nil {
				t.Fatalf("%v: %s", err, lines[k])
			}
			k++
			var ts streamOutcome
			if err := json.Unmarshal([]byte(got.Outcome), &ts); err != nil {
				t.Fatalf("%v: %s", err, got.Outcome)
			}
			want := engineStream(prog, in, unit, d)
			name := fmt.Sprintf("input %q (%v, %s)", codegentest.Clip(in, 0), unit, opts)
			if fmt.Sprint(ts.Elements) != fmt.Sprint(want.Elements) {
				mismatches = append(mismatches, fmt.Sprintf("%s: elements\n generated %v\n engine    %v", name, ts.Elements, want.Elements))
			}
			if ts.Error != want.Error {
				mismatches = append(mismatches, fmt.Sprintf("%s: error\n generated %s\n engine    %s", name, ts.Error, want.Error))
			}
			if fmt.Sprint(ts.Diagnostics) != fmt.Sprint(want.Diagnostics) {
				mismatches = append(mismatches, fmt.Sprintf("%s: diagnostics\n generated %v\n engine    %v", name, ts.Diagnostics, want.Diagnostics))
			}
			if fmt.Sprint(ts.Recovered) != fmt.Sprint(want.Recovered) {
				mismatches = append(mismatches, fmt.Sprintf("%s: recovered errors\n generated %v\n engine    %v", name, ts.Recovered, want.Recovered))
			}
			if ts.Units != want.Units && !strings.HasPrefix(want.Error, "interrupted: compute") {
				mismatches = append(mismatches, fmt.Sprintf("%s: units: generated %d, engine %d", name, ts.Units, want.Units))
			}
			for _, p := range got.Problems {
				mismatches = append(mismatches, fmt.Sprintf("%s: %s", name, p))
			}
		}
	}
	return mismatches
}

// streamOracleCases are the grammars of the stream oracle, with their inputs: elements that end with
// a line feed and begin at a line start (^), which reads the unit before the commit point; memoized
// rules that backtrack within an element; an element that tries a rule past its end, whose memo
// entries the next element reuses; recovered errors; and inputs long enough to discard the input, prune the memo
// and count lines across discards, with syntax errors after them.
var streamOracleCases = []struct {
	name, src string
	inputs    []string
	// runs are the options of the runs; none is one run without them.
	runs []streamOptions
}{
	{"lines", `@start
rule main = line* #stream !.
rule line = ^ w:word ("," word)* "\n"
rule word = [a-zé€]*
`, []string{"", "\n", "a\n", "a,b\nc\n", "é,€\n\n", "a\nb", "a,\xff\n", "ab\n,", longLines(400, ""), longLines(400, "x;")}, limitRuns},
	{"items", `@start
rule main = item* #stream !.
rule item = pair ";" / word ";" / word "."
rule pair = k:word "=" v:word
rule word = [a-z€]+ "é!"?
`, []string{"", "a;", "a=b;", "a=b;c.d=e;", "ab=cé!;€.", "a=", "a;b", "a=b;;", longItems(600, ""), longItems(600, "=;")}, limitRuns},
	{"past the end", `@start
rule main = item* #stream !.
rule item = w:word ";" (word "!")?
rule word = [a-z]+ / "<" word ">"
`, []string{"", "a;", "a;b;", "a;b!", "a;;", "<a>;b!<c>;<", "a;b", longLookahead(2000)}, limitRuns},
	{"recover", `@start
rule main = stmt* #stream !.
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;]* ";")
`, []string{"", "a=1;", "a=1;b;c=2;", "?;", "a=1;b", "x=;y=2;", longRecover(300)}, recoverDiagnostics},
	{"nested recover", `@start
rule main = block* #stream !.
rule block = "{" stmt* "}" / stmt
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;}]* ";")
`, []string{"", "{a=1;b;}", "{a;b;c=1;}x;", "{a;}{b;}{c;", "y;{a=1;b;", longBlocks(120)}, recoverDiagnostics},
	{"adaptive", `@start
rule main = item* #stream !.
rule item = (&([a-z]* ";") [a-z])* ";"
`, adaptiveInputs(), nil},
	{"trailing literal", `@start
rule main = item* #stream "!"
rule item = [a-z]+ ";"
`, []string{"a;!b", "a;!!", "a;!", "!", "a;b;", "a;b;!", strings.Repeat("ab;", 200) + "!x"}, limitRuns},
	{"open end", `@start
rule main = item* #stream
rule item = [a-z]+ ";"
`, []string{"", "a;b;", "a;b", "a;;", strings.Repeat("ab;", 200)}, nil},
	{"text", `@start
rule main = item* #stream !.
rule item = k:key o:("=" / ":==") v:val ";" {text($k) != "x" && text($o) != "" && text($v) != "0"}
rule key = [a-z]+
rule val = [0-9]+ ("," [0-9]+)*
`, []string{"", "a=1;", "ab:==12,3;x=1;c=4;", "a=0;", "abc=1,22,333;" + strings.Repeat("key:==12,34,56;", 120)}, limitRuns},
	{"trailing recover", `@start
rule main = stmt* #stream fin !.
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;!]* ";")
rule fin = "!" #recover(skip=[^!]* "!")
`, []string{"!", "a=1;!", "a=1;b;x!", "b;c;d!e!", "x!", "a=1;b;c=2;d;yy", longRecover(200) + "zz!"}, recoverDiagnostics},
	{"recursive start", `@start
rule main = stmt* #stream fin
rule stmt = "(" main ")" / ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;!()]* ";")
rule fin = "!" #recover(skip=[^!()]* "!") / ""
`, []string{"", "(a=1;b;)", "a;(b;(c=1;d;)e;)f;", "(a;(b;!)c;)", "x;(y;!z!)!", "(a=1;b;", longNested(60)}, recoverDiagnostics},
}

// limitRuns are the options of the oracle cases without recoveries: the default limits, compute limits
// that interrupt streams at their start, in their middle and near their end, and input limits.
var limitRuns = []streamOptions{
	{},
	{Compute: 40},
	{Compute: 400},
	{Compute: 5000},
	{Input: 3},
	{Input: 50},
	{Input: 1500},
	{Memory: 1 << 30},
}

// recoverDiagnostics are the diagnostic options of the oracle cases with recoveries: none, a callback,
// limits that keep fewer errors than a stream recovers, a zero limit, which keeps them all, and
// callbacks that fail the stream at an error.
var recoverDiagnostics = []streamOptions{
	{},
	{Callback: true},
	{Limit: ptr(1)},
	{Limit: ptr(2), Callback: true},
	{Limit: ptr(0)},
	{Callback: true, FailAt: 1},
	{Callback: true, FailAt: 3},
	{Callback: true, Compute: 300},
	{Limit: ptr(2), Memory: 1 << 30},
}

// mutantCases names the one oracle case that a mutant runs on, for a mutant of a method that only the
// parsers of that case include.
var mutantCases = map[string]string{"range text": "text", "held text": "text"}

func ptr[T any](v T) *T { return &v }

// runsOf returns the options of the runs of an oracle case whose options are runs.
func runsOf(runs []streamOptions) []streamOptions {
	if len(runs) == 0 {
		return []streamOptions{{}}
	}
	return runs
}

// longNested returns n statements, some of them wrong and some nested in parentheses.
func longNested(n int) string {
	var b strings.Builder
	for i := range n {
		switch i % 3 {
		case 0:
			fmt.Fprintf(&b, "(v=%d;w;!)", i)
		case 1:
			b.WriteString("x;")
		default:
			fmt.Fprintf(&b, "(a=%d;(b;c=1;)!x!)", i)
		}
	}
	return b.String() + "!"
}

// adaptiveInputs are the inputs of the adaptive case, whose elements cost units quadratic in their
// length: elements past the floor of the default compute limit that its rise with the demanded input
// admits, elements past what it admits, alone and after cheap elements, and an element that it admits
// only after cheap elements.
func adaptiveInputs() []string {
	a := strings.Repeat
	return []string{
		a("a", 1500) + ";",
		a("a", 2100) + ";",
		a("a;", 300) + a("a", 2600) + ";",
		a("ab;", 400) + a("a", 2100) + ";",
	}
}

// longBlocks returns n blocks of statements, some of them wrong.
func longBlocks(n int) string {
	var b strings.Builder
	for i := range n {
		switch i % 4 {
		case 0:
			fmt.Fprintf(&b, "{v=%d;w;}", i)
		case 1:
			b.WriteString("x;")
		case 2:
			fmt.Fprintf(&b, "{a=%d;b=2;}", i)
		default:
			b.WriteString("{;;c=1;}")
		}
	}
	return b.String()
}

// longLines returns n lines of words, followed by tail.
func longLines(n int, tail string) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "w%c,é%c\n", 'a'+rune(i%26), 'a'+rune(i%7))
	}
	return strings.ReplaceAll(b.String(), "w", "") + tail
}

// longItems returns n items, followed by tail.
func longItems(n int, tail string) string {
	var b strings.Builder
	for i := range n {
		switch i % 3 {
		case 0:
			fmt.Fprintf(&b, "a%c=b;", 'a'+rune(i%26))
		case 1:
			b.WriteString("€é!;")
		default:
			b.WriteString("x.")
		}
	}
	return b.String() + tail
}

// longLookahead returns n items.
func longLookahead(n int) string {
	var b strings.Builder
	for i := range n {
		if i%4 == 3 {
			b.WriteString("<<x>>;")
		} else {
			fmt.Fprintf(&b, "%c;", 'a'+rune(i%26))
		}
	}
	return b.String()
}

// longRecover returns n statements, every fifth one wrong.
func longRecover(n int) string {
	var b strings.Builder
	for i := range n {
		if i%5 == 4 {
			b.WriteString("bad;")
		} else {
			fmt.Fprintf(&b, "v=%d;", i)
		}
	}
	return b.String()
}

// TestStreamOracle checks the TypeScript streams of the oracle grammars against the engine's stream
// parse: on each input, in each chunking and in both position units, a stream delivers the elements
// of the engine, with their positions, ends with its error, with its position, and counts its compute
// units, and every feed keeps the protocol of StreamResult.
func TestStreamOracle(t *testing.T) {
	t.Parallel()
	for _, c := range streamOracleCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, d := range runsOf(c.runs) {
				for _, m := range streamOracleMismatches(t, c.src, c.inputs, d, nil) {
					t.Error(m)
				}
			}
		})
	}
}

// TestStreamOracleObservable checks the streams of parsers generated with observability against the
// engine on the oracle cases: their stream table is lowered for observation, and a stream that is not
// observed parses as the stream of a parser without it.
func TestStreamOracleObservable(t *testing.T) {
	t.Parallel()
	for _, c := range streamOracleCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := runsOf(c.runs)[0]
			d.Observable = true
			for _, m := range streamOracleMismatches(t, c.src, c.inputs, d, nil) {
				t.Error(m)
			}
		})
	}
}

// TestStreamOracleDocument checks the streams of parsers generated with a Document as well against the
// engine on the oracle cases, observed and not: a package has both, as a Go package does, and its
// stream parses as the stream of a package without a Document.
func TestStreamOracleDocument(t *testing.T) {
	t.Parallel()
	for _, c := range streamOracleCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, observable := range []bool{false, true} {
				d := runsOf(c.runs)[0]
				d.Document, d.Observable = true, observable
				for _, m := range streamOracleMismatches(t, c.src, c.inputs, d, nil) {
					t.Error(m)
				}
			}
		})
	}
}

// storePolicyStreams returns the stream cases of the store policy (codegentest.StorePolicyCases), whose
// start rule is renamed main, as the oracle starts streams there.
func storePolicyStreams() []codegentest.StorePolicyCase {
	var out []codegentest.StorePolicyCase
	for _, c := range codegentest.StorePolicyCases() {
		if c.Stream {
			c.Src = strings.Replace(c.Src, "rule "+c.Start+" = ", "rule main = ", 1)
			out = append(out, c)
		}
	}
	return out
}

// TestStreamOracleStorePolicy runs the stream cases of the store policy of "Evaluations of Rules" in
// spec/runtime/parsing.md through the oracle: a stream memoizes a rule on its first call at a position,
// as the engine's stream does, so its compute units, which count each evaluation of the rule's body,
// are the engine's in every chunking, with and without a compute limit.
func TestStreamOracleStorePolicy(t *testing.T) {
	t.Parallel()
	cases := storePolicyStreams()
	if len(cases) == 0 {
		t.Fatal("no stream cases of the store policy")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			for _, d := range []streamOptions{{}, {Compute: 20}} {
				for _, m := range streamOracleMismatches(t, c.Src, []string{c.Input, c.Input + c.Input[:1]}, d, nil) {
					t.Error(m)
				}
			}
		})
	}
}

// TestStreamOracleStorePolicyMutant is the negative control of TestStreamOracleStorePolicy: a stream
// that defers the memoization of a first call, as a parse of the whole input does, evaluates rules
// that the engine's stream reuses.
func TestStreamOracleStorePolicyMutant(t *testing.T) {
	t.Parallel()
	const old = "export class StreamParser extends Parser {\n  override readonly deferMemo: boolean = false;"
	mutate := func(src string) string {
		if n := strings.Count(src, old); n != 1 {
			t.Fatalf("the parser has %d occurrences of %q, want 1", n, old)
		}
		return strings.Replace(src, old, "export class StreamParser extends Parser {\n  override readonly deferMemo: boolean = true;", 1)
	}
	found := 0
	for _, c := range storePolicyStreams() {
		found += len(streamOracleMismatches(t, c.Src, []string{c.Input}, streamOptions{}, mutate))
	}
	if found == 0 {
		t.Error("a stream that defers first calls matches the engine on every store-policy case")
	}
}

// TestStreamOptions checks the options of a stream: a stream of a parser generated without
// observability rejects the options of an observed parse, as a parse of the whole input does, a stream
// rejects invalid diagnostic options, and a parse of the whole input rejects the diagnostic options,
// which only a stream takes.
func TestStreamOptions(t *testing.T) {
	t.Parallel()
	g, err := parseTest("@start\nrule main = item* #stream !.\nrule item = [a-z]+ \";\"\n")
	if err != nil {
		t.Fatal(err)
	}
	script := `import { createStream, parseWithOptions } from "./parser.ts";
const options: unknown[] = [
  { trace: () => {} },
  { traceRules: [] },
  { diagnosticLimit: -1 },
  { diagnosticLimit: 1.5 },
  { diagnostic: 3 },
  { bogus: 1 },
  { diagnostic: null, diagnosticLimit: 0 },
];
for (const o of options) console.log(String(createStream(o as never).error));
console.log(String(parseWithOptions("a;", { diagnostic: () => null } as never).error));
console.log(String(parseWithOptions("a;", { diagnosticLimit: 0 } as never).error));
console.log(String(parseWithOptions("a;", { diagnostic: null } as never).error));
`
	out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main"}, codegentest.GenerateTSStreamBundle)
	want := `Error: trace and profile options are only supported by a parser generated with observability
Error: trace and profile options are only supported by a parser generated with observability
Error: diagnostic limit must be a non-negative safe integer: -1
Error: diagnostic limit must be a non-negative safe integer: 1.5
Error: diagnostic must be a function
Error: unknown parse option: bogus
null
Error: diagnostic options are only supported for stream parsing
Error: diagnostic options are only supported for stream parsing
null
`
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

// TestStreamObserved checks the lifetime of the observation of a stream: a profile records the stream as
// one parse over all its feeds, trace functions and profiles may be arrays, a value that a trace function
// throws leaves the feed unchanged and fails the stream, and the observation ends with the stream, so that
// a profile records nothing of the feeds after it.
func TestStreamObserved(t *testing.T) {
	t.Parallel()
	g, err := parseTest("@start\nrule main = item* #stream !.\nrule item = [a-z]+ \";\"\n")
	if err != nil {
		t.Fatal(err)
	}
	script := `import { createStream, Profile } from "./parser.ts";
const enc = (s: string) => new TextEncoder().encode(s);
const calls = (p: Profile) => p.snapshot().parses + " " + p.snapshot().calls;

const p = new Profile(), q = new Profile();
const kinds: string[] = [];
const a = createStream({ trace: [(e) => { kinds.push(e.kind); }, null], profile: [p, q] }).stream!;
console.log(a.feed(enc("ab;c")).state, calls(p), calls(q));
console.log(a.feed(enc("d;"), true).state, calls(p), calls(q), kinds.length);
a.feed(enc(""), true);
console.log(calls(p));

const r = new Profile();
let n = 0;
const b = createStream({ trace: (e) => { if (e.kind === "exit" && e.rule === "item" && ++n === 2) throw "boom"; }, profile: r }).stream!;
console.log(b.feed(enc("ab;")).state);
try { b.feed(enc("cd;")); } catch (x) { console.log("thrown", x); }
console.log(String(b.feed(enc("e;")).error), calls(r));
`
	out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main", Observability: true}, codegentest.GenerateTSStreamBundle)
	// A profile counts a call at its exit: after "ab;c" one item has exited, and the trace function
	// throws on the exit of the second item, which the profile has recorded before.
	want := `waiting 1 1 1 1
done 1 4 1 4 8
1 4
waiting
thrown boom
Error: stream already failed 1 2
`
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

// TestStreamObservedProfileBounded checks that the profile of a long stream forgets the positions that
// the stream discarded (spec/runtime/observability.md, Engine Profiling): the evaluation counts that
// its recorder keeps by position stay within the window that the stream holds, and the profile
// counts every call: the start rule, the 4000 items and the item that fails at the end. It reads the recorder, which the library does not export.
func TestStreamObservedProfileBounded(t *testing.T) {
	t.Parallel()
	g, err := parseTest("@start\nrule main = item* #stream !.\nrule item = [a-z]+ \";\"\n")
	if err != nil {
		t.Fatal(err)
	}
	script := `import { createStream, Profile } from "./parser.ts";
const p = new Profile();
const s = createStream({ profile: p }).stream!;
const item = new TextEncoder().encode("ab;");
let most = 0;
for (let k = 0; k < 4000; k++) {
  s.feed(item);
  let n = 0;
  for (const positions of (s as any).p.observation.recorders[0].seen.values()) n += positions.size;
  most = Math.max(most, n);
}
console.log(s.feed(new Uint8Array(0), true).state, p.snapshot().calls, most <= 2048 ? "bounded" : "kept " + most);
`
	out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main", Observability: true}, codegentest.GenerateTSStreamBundle)
	if want := "done 4002 bounded\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// TestStreamSignal checks the cancellation of a stream: an aborted signal fails createStream, a feed
// after the signal aborts fails the stream with the reason, a feed whose signal aborts while it parses
// returns the elements committed before, a signal getter that throws propagates from the feed and fails
// the stream, and a signal getter that feeds the stream gets a busy error.
func TestStreamSignal(t *testing.T) {
	t.Parallel()
	g, err := parseTest("@start\nrule main = item* #stream !.\nrule item = [a-z]+ \";\"\n")
	if err != nil {
		t.Fatal(err)
	}
	script := `import { createStream, ParseInterruptionError } from "./parser.ts";
import type { Stream } from "./parser.ts";
const enc = (s: string) => new TextEncoder().encode(s);
const show = (r: { elements: readonly unknown[]; state: string; error: Error | null }) =>
  r.elements.length + " " + r.state + " " + (r.error instanceof ParseInterruptionError ? r.error.kind + " " + String(r.error.cause) : String(r.error));

console.log(String(createStream({ signal: { aborted: true, reason: "early" } }).error));

const s1 = { aborted: false, reason: "stop" };
const a = createStream({ signal: s1 }).stream!;
console.log(show(a.feed(enc("a;b;c"))));
s1.aborted = true;
console.log(show(a.feed(enc("d;"))));
console.log(show(a.feed(enc("e;"), true)));

let reads = 0;
const s2 = { get aborted() { return ++reads > 6; }, reason: "late" };
const b = createStream({ signal: s2 }).stream!;
console.log(show(b.feed(enc("ab;".repeat(400)))));

let thrown = "";
const s3 = { get aborted(): boolean { throw new Error("getter"); } };
try { createStream({ signal: s3 }); } catch (x) { thrown = String(x); }
console.log(thrown);

let inner = "";
let e: Stream | null = null;
const s4 = { get aborted() { if (e !== null && inner === "") inner = show(e.feed(enc("x;"))); return false; } };
e = createStream({ signal: s4 }).stream!;
console.log(show(e.feed(enc("a;b;"), true)), "|", inner);

let calls = 0;
const s5 = { get aborted(): boolean { if (++calls === 2) throw new Error("getter " + calls); return false; } };
const f = createStream({ signal: s5 }).stream!;
try { f.feed(enc("a;")); } catch (x) { thrown = String(x); }
console.log(thrown, "|", show(f.feed(enc("b;"), true)));
`
	out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main"}, codegentest.GenerateTSStreamBundle)
	want := `ParseInterruptionError: parse canceled
2 waiting null
0 failed canceled stop
0 failed Error: stream already failed
2 failed canceled late
Error: getter
2 done null | 0 failed Error: stream is already feeding
Error: getter 2 | 0 failed Error: stream already failed
`
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

// TestStreamOracleReach checks that the runs of the oracle reach what they compare: on the engine, each
// case with diagnostic runs reports errors to a callback, ends with a truncated tail and is stopped by its
// callback, and each case with limit runs is interrupted by the compute limit and by the input limit and
// delivers elements before an interruption.
func TestStreamOracleReach(t *testing.T) {
	t.Parallel()
	for _, c := range streamOracleCases {
		prog := compileBare(t, c.src, check.Options{NoProjections: true})
		var reported, truncated, stopped, compute, inputs, before int
		var diagnostics, limits bool
		for _, d := range c.runs {
			diagnostics = diagnostics || d.Callback
			limits = limits || d.Input != 0
			for _, in := range c.inputs {
				o := engineStream(prog, in, input.CodePoints, d)
				reported += len(o.Diagnostics)
				switch {
				case strings.HasPrefix(o.Error, "recovered syntax errors truncated"):
					truncated++
				case strings.HasPrefix(o.Error, "stop at"):
					stopped++
				case strings.HasPrefix(o.Error, "interrupted: compute"):
					compute++
				case strings.HasPrefix(o.Error, "interrupted: input"):
					inputs++
				}
				if strings.HasPrefix(o.Error, "interrupted") && len(o.Elements) > 0 {
					before++
				}
			}
		}
		if diagnostics && (reported == 0 || truncated == 0 || stopped == 0) {
			t.Errorf("%s: %d reported errors, %d truncated tails, %d stopped streams; want some of each", c.name, reported, truncated, stopped)
		}
		if limits && (compute == 0 || inputs == 0 || before == 0) {
			t.Errorf("%s: %d compute and %d input interruptions, %d after elements; want some of each", c.name, compute, inputs, before)
		}
		if c.name == "adaptive" {
			// The default limit interrupts the second and third inputs, the third after elements, and
			// admits the first and the last, which spend more than its floor.
			var got []string
			for _, in := range c.inputs {
				o := engineStream(prog, in, input.CodePoints, streamOptions{})
				got = append(got, fmt.Sprintf("%v %d %v", strings.HasPrefix(o.Error, "interrupted: compute"), len(o.Elements), o.Units > 1<<20))
			}
			if want := "[false 1 true true 0 true true 300 true false 401 true]"; fmt.Sprint(got) != want {
				t.Errorf("adaptive: %v, want %s", got, want)
			}
		}
	}
}

// TestStreamOracleMutants checks that the oracle tells each mechanism of the stream: a stream whose
// mechanism is broken differs from the engine on the oracle grammars.
func TestStreamOracleMutants(t *testing.T) {
	t.Parallel()
	for _, m := range []struct{ name, old, new string }{
		// The commit keeps the unit before the commit point, which a line start reads.
		{"discard", "this.streamDiscard(pos - 1);", "this.streamDiscard(pos);"},
		// The end of the input is the final chunk, not the end of the chunks fed so far.
		{"final", "if (final) p.end();", "if (final) p.final = false;"},
		// A read at the end of the buffer suspends unless the input is final.
		{"buffer end", "if (this.pos >= this.n) return 1;", "if (this.pos >= this.n) return 0;"},
		// A feed returns the elements committed during it, once.
		{"delivery", `return { elements: p.delivered as E[], state: "waiting", error: null };`, `return { elements: [], state: "waiting", error: null };`},
		// The memo of a position is found at its distance from the floor, after each prune.
		{"memo prune", "this.gapLen = -pos;", "this.gapLen = -k;"},
		// The buffer starts at base after a discard.
		{"window", "this.base = keep;", "this.base = keep - 1;"},
		// The line and column of base follow the discards.
		{"lines", "this.baseLine = this.errorLine(keep);", "this.errorLine(keep);"},
		// In code points a character whose bytes may continue in the next chunk waits for them.
		{"pending", "if (!this.final && decodedEnd(b, i, b.length, decodedSize) > b.length) break;", "if (decodedEnd(b, i, b.length, decodedSize) > b.length + 9) break;"},
		// The recovered errors before an element are reported before it is delivered, not at the end.
		{"report", "const err = this.reportRecovered();\n    if (err !== null) throw new Fatal(err);", "return;"},
		// The tail holds the latest errors in the order they occurred.
		{"tail order", "const ordered = [...this.errors.slice(this.head), ...this.errors.slice(0, this.head)];", "const ordered = this.errors.slice();"},
		// A zero limit retains every error.
		{"zero limit", "if (limit > 0) p.tail = new DiagnosticTail(limit);", "if (limit >= 0) p.tail = new DiagnosticTail(limit);"},
		// The reports start again with the record that a commit empties.
		{"settle", "this.recovered.length = 0;\n    this.diagnosed = 0;", "this.recovered.length = 0;"},
		// The recovered errors after the last element are reported before the result.
		{"final report", "const err = p.reportRecovered();\n        if (err !== null) return this.end(null, err);", ""},
		// The default compute limit rises with the demanded input.
		{"adaptive raise", "if (c.raise(defaultBudget(d))) this.unitsCheck = c.unitsCheck();", ""},
		// The input limit fails a stream that demands a unit past it.
		{"input limit", "d > this.maxInput && this.n > this.maxInput", "d > this.maxInput + 1 && this.n > this.maxInput + 1"},
		// In a stream the text of a node counts a unit for each position of its range.
		{"range text", "if (this.control !== null) this.unitsSized(end - start);\n    return this.text(start, end);", "return this.text(start, end);"},
		// So does the text that a string node holds.
		{"held text", "if (n.terminal && this.control !== null && nodeShape(n) !== shapeError) this.unitsSized(n.end - n.start);", ""},
		// The stream ends only when the input does: the end check after the start rule waits for it.
		{"end wait", "while (!p.final && p.pos >= p.n) yield 1;", ""},
		// The callback receives a copy of its own.
		{"copy", "callback(bareSyntaxError(e.pos, e.line, e.col, e.expected.slice(), e.messages.slice()))", "callback(e)"},
	} {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			mutate := func(src string) string {
				if n := strings.Count(src, m.old); n != 1 {
					t.Fatalf("the parser has %d occurrences of %q, want 1", n, m.old)
				}
				return strings.Replace(src, m.old, m.new, 1)
			}
			found := 0
			for _, c := range streamOracleCases {
				if only, ok := mutantCases[m.name]; ok && c.name != only {
					continue
				}
				for _, d := range runsOf(c.runs) {
					found += len(streamOracleMismatches(t, c.src, c.inputs, d, mutate))
				}
			}
			if found == 0 {
				t.Error("the oracle finds no difference")
			}
		})
	}
}
