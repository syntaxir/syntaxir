package print_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// streamPastEndGrammar is a stream whose elements try a rule past their end: the lookahead of an
// element calls num at the start of the next one, whose call then reuses what the lookahead built.
const streamPastEndGrammar = `@start
rule main = item* #stream !.
rule item = n:num ";" &(num ";")?
rule num = d:[0-9]+ {text($d) != ""}
`

// streamRecoverMemoGrammar is a stream whose elements recover errors in a rule that a lookahead calls
// first, so that the memo entry of the call holds the errors that the element reports.
const streamRecoverMemoGrammar = `@start
rule main = stmt* #stream !.
rule stmt = &body body
rule body = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;]* ";")
`

// streamPrefixGrammar is a stream whose start rule builds values before its #stream repetition.
const streamPrefixGrammar = `@start
rule main = header item* #stream !.
rule header = h:[a-z]+ ":" {text($h) != ""}
rule item = k:[0-9]+ ";" {text($k) != ""}
`

// tsStreamMemoryHeld feeds INPUT to a stream under a memory limit in chunks of SIZE bytes, and checks
// at each delivery of an element (streamEmit: the report of the recovered errors, the delivery and the
// commit) that the memory count returns nothing still referenced: a delivery after which the memo
// floor is where it was, so that no memo entry or skip record was discarded, does not lower the count,
// and the count never falls below the values built before the #stream repetition (streamStart). It
// prints the problems, or "ok".
const tsStreamMemoryHeld = `
import { createStream } from "./parser.ts";

const input = new TextEncoder().encode(INPUT);
const size = SIZE;
const { stream, error } = createStream({ limits: { memory: 1 << 30 }, diagnosticLimit: LIMIT });
if (stream === null) throw error;
const p = (stream as any).p;
const c = p.control;
const problems: string[] = [];
let prefix = 0;
const start = p.streamStart.bind(p);
p.streamStart = () => { prefix = p.values; start(); };
const emit = p.streamEmit.bind(p);
let emits = 0;
p.streamEmit = (v: any, end: number) => {
  const mem = c.mem, floor = p.memoFloor;
  emit(v, end);
  emits++;
  if (p.memoFloor === floor && c.mem < mem && problems.length < 5) problems.push("the delivery of the element ending at " + end + " lowers the count by " + (mem - c.mem) + " without a prune");
  if (c.mem < prefix && problems.length < 5) problems.push("after the element ending at " + end + " the count is " + c.mem + ", below the " + prefix + " bytes built before the stream");
};
for (let i = 0; ; i += size) {
  const end = Math.min(i + size, input.length);
  const r = stream.feed(input.subarray(i, end), end === input.length);
  if (r.state === "waiting") continue;
  if (r.state === "failed") throw r.error;
  break;
}
if (emits < 100) problems.push("only " + emits + " elements");
console.log(problems.length === 0 ? "ok" : problems.join("\n"));
`

// streamMemoryHeldCases are the streams of TestStreamMemoryHeldStreams, with their inputs and
// diagnostic limits.
var streamMemoryHeldCases = []struct {
	name, src, input string
	limit            int
}{
	{"prefix", streamPrefixGrammar, strings.Repeat("x", 100000) + ":" + strings.Repeat("12;", 3000), 0},
	{"past end", streamPastEndGrammar, strings.Repeat("12;", 3000), 0},
	{"recover memo", streamRecoverMemoGrammar, strings.Repeat("a=1;b;", 3000), 1},
}

// streamMemoryHeldOutput runs the stream of the case c, with its bundle changed by mutate if not nil,
// in chunks of 64 and of 7 bytes, and returns the outputs of tsStreamMemoryHeld that are not "ok".
func streamMemoryHeldOutput(t *testing.T, c int, mutate func(string) string) []string {
	t.Helper()
	cs := streamMemoryHeldCases[c]
	g, err := parseTest(cs.src)
	if err != nil {
		t.Fatal(err)
	}
	gen := codegentest.GenerateTSStreamBundle
	if mutate != nil {
		gen = func(g *ast.Grammar, o common.GenOptions) ([]byte, error) {
			b, err := codegentest.GenerateTSStreamBundle(g, o)
			return []byte(mutate(string(b))), err
		}
	}
	quoted, _ := json.Marshal(cs.input)
	var bad []string
	for _, size := range []int{64, 7} {
		script := strings.NewReplacer("INPUT", string(quoted), "SIZE", strconv.Itoa(size), "LIMIT", strconv.Itoa(cs.limit)).Replace(tsStreamMemoryHeld)
		out := strings.TrimSpace(codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main"}, gen))
		if out != "ok" {
			bad = append(bad, cs.name+", chunks of "+strconv.Itoa(size)+": "+out)
		}
	}
	return bad
}

// TestStreamMemoryHeldStreams checks on generated TypeScript streams that the memory count returns no
// storage that the parse still references (spec/runtime/streaming.md, Memory): the values that the
// start rule built before its #stream repetition, the values of elements that memo entries of a
// lookahead past their end may reference, and the recovered errors that memo entries hold when the
// diagnostic tail no longer retains them. TestStreamMemoryHeld checks the same on the library's
// operations with exact counts.
func TestStreamMemoryHeldStreams(t *testing.T) {
	t.Parallel()
	for i, c := range streamMemoryHeldCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if bad := streamMemoryHeldOutput(t, i, nil); len(bad) > 0 {
				t.Error(strings.Join(bad, "\n"))
			}
		})
	}
}

// TestStreamMemoryHeldStreamsMutants is the negative control of TestStreamMemoryHeldStreams: a stream
// that returns what is still referenced, on each path, fails it.
func TestStreamMemoryHeldStreamsMutants(t *testing.T) {
	t.Parallel()
	for _, m := range []struct {
		name, old, new string
		c              int
	}{
		{"prefix", "  streamStart(): void {\n    this.values = 0;\n  }", "  streamStart(): void {}", 0},
		{"release at commit", "if (this.values > 0) this.retired.push({ bytes: this.values, high: this.memoHigh });",
			"if (this.control !== null) this.control.mem -= this.values;", 1},
		{"tail", "if (c !== null) c.mem += syntaxErrorBytes(e) - (dropped === null ? 0 : syntaxErrorBytes(dropped));",
			"if (c !== null && dropped !== null) c.mem -= syntaxErrorBytes(dropped);", 2},
	} {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			mutate := func(src string) string {
				if n := strings.Count(src, m.old); n != 1 {
					t.Fatalf("the parser has %d occurrences of %q, want 1", n, m.old)
				}
				return strings.Replace(src, m.old, m.new, 1)
			}
			bad := streamMemoryHeldOutput(t, m.c, mutate)
			if len(bad) == 0 {
				t.Fatal("the mutant passes")
			}
			t.Log(strings.Join(bad, "\n"))
		})
	}
}
