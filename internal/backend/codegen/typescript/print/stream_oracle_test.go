package print_test

import (
	"encoding/base64"
	"encoding/json"
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
// error (empty for none) and its compute units.
type streamOutcome struct {
	Elements []string `json:"elements"`
	Error    string   `json:"error"`
	Units    uint64   `json:"units"`
}

// engineStream returns the outcome of the engine's stream parse of in from the rule main of the
// program prog, in the position unit, with expectations.
func engineStream(prog *engine.Program, in string, unit input.PositionUnit) streamOutcome {
	var out streamOutcome
	err := prog.ParseStreamWith("main", strings.NewReader(in), func(n *value.Node) error {
		data, err := json.Marshal(n)
		if err != nil {
			return err
		}
		out.Elements = append(out.Elements, string(data))
		return nil
	}, engine.ParseOptions{Unit: unit, Expectations: true, ComputeReport: &out.Units})
	if err != nil {
		out.Error = err.Error()
	}
	return out
}

// tsStreamOracle is the script that feeds each input to createStream in each position unit, split
// into chunks in each chunking of chunkings, and prints a line for each input and unit: the outcome
// of the first chunking (the whole input in one final chunk), and the problems of the others: an
// outcome that differs from it, or a feed that breaks the protocol of StreamResult.
const tsStreamOracle = `
import { createStream, marshal, CodePoints, Bytes } from "./parser.ts";
import type { Unit, StreamResult } from "./parser.ts";

const inputs: string[] = JSON.parse(INPUTS);

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
  const { stream, error } = createStream({ unit, expectations: true, computeReport: report });
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
    if (r.state === "done" && r.error !== null && r.error.name !== "SyntaxErrors") problems.push(at + ": done with " + String(r.error));
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
  return { outcome: JSON.stringify({ elements, error: err === null ? "" : err.message, units: report.units }), problems };
}

// guarded is run that returns what a run throws as its outcome.
function guarded(bytes: Uint8Array, unit: Unit, ends: number[], empties: boolean, late: boolean): { outcome: string; problems: string[] } {
  try {
    return run(bytes, unit, ends, empties, late);
  } catch (x) {
    return { outcome: JSON.stringify({ elements: [], error: "thrown: " + String(x), units: 0 }), problems: [] };
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
// on the inputs, in every chunking and in both position units, differ from the engine's stream parse
// (ParseStream) of each input: in the elements, their positions, the error with its position, or the
// compute units. It also returns the feeds that break the protocol of StreamResult.
func streamOracleMismatches(t *testing.T, src string, inputs []string, mutate func(string) string) []string {
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
			want := engineStream(prog, in, unit)
			name := fmt.Sprintf("input %q (%v)", codegentest.Clip(in, 0), unit)
			if fmt.Sprint(ts.Elements) != fmt.Sprint(want.Elements) {
				mismatches = append(mismatches, fmt.Sprintf("%s: elements\n generated %v\n engine    %v", name, ts.Elements, want.Elements))
			}
			if ts.Error != want.Error {
				mismatches = append(mismatches, fmt.Sprintf("%s: error\n generated %s\n engine    %s", name, ts.Error, want.Error))
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
}{
	{"lines", `@start
rule main = line* #stream !.
rule line = ^ w:word ("," word)* "\n"
rule word = [a-zé€]*
`, []string{"", "\n", "a\n", "a,b\nc\n", "é,€\n\n", "a\nb", "a,\xff\n", "ab\n,", longLines(400, ""), longLines(400, "x;")}},
	{"items", `@start
rule main = item* #stream !.
rule item = pair ";" / word ";" / word "."
rule pair = k:word "=" v:word
rule word = [a-z€]+ "é!"?
`, []string{"", "a;", "a=b;", "a=b;c.d=e;", "ab=cé!;€.", "a=", "a;b", "a=b;;", longItems(600, ""), longItems(600, "=;")}},
	{"past the end", `@start
rule main = item* #stream !.
rule item = w:word ";" (word "!")?
rule word = [a-z]+ / "<" word ">"
`, []string{"", "a;", "a;b;", "a;b!", "a;;", "<a>;b!<c>;<", "a;b", longLookahead(2000)}},
	{"recover", `@start
rule main = stmt* #stream !.
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;]* ";")
`, []string{"", "a=1;", "a=1;b;c=2;", "?;", "a=1;b", "x=;y=2;", longRecover(300)}},
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
			for _, m := range streamOracleMismatches(t, c.src, c.inputs, nil) {
				t.Error(m)
			}
		})
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
				found += len(streamOracleMismatches(t, c.src, c.inputs, mutate))
			}
			if found == 0 {
				t.Error("the oracle finds no difference")
			}
		})
	}
}
