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

// streamTextFloorGrammar takes, after its #stream repetition, the text of a string node made before
// it, which a stream holds.
const streamTextFloorGrammar = `@start
rule main = h:("h" "h") g:"g" item* #stream { text($g) == "g" } $$
rule item = "a" ";"
`

// tsStreamTextFloor feeds INPUT to a stream in chunks of SIZE bytes and prints how the stream ends.
const tsStreamTextFloor = `
import { createStream } from "./parser.ts";

const input = new TextEncoder().encode(INPUT);
const size = SIZE;
const { stream, error } = createStream({});
if (stream === null) throw error;
for (let i = 0; ; i += size) {
  const end = Math.min(i + size, input.length);
  const r = stream.feed(input.subarray(i, end), end === input.length);
  if (r.state === "waiting") continue;
  console.log(r.state === "failed" ? "failed: " + (r.error as Error).message : r.state);
  break;
}
`

// TestStreamTextBeforeCommitFails checks that a generated TypeScript stream whose predicate after the
// #stream repetition takes the text of a node that begins before the last commit point, which the
// checker rejects in every grammar, fails with the internal error, however the input is fed. The
// bundle is that of a grammar that takes the text of a string node, changed to read the capture of a
// tuple node made before the repetition.
func TestStreamTextBeforeCommitFails(t *testing.T) {
	t.Parallel()
	g, err := parseTest(streamTextFloorGrammar)
	if err != nil {
		t.Fatal(err)
	}
	// main captures h and g, in slots 0 and 1; only the predicate reads a capture.
	gen := func(g *ast.Grammar, o common.GenOptions) ([]byte, error) {
		b, err := codegentest.GenerateTSStreamBundle(g, o)
		src := string(b)
		if n := strings.Count(src, "c.cap(1))"); n == 0 || strings.Contains(src, "c.cap(0))") {
			t.Errorf("the predicate reads %d captures of slot 1", n)
		}
		return []byte(strings.ReplaceAll(src, "c.cap(1))", "c.cap(0))")), err
	}
	for _, c := range []struct{ in, want string }{
		{"hhga;a;a;", "failed: internal error: the text of [0, 2) begins before the commit point 9"},
		{"hhga;", "failed: internal error: the text of [0, 2) begins before the commit point 5"},
	} {
		quoted, _ := json.Marshal(c.in)
		for _, size := range []int{1, 64} {
			script := strings.NewReplacer("INPUT", string(quoted), "SIZE", strconv.Itoa(size)).Replace(tsStreamTextFloor)
			if got := strings.TrimSpace(codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main"}, gen)); got != c.want {
				t.Errorf("%q in chunks of %d: got %s, want %s", c.in, size, got, c.want)
			}
		}
	}
}
