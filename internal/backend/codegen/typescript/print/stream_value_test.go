package print_test

import (
	"encoding/json"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// typedStreamCases are the grammars of the typed stream test, which are those of the Go generator's
// typed stream test: struct, union, record, list and nil elements, recovered elements, matched text
// in both units, and streams with parts around the repetition, whose batch value is no list of the
// elements (batch false).
var typedStreamCases = []struct {
	name, src string
	batch     bool
	inputs    []string
}{
	{"structs", `type Pair = { Key node[string]; Value node[string] }
rule main = item* #stream
rule item Pair = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic "\n" => Pair{Key: $k, Value: $v}`, true, []string{"a=1\nbc=22\n", "a=1\nb=x\n", "", strings.Repeat("ab=12\n", 300)}},
	{"unions", `type A = { Text string }
type B = { Text string }
type U ~ A | B
rule main = item* #stream
rule item U = a / b
rule a A = t:("a") #atomic => A{Text: text($t)}
rule b B = t:("b") #atomic => B{Text: text($t)}`, true, []string{"abba", "abc", ""}},
	{"records", `rule main = item* #stream
rule item = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic ";"`, true, []string{"a=1;bc=22;", "a=1;b", strings.Repeat("ab=1;", 300)}},
	{"inline records", `rule main = (k:[a-z] v:[0-9]?)* #stream $$`, false, []string{"a1bc2", "a1!"}},
	{"recovered", `type S = { N node[string] }
rule main = (stmt #recover(skip=[^;]+ ";"))* #stream
rule stmt S = n:[a-z]+ #atomic ";" => S{N: $n}`, true, []string{"ab;1x;cd;9;", "1x;ab", "ab;"}},
	{"nil", `rule main = item* #stream
rule item = "a" => nil`, true, []string{"aaa", "ab"}},
	{"ints", `rule main = item* #stream
rule item int = d:[0-9]+ #atomic ";" => len($d)`, true, []string{"1;22;333;", "1;x"}},
	{"matches", `rule main = ("a" / "é")* #stream`, true, []string{"aéa", "é\xc3", "a\xffa"}},
	{"lists", `rule main = item* #stream
rule item = xs:[a-z]* ";" => $xs`, true, []string{"ab;;c;", "ab;c"}},
	{"surrounded", `rule main = h:"#" xs:item* #stream t:"!"?
rule item = [a-z] #atomic`, false, []string{"#ab!", "#", "#a?"}},
}

// tsStreamValue feeds each input to createStreamValue in chunks of every size and prints, for each
// input and unit, the typed elements of the whole input fed at once, whether every chunking gave the
// same elements and errors, whether createStream delivered as many elements with the same error, and,
// with batch, whether the elements are those of the repetition of the batch parseValue, whose error
// the stream has.
const tsStreamValue = `
import { createStream, createStreamValue, parseValueWithOptions, CodePoints, Bytes } from "./parser.ts";
import type { Unit, StreamValue } from "./parser.ts";

const inputs: string[] = JSON.parse(INPUTS);
const batch: boolean = BATCH;
const json = (v: unknown): string => JSON.stringify(v, (_, x) => typeof x === "bigint" ? x.toString() + "n" : x);
const run = (input: Uint8Array, unit: Unit, size: number): { elements: StreamValue[]; error: string } => {
  const { stream, error } = createStreamValue({ unit });
  if (stream === null) throw error;
  const elements: StreamValue[] = [];
  let i = 0;
  for (;;) {
    const end = Math.min(i + size, input.length);
    const r = stream.feed(input.subarray(i, end), end === input.length);
    elements.push(...r.elements);
    i = end;
    if (r.state !== "waiting") return { elements, error: String(r.error) };
    if (i === input.length) throw new Error("the stream waits after the final chunk");
  }
};
for (const s of inputs) {
  const input = new TextEncoder().encode(s);
  for (const unit of [CodePoints, Bytes]) {
    const whole = run(input, unit, Math.max(input.length, 1));
    let chunked = true;
    for (let size = 1; size < input.length; size++) {
      const r = run(input, unit, size);
      if (json(r.elements) !== json(whole.elements) || r.error !== whole.error) chunked = false;
    }
    const { stream } = createStream({ unit });
    const nodes = stream!.feed(input, true);
    const sameNodes = nodes.elements.length === whole.elements.length && String(nodes.error) === whole.error;
    let sameBatch = "-";
    if (batch) {
      const b = parseValueWithOptions(input, { unit });
      // A failed batch parse has no value: the stream then fails with its error after the elements it committed.
      const same = b.value === null || json((b.value as { value: unknown }).value) === json(whole.elements);
      sameBatch = String(same && String(b.error) === whole.error);
    }
    console.log(json(s), unit === Bytes ? "bytes" : "points", json(whole.elements), json(whole.error), chunked, sameNodes, sameBatch);
  }
}
`

// TestStreamValue checks createStreamValue, the stream of typed elements of a parser generated with
// types: the elements do not depend on the chunks, the stream delivers what createStream delivers, as
// typed values, and those are the elements of the repetition in the value of the batch parseValue.
func TestStreamValue(t *testing.T) {
	t.Parallel()
	for _, c := range typedStreamCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			g, err := parseTest(strings.Replace(c.src, "rule main", "@start\nrule main", 1))
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(c.inputs)
			quoted, _ := json.Marshal(string(data))
			script := strings.Replace(tsStreamValue, "INPUTS", string(quoted), 1)
			script = strings.Replace(script, "BATCH", map[bool]string{true: "true", false: "false"}[c.batch], 1)
			out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main", Types: true}, codegentest.GenerateTSStreamBundle)
			for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
				f := strings.Fields(line)
				if len(f) < 3 || f[len(f)-3] != "true" || f[len(f)-2] != "true" || (c.batch && f[len(f)-1] != "true") {
					t.Errorf("the typed stream differs across chunkings, from createStream or from parseValue: %s", line)
				}
			}
		})
	}
}

