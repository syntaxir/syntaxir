package print

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// runStreamTextLib runs the checks of the text of a stream on the TypeScript library, with its source
// changed by mutate if not nil, and returns the output of node and whether it failed.
func runStreamTextLib(t *testing.T, node string, mutate func(string) string) (string, bool) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(libDir, tsLibFileName))
	if err != nil {
		t.Fatal(err)
	}
	helpers, err := os.ReadFile(filepath.Join(libDir, "budget.ts"))
	if err != nil {
		t.Fatal(err)
	}
	lib := string(helpers) + string(source)
	if mutate != nil {
		changed := mutate(lib)
		if changed == lib {
			t.Fatal("the mutant changes nothing")
		}
		lib = changed
	}
	path := filepath.Join(t.TempDir(), "stream_text.ts")
	if err := os.WriteFile(path, []byte(lib+streamTextLibTest), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, path).CombinedOutput()
	return string(out), err != nil
}

// TestStreamTextInsideCharacter checks the text of a range of a TypeScript stream in bytes whose held
// part begins inside a character, after the stream discarded the input before the one unit it keeps
// (spec/runtime/streaming.md): it holds the bytes that remain, as the Go runtime's text does, each
// byte that does not begin a valid sequence being the lone surrogate U+DC00 plus the byte, as for
// invalid input; the same bytes parsed whole give the same text, and len counts them.
func TestStreamTextInsideCharacter(t *testing.T) {
	node := tooltest.RequireNode(t)
	if out, failed := runStreamTextLib(t, node, nil); failed {
		t.Fatalf("node:\n%s", out)
	}
	// A stream that decodes the bytes it holds as characters loses the bytes before the first whole
	// character.
	out, failed := runStreamTextLib(t, node, func(s string) string {
		return strings.Replace(s, "documentText(this.bs.subarray(start - this.base, end - this.base), Bytes)", "documentText(this.bs.subarray(start - this.base, end - this.base), CodePoints)", 1)
	})
	if !failed {
		t.Fatalf("the mutant passes:\n%s", out)
	}
}

const streamTextLibTest = `

const enc = (s: string): Uint8Array => new TextEncoder().encode(s);
// "ab€é": € at 2-4, é at 5-6; a discard keeps at least half of the buffer.
const input = enc("ab\u20ac\u00e9");
const failures: string[] = [];
for (const keep of [4, 6]) {
  const p = new StreamParser(mkGrammar(100, true, false), Bytes, 1, 100, null, 0);
  p.append(input);
  p.streamDiscard(keep);
  if (p.base !== keep) {
    failures.push("discarding before " + keep + " leaves the base at " + p.base);
    continue;
  }
  const got = p.text(0, input.length);
  // The bytes the stream holds, parsed whole by a parser of their own.
  const q = new Parser(mkGrammar(100, true, false), input.subarray(keep), Bytes, 1, 100);
  const want = q.text(0, input.length - keep);
  let bytes = "";
  for (const ch of got) {
    const c = ch.codePointAt(0)!;
    bytes += c >= 0xdc80 && c <= 0xdcff ? "\\\\x" + (c - 0xdc00).toString(16) : ch;
  }
  if (got !== want) failures.push("from " + keep + ": the stream's text is " + JSON.stringify(bytes) + ", the whole parse's " + JSON.stringify(want));
  if (utf8Len(got) !== input.length - keep) failures.push("from " + keep + ": len of the text is " + utf8Len(got) + ", want " + (input.length - keep));
}
if (failures.length > 0) {
  console.log(failures.join("\\n"));
  process.exit(1);
}
`
