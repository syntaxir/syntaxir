package print_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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

// streamMemoryCases are the grammars of the memory test, each with a function that returns an input of
// n elements: elements whose actions build nodes and copy text out of the input, memoized rules whose
// entries a call outside a lookahead replaces, recovery skips whose walks keep skip records, lookaheads
// past the end of an element, and recovered errors with and without a diagnostic limit (limit, 0 for
// none), also held by memo entries.
var streamMemoryCases = []struct {
	name, src string
	input     func(n int) string
	limit     int
	// grows says that the count grows with the length of the stream: recovered errors without a
	// diagnostic limit are retained for the result.
	grows bool
}{
	{"text", `@start
rule main = item* #stream !.
rule item = k:key o:("=" / ":==") v:val ";" {text($k) != "x" && text($o) != "" && text($v) != "0"}
rule key = [a-z]+
rule val = [0-9]+ ("," [0-9]+)*
`, func(n int) string { return strings.Repeat("key:==12,34,56;", n) }, 0, false},
	{"memo", `@start
rule main = item* #stream !.
rule item = &(pair "!") pair / -pair "?" / pair ";" / pair "." / word ";"
rule pair = k:word "=" v:word ("," word)*
rule word = [a-z]+ "!"?
`, func(n int) string { return strings.Repeat("ab=cd,x;ef=g.gh!;", n) }, 0, false},
	{"recover limited", `@start
rule main = stmt* #stream !.
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;]* ";")
`, func(n int) string { return strings.Repeat("a=1;b;", n) }, 2, false},
	{"skip records", `@start
rule main = item* #stream !.
rule item = x / .
rule x = ("b" #recover(skip=(word / [a-z ])*)) "c"
rule word = [a-z]+ !"="
`, func(n int) string { return strings.Repeat("bab abc cxbaa a!c", n) }, 1, false},
	{"past end", streamPastEndGrammar, func(n int) string { return strings.Repeat("12;", n) }, 0, false},
	{"recover memo", streamRecoverMemoGrammar, func(n int) string { return strings.Repeat("a=1;b;", n) }, 1, false},
	{"recover", `@start
rule main = stmt* #stream !.
rule stmt = ([a-z]+ "=" [0-9]+ ";") #recover(skip=[^;]* ";")
`, func(n int) string { return strings.Repeat("a=1;b;", n) }, 0, true},
}

// tsStreamMemory finds, for each input, the least memory limit under which the stream of the input fed
// in chunks of SIZE bytes is done, and prints them.
const tsStreamMemory = `
import { createStream, ParseInterruptionError } from "./parser.ts";

const inputs: string[] = JSON.parse(INPUTS);
const size = SIZE;
const limit = LIMIT;
const passes = (input: Uint8Array, memory: number): boolean => {
  const { stream, error } = createStream({ limits: { memory }, diagnosticLimit: limit });
  if (stream === null) throw error;
  for (let i = 0; ; i += size) {
    const end = Math.min(i + size, input.length);
    const r = stream.feed(input.subarray(i, end), end === input.length);
    if (r.state === "waiting") continue;
    if (r.state === "done") return true;
    if (r.error instanceof ParseInterruptionError && r.error.kind === "memory-limit") return false;
    throw r.error;
  }
};
const least: number[] = [];
for (const s of inputs) {
  const input = new TextEncoder().encode(s);
  let lo = 0, hi = 1 << 30;
  while (lo + 1 < hi) {
    const mid = Math.floor((lo + hi) / 2);
    if (passes(input, mid)) hi = mid; else lo = mid;
  }
  least.push(hi);
}
console.log(least.join(" "));
`

// engineLeastMemory returns the least memory limit under which the engine's stream parse of in is done.
func engineLeastMemory(t *testing.T, src, in string, limit int) uint64 {
	t.Helper()
	prog := compileBare(t, src, check.Options{NoProjections: true})
	lo, hi := uint64(0), uint64(1)<<30
	for lo+1 < hi {
		mid := (lo + hi) / 2
		o := engine.ParseOptions{Unit: input.CodePoints}
		o.Limits.Memory = mid
		if limit > 0 {
			o.DiagnosticLimit, o.DiagnosticLimitSet = limit, true
		}
		err := prog.ParseStreamWith("main", strings.NewReader(in), func(*value.Node) error { return nil }, o)
		var le *runtime.LimitError
		var se value.SyntaxErrors
		switch {
		case err == nil || errors.As(err, &se):
			hi = mid
		case errors.As(err, &le) && le.Kind == runtime.LimitMemory:
			lo = mid
		default:
			t.Fatalf("the engine: %v", err)
		}
	}
	return hi
}

// The lengths of the memory test's streams, both past the buffer through which the engine reads the
// first kilobytes of its input whole.
const memoryShort, memoryLong = 2000, 6000

// memoryGrew reports that the least memory limit of the long stream is past that of the short one by
// at least 40 bytes for each element more: a retained error takes more on every backend, and a window
// holds a few elements.
func memoryGrew(least [2]uint64) bool { return least[1] > least[0]+40*(memoryLong-memoryShort) }

