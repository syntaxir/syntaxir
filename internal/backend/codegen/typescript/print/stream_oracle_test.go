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

// streamDiagnostics are the diagnostic options of the streams of an oracle run: a diagnostic limit if
// Limit is not nil, and a diagnostic callback if Callback is set, which returns an error at its
// FailAt-th error if FailAt is positive.
type streamDiagnostics struct {
	Limit    *int `json:"limit"`
	Callback bool `json:"callback"`
	FailAt   int  `json:"failAt"`
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
// program prog, in the position unit, with expectations and the diagnostic options d.
func engineStream(prog *engine.Program, in string, unit input.PositionUnit, d streamDiagnostics) streamOutcome {
	out := streamOutcome{Diagnostics: []string{}, Recovered: []string{}}
	o := engine.ParseOptions{Unit: unit, Expectations: true, ComputeReport: &out.Units}
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
import { createStream, marshal, CodePoints, Bytes, SyntaxErrors } from "./parser.ts";
import type { Unit, StreamResult, StreamOptions } from "./parser.ts";

const inputs: string[] = JSON.parse(INPUTS);
const diag: { limit: number | null; callback: boolean; failAt: number } = JSON.parse(DIAG);

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
  const options: StreamOptions = { unit, expectations: true, computeReport: report };
  if (diag.limit !== null) options.diagnosticLimit = diag.limit;
  if (diag.callback) {
    options.diagnostic = (e) => {
      diagnostics.push(JSON.stringify({ pos: e.pos, line: e.line, col: e.col, expected: e.expected, messages: e.messages }));
      // The callback owns its copy: changing it changes nothing.
      e.expected.push("changed");
      return diagnostics.length === diag.failAt ? new Error("stop at " + diag.failAt) : null;
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
  const recovered = err instanceof SyntaxErrors ? err.errors.map((e) => JSON.stringify({ pos: e.pos, line: e.line, col: e.col, expected: e.expected, messages: e.messages })) : [];
  return { outcome: JSON.stringify({ elements, error: err === null ? "" : err.message, units: report.units, diagnostics, recovered }), problems };
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
// on the inputs, in every chunking and in both position units, with the diagnostic options d, differ
// from the engine's stream parse (ParseStream) of each input: in the elements, their positions, the
// error with its position, the compute units or the errors that the diagnostic callback receives. It
// also returns the feeds that break the protocol of StreamResult.
func streamOracleMismatches(t *testing.T, src string, inputs []string, d streamDiagnostics, mutate func(string) string) []string {
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
	diag, _ := json.Marshal(d)
	quotedDiag, _ := json.Marshal(string(diag))
	script = strings.Replace(script, "DIAG", string(quotedDiag), 1)
	out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main", Expectations: true}, func(g *ast.Grammar, opts common.GenOptions) ([]byte, error) {
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
			name := fmt.Sprintf("input %q (%v, %s)", codegentest.Clip(in, 0), unit, diag)
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
			if ts.Units != want.Units {
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
	// diags are the diagnostic options of the runs; none is one run without them.
	diags []streamDiagnostics
}{
	{"lines", `@start
rule main = line* #stream !.
rule line = ^ w:word ("," word)* "\n"
rule word = [a-zé€]*
`, []string{"", "\n", "a\n", "a,b\nc\n", "é,€\n\n", "a\nb", "a,\xff\n", "ab\n,", longLines(400, ""), longLines(400, "x;")}, nil},
	{"items", `@start
rule main = item* #stream !.
rule item = pair ";" / word ";" / word "."
rule pair = k:word "=" v:word
rule word = [a-z€]+ "é!"?
`, []string{"", "a;", "a=b;", "a=b;c.d=e;", "ab=cé!;€.", "a=", "a;b", "a=b;;", longItems(600, ""), longItems(600, "=;")}, nil},
	{"past the end", `@start
rule main = item* #stream !.
rule item = w:word ";" (word "!")?
rule word = [a-z]+ / "<" word ">"
`, []string{"", "a;", "a;b;", "a;b!", "a;;", "<a>;b!<c>;<", "a;b", longLookahead(2000)}, nil},
	{"recover", `@start
rule main = stmt* #stream !.
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;]* ";")
`, []string{"", "a=1;", "a=1;b;c=2;", "?;", "a=1;b", "x=;y=2;", longRecover(300)}, recoverDiagnostics},
	{"nested recover", `@start
rule main = block* #stream !.
rule block = "{" stmt* "}" / stmt
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;}]* ";")
`, []string{"", "{a=1;b;}", "{a;b;c=1;}x;", "{a;}{b;}{c;", "y;{a=1;b;", longBlocks(120)}, recoverDiagnostics},
	{"trailing recover", `@start
rule main = stmt* #stream fin !.
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;!]* ";")
rule fin = "!" #recover(skip=[^!]* "!")
`, []string{"!", "a=1;!", "a=1;b;x!", "b;c;d!e!", "x!", "a=1;b;c=2;d;yy", longRecover(200) + "zz!"}, recoverDiagnostics},
	{"recursive start", `@start
rule main = stmt* #stream fin
rule stmt = "(" main ")" / ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;!()]* ";")
rule fin = "!" #recover(skip=[^!()]* "!") / ""
`, []string{"", "(a=1;b;)", "a;(b;(c=1;d;)e;)f;", "(a;(b;!)c;)", "x;(y;!z!)!", "(a=1;b;" , longNested(60)}, recoverDiagnostics},
}

// recoverDiagnostics are the diagnostic options of the oracle cases with recoveries: none, a callback,
// limits that keep fewer errors than a stream recovers, a zero limit, which keeps them all, and
// callbacks that fail the stream at an error.
var recoverDiagnostics = []streamDiagnostics{
	{},
	{Callback: true},
	{Limit: ptr(1)},
	{Limit: ptr(2), Callback: true},
	{Limit: ptr(0)},
	{Callback: true, FailAt: 1},
	{Callback: true, FailAt: 3},
}

func ptr[T any](v T) *T { return &v }

// runsOf returns the diagnostic options of the runs of an oracle case whose options are diags.
func runsOf(diags []streamDiagnostics) []streamDiagnostics {
	if len(diags) == 0 {
		return []streamDiagnostics{{}}
	}
	return diags
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
			for _, d := range runsOf(c.diags) {
				for _, m := range streamOracleMismatches(t, c.src, c.inputs, d, nil) {
					t.Error(m)
				}
			}
		})
	}
}

// TestStreamOptions checks the options of a stream: it rejects the options of an observed parse and
// invalid diagnostic options, and a parse of the whole input rejects the diagnostic options, which
// only a stream takes.
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
	want := `Error: a stream is not observed: it takes no trace or profile options
Error: a stream is not observed: it takes no trace or profile options
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

// TestStreamOracleDiagnosticsReach checks that the diagnostic runs of the oracle reach what they compare:
// on the engine, each case with recoveries reports errors to a callback, ends with a truncated tail and
// is stopped by its callback, and a list of recovered errors (nested recover) is reported.
func TestStreamOracleDiagnosticsReach(t *testing.T) {
	t.Parallel()
	for _, c := range streamOracleCases {
		if c.diags == nil {
			continue
		}
		prog := compileBare(t, c.src, check.Options{NoProjections: true})
		var reported, truncated, stopped int
		for _, d := range c.diags {
			for _, in := range c.inputs {
				o := engineStream(prog, in, input.CodePoints, d)
				reported += len(o.Diagnostics)
				if strings.HasPrefix(o.Error, "recovered syntax errors truncated") {
					truncated++
				}
				if strings.HasPrefix(o.Error, "stop at") {
					stopped++
				}
			}
		}
		if reported == 0 || truncated == 0 || stopped == 0 {
			t.Errorf("%s: %d reported errors, %d truncated tails, %d stopped streams; want some of each", c.name, reported, truncated, stopped)
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
		{"delivery", `return { elements: p.delivered, state: "waiting", error: null };`, `return { elements: [], state: "waiting", error: null };`},
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
				for _, d := range runsOf(c.diags) {
					found += len(streamOracleMismatches(t, c.src, c.inputs, d, mutate))
				}
			}
			if found == 0 {
				t.Error("the oracle finds no difference")
			}
		})
	}
}
