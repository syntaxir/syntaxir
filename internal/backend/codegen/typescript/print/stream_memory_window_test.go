package print_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// tsStreamMemoryWindow feeds LINES and then 4*LINES copies of LINE, one for each chunk, to streams
// under a memory limit and the diagnostic limit LIMIT, and prints the largest memory count after a
// commit of each, then the first commit after which the floor of the memo breaks the window, if any:
// the floor is a commit point fewer than 1,024 units before the commit point, and it moves only to a
// commit point at least 1,024 units past it.
const tsStreamMemoryWindow = `
import { createStream } from "./parser.ts";

const line = new TextEncoder().encode(LINE);
let problem = "";
const most = (n: number): number => {
  const { stream, error } = createStream({ limits: { memory: 2 ** 40 }, diagnosticLimit: LIMIT });
  if (stream === null) throw error;
  const p = (stream as any).p;
  const c = p.control;
  const commits = new Set<number>([0]);
  let m = 0, floor = 0;
  const commit = p.streamCommit.bind(p);
  p.streamCommit = (pos: number) => {
    commit(pos);
    m = Math.max(m, c.memoryUsed());
    const f = p.memoFloor, at = p.textFloor;
    commits.add(at);
    if (problem === "" && (!commits.has(f) || f > at || at - f >= 1024 || f < floor || (f > floor && f - floor < 1024))) {
      problem = "after the commit at " + at + " the floor moved from " + floor + " to " + f;
    }
    floor = f;
  };
  for (let i = 0; i < n; i++) {
    const r = stream.feed(line, i === n - 1);
    if (r.state === "waiting") continue;
    if (r.state === "failed" && LIMIT === 0) throw r.error;
    break;
  }
  return m;
};
const short = most(LINES), long = most(4 * LINES);
console.log(short + "\t" + long + "\t" + problem);
`

// TestStreamMemoryWindowStreams checks the window of the memory theorem of the streaming proof on
// generated TypeScript streams, as TestStreamMemoryWindow does on the engine's: after each commit the
// floor of the memo is a commit point fewer than 1,024 units back that moved there from at least
// 1,024 units back, and the largest memory count after a commit rises by at most StreamCountSlack for
// a stream four times as long.
func TestStreamMemoryWindowStreams(t *testing.T) {
	t.Parallel()
	for i, c := range codegentest.StreamWindowCases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			if bad := streamWindowTS(t, i, nil); bad != "" {
				t.Error(bad)
			}
		})
	}
}

// streamWindowTS runs tsStreamMemoryWindow on the case c of StreamWindowCases, with its bundle changed
// by mutate if not nil, and returns how it breaks the window or the count, or "".
func streamWindowTS(t *testing.T, c int, mutate func(string) string) string {
	t.Helper()
	cs := codegentest.StreamWindowCases[c]
	g, err := parseTest(cs.Src)
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
	quoted, _ := json.Marshal(cs.Line)
	script := strings.NewReplacer("LINES", strconv.Itoa(cs.Lines), "LINE", string(quoted), "LIMIT", strconv.Itoa(cs.Limit)).Replace(tsStreamMemoryWindow)
	out := strings.TrimRight(codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main"}, gen), "\r\n")
	f := strings.SplitN(out, "\t", 3)
	if len(f) != 3 {
		t.Fatalf("output %q", out)
	}
	short, err1 := strconv.ParseUint(f[0], 10, 64)
	long, err2 := strconv.ParseUint(f[1], 10, 64)
	switch {
	case err1 != nil || err2 != nil || short == 0:
		t.Fatalf("output %q", out)
	case f[2] != "":
		return f[2]
	case long < short || long > short+codegentest.StreamCountSlack:
		return fmt.Sprintf("the largest count is %d bytes for the shorter stream and %d for the longer", short, long)
	}
	return ""
}