// streamMemoryProblems returns how the TypeScript streams of the case c, with its bundle changed by
// mutate if not nil, fed in chunks, fail the window of the memory count: their least memory limits grow
// with the length unless c grows, which the engine's least limits eng decide, or the least limit of a
// stream that does not grow is past that of the short stream by more than a quarter.
func streamMemoryProblems(t *testing.T, c int, eng [2]uint64, mutate func(string) string) []string {
	t.Helper()
	cs := streamMemoryCases[c]
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
	data, _ := json.Marshal([]string{cs.input(memoryShort), cs.input(memoryLong)})
	quoted, _ := json.Marshal(string(data))
	var bad []string
	for _, size := range []int{64, 7} {
		script := strings.NewReplacer("INPUTS", string(quoted), "SIZE", strconv.Itoa(size), "LIMIT", strconv.Itoa(cs.limit)).Replace(tsStreamMemory)
		out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main"}, gen)
		var ts [2]uint64
		if _, err := fmt.Sscan(out, &ts[0], &ts[1]); err != nil {
			t.Fatalf("%v: %q", err, out)
		}
		if memoryGrew(ts) != cs.grows {
			bad = append(bad, fmt.Sprintf("%s, chunks of %d: the least memory limit of %d elements is %d, of %d elements %d; the engine's %d and %d, want growth %v", cs.name, size, memoryShort, ts[0], memoryLong, ts[1], eng[0], eng[1], cs.grows))
		}
		if !cs.grows && ts[1] > ts[0]+ts[0]/4 {
			bad = append(bad, fmt.Sprintf("%s, chunks of %d: the least memory limit of %d elements is %d, of %d elements %d; want the same window", cs.name, size, memoryShort, ts[0], memoryLong, ts[1]))
		}
	}
	return bad
}

// engineMemoryLeast returns the engine's least memory limits of the short and the long stream of the
// case c, and fails the test if their growth is not that of the case.
func engineMemoryLeast(t *testing.T, c int) [2]uint64 {
	t.Helper()
	cs := streamMemoryCases[c]
	eng := [2]uint64{engineLeastMemory(t, cs.src, cs.input(memoryShort), cs.limit), engineLeastMemory(t, cs.src, cs.input(memoryLong), cs.limit)}
	if memoryGrew(eng) != cs.grows {
		t.Fatalf("the engine: the least memory limit of %d elements is %d, of %d elements %d; want growth %v", memoryShort, eng[0], memoryLong, eng[1], cs.grows)
	}
	return eng
}

// TestStreamMemoryWindow checks that the memory count of a TypeScript stream bounds its window, not its
// length (spec/runtime/streaming.md, Memory): fed in chunks, the least memory limit under which a stream
// is done does not grow with the number of its elements, except for the recovered errors that a stream
// without a diagnostic limit retains, which grow it with each error. Memory counts depend on the backend
// (spec/runtime/parsing.md, Memory Limit): the engine counts its buffers and pools, so no limit decides
// the same for it and for TypeScript. The engine decides which streams grow, and TypeScript must agree.
func TestStreamMemoryWindow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	for i, c := range streamMemoryCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if bad := streamMemoryProblems(t, i, engineMemoryLeast(t, i), nil); len(bad) > 0 {
				t.Error(strings.Join(bad, "\n"))
			}
		})
	}
}

// TestStreamMemoryWindowMutants is the negative control of TestStreamMemoryWindow: a stream that keeps
// counting what it released grows its count with its length.
func TestStreamMemoryWindowMutants(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	for _, m := range []struct{ name, old, new string }{
		// A prune returns the values of the elements that no remaining holder references.
		{"values", "c.mem -= this.retired[n]!.bytes;", ""},
		// A prune returns the recovered errors and lists that no remaining holder references.
		{"memo held", "c.mem += bytes - this.memoHeld;", ""},
		// The visits of a walk are held only while the walk is in progress.
		{"skip visits", "    w.visits.push(", "    this.alloc(objectBytes(9));\n    w.visits.push("},
		// A memo prune returns the entries and slots it drops.
		{"memo prune", "if (c !== null && drop > 0) c.mem -=", "if (c === null && drop > 0) c.mem -="},
		// The memo's slots cover the window, not the stream from its start.
		{"memo slots", "return this.n + 1 + this.gapLen;", "return this.n + 1;"},
		// A store that replaces an entry holds one entry for its key.
		{"memo replace", "if (this.memoEntries === entries && this.control !== null) this.control.mem -= entryBytes;", ""},
		// An error that the diagnostic tail drops is no longer held by it.
		{"tail", " - (dropped === null ? 0 : syntaxErrorBytes(dropped))", ""},
		// A commit returns the skip marks and records before it.
		{"skip records", "if (c !== null) c.mem -= freed;", ""},
	} {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			// The library of a parser leaves out what its grammar does not use, so a mutant changes only
			// the parsers that keep its text.
			changed := false
			mutate := func(src string) string {
				n := strings.Count(src, m.old)
				if n > 1 {
					t.Fatalf("the parser has %d occurrences of %q, want at most 1", n, m.old)
				}
				changed = changed || n == 1
				return strings.Replace(src, m.old, m.new, 1)
			}
			found := 0
			for i, c := range streamMemoryCases {
				if c.grows {
					continue
				}
				found += len(streamMemoryProblems(t, i, [2]uint64{}, mutate))
			}
			if !changed {
				t.Fatalf("no parser has %q", m.old)
			}
			if found == 0 {
				t.Error("the mutant keeps the window of every stream")
			}
		})
	}
}