// tsStreamValueInterrupt cancels a typed stream while it converts an element: the signal reports
// aborted only when the conversion reads it (astConvert observes the signal every 256 steps), so the
// parse runs to the element, whose conversion takes more steps than that, and the interruption fails
// the stream after the elements before it, as one during the parse does. createStream, which converts
// nothing, never reads the signal from a conversion and parses the input.
const tsStreamValueInterrupt = `
import { createStream, createStreamValue, ParseInterruptionError } from "./parser.ts";

const signal = {
  get aborted(): boolean { return (new Error().stack ?? "").includes("astConvert"); },
  reason: "converting",
} as unknown as AbortSignal;
const input = new TextEncoder().encode("ab;" + "x".repeat(600) + ";cd;");
const { stream } = createStreamValue({ signal });
const r = stream!.feed(input, true);
const e = r.error;
console.log(r.state, r.elements.length, e instanceof ParseInterruptionError ? e.kind + " " + String(e.cause) : String(e));
console.log(stream!.feed(new Uint8Array(0), true).error?.message);
const nodes = createStream({ signal }).stream!.feed(input, true);
console.log(nodes.state, nodes.elements.length, nodes.error);
`

// TestStreamValueInterrupted checks that an interruption during the conversion of a typed element
// fails the stream with it, after the elements converted before.
func TestStreamValueInterrupted(t *testing.T) {
	t.Parallel()
	g, err := parseTest("@start\nrule main = item* #stream\nrule item = xs:[a-z]* \";\" => $xs\n")
	if err != nil {
		t.Fatal(err)
	}
	out := codegentest.RunTSGrammarScriptBundle(t, g, tsStreamValueInterrupt, common.GenOptions{Start: "main", Types: true}, codegentest.GenerateTSStreamBundle)
	want := "failed 1 canceled converting\nstream already failed\ndone 3 null\n"
	if out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}
