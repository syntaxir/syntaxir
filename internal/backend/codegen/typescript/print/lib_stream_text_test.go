package print

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// runStreamTextLib runs the checks script of the text of a stream on the TypeScript library, with its
// source changed by mutate if not nil, and returns the output of node and whether it failed.
func runStreamTextLib(t *testing.T, node, script string, mutate func(string) string) (string, bool) {
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
	if err := os.WriteFile(path, []byte(lib+script), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := tooltest.NodeCombinedOutput(t, node, filepath.Dir(path), filepath.Base(path))
	return string(out), err != nil
}

// TestStreamTextAfterCommitInBytes checks the text of a TypeScript stream in bytes after a commit
// whose discard left the base inside a character (spec/runtime/streaming.md): a range after the
// commit point holds its bytes, each byte that does not begin a valid sequence being the lone
// surrogate U+DC00 plus the byte, as for invalid input, and the same bytes parsed whole give the same
// text, which len counts; a range that ends at or before the commit point has the empty text. The
// commit point is a character boundary, as every element ends at one: #stream stands only at the top
// level of a rule body, so a text never reads a range across it.
func TestStreamTextAfterCommitInBytes(t *testing.T) {
	node := tooltest.RequireNode(t)
	if out, failed := runStreamTextLib(t, node, streamTextLibTest, nil); failed {
		t.Fatalf("node:\n%s", out)
	}
	// A stream that decodes the bytes it holds as characters loses the bytes before the first whole
	// character.
	out, failed := runStreamTextLib(t, node, streamTextLibTest, func(s string) string {
		return strings.Replace(s, "documentText(this.bs.subarray(start - this.base, end - this.base), Bytes)", "documentText(this.bs.subarray(start - this.base, end - this.base), CodePoints)", 1)
	})
	if !failed {
		t.Fatalf("the mutant passes:\n%s", out)
	}
}

// TestStreamRangeTextBeforeCommitFails checks the text of a node in a TypeScript stream around the
// last commit point: a range that begins at or after it is the input of the range, and one that begins
// before it, which the checker keeps every grammar from taking, ends the parse with an internal error,
// whether or not the buffer still holds its input. Cutting the range at the commit point instead fails
// the check.
func TestStreamRangeTextBeforeCommitFails(t *testing.T) {
	node := tooltest.RequireNode(t)
	if out, failed := runStreamTextLib(t, node, streamRangeTextLibTest, nil); failed {
		t.Fatalf("node:\n%s", out)
	}
	out, failed := runStreamTextLib(t, node, streamRangeTextLibTest, func(s string) string {
		return strings.Replace(s, "if (start < this.textFloor) {\n      throw new Fatal(", "if (false) {\n      throw new Fatal(", 1)
	})
	if !failed || !strings.Contains(out, "begins before the commit point") {
		t.Fatalf("the mutant passes:\n%s", out)
	}
}

const streamRangeTextLibTest = `

const failures: string[] = [];
const text = "abcdefgh";
for (const unit of [CodePoints, Bytes]) {
  for (const discard of [false, true]) {
    const p = new StreamParser(mkGrammar(100, true, false), unit, 1, 100, null, 0);
    p.append(new TextEncoder().encode(text));
    if (discard) p.streamDiscard(3);
    p.textFloor = 4;
    const at = unit + ", discard " + discard;
    for (const [start, end] of [[4, 4], [4, 6], [5, 8]]) {
      const got = p.rangeText(start, end);
      if (got !== text.slice(start, end)) failures.push(at + ": the text of [" + start + ", " + end + ") is " + JSON.stringify(got));
    }
    for (const [start, end] of [[0, 2], [3, 4], [3, 3], [2, 6]]) {
      const want = "internal error: the text of [" + start + ", " + end + ") begins before the commit point 4";
      try {
        const got = p.rangeText(start, end);
        failures.push(at + ": the text of [" + start + ", " + end + ") is " + JSON.stringify(got) + ", want " + want);
      } catch (e) {
        if (!(e instanceof Fatal) || e.err.message !== want) failures.push(at + ": the text of [" + start + ", " + end + ") throws " + String(e instanceof Fatal ? e.err.message : e) + ", want " + want);
      }
    }
  }
}
if (failures.length > 0) {
  console.log(failures.join("\\n"));
  process.exit(1);
}
`

const streamTextLibTest = `

const enc = (s: string): Uint8Array => new TextEncoder().encode(s);
// "ab€", the invalid byte 0xff, "é": € at 2-4, 0xff at 5, é at 6-7. Each commit at pos discards the
// input before pos - 1 once at least half of the buffer can go and cuts text at pos: a commit at 2
// discards nothing, one at 5 leaves the base at 4, inside €.
const input = new Uint8Array([...enc("ab\u20ac"), 0xff, ...enc("\u00e9")]);
const failures: string[] = [];
const shown = (s: string): string => {
  let bytes = "";
  for (const ch of s) {
    const c = ch.codePointAt(0)!;
    bytes += c >= 0xdc80 && c <= 0xdcff ? "\\x" + (c - 0xdc00).toString(16) : ch;
  }
  return JSON.stringify(bytes);
};
for (const [keep, base] of [[2, 0], [5, 4]]) {
  const p = new StreamParser(mkGrammar(100, true, false), Bytes, 1, 100, null, 0);
  p.append(input);
  p.streamDiscard(keep - 1);
  p.textFloor = keep;
  if (p.base !== base) failures.push("from " + keep + ": the base is " + p.base + ", want " + base);
  const got = p.text(keep, input.length);
  // The bytes after the commit point, parsed whole by a parser of their own.
  const q = new Parser(mkGrammar(100, true, false), input.subarray(keep), Bytes, 1, 100);
  const want = q.text(0, input.length - keep);
  if (got !== want) failures.push("from " + keep + ": the stream's text is " + shown(got) + ", the whole parse's " + shown(want));
  if (utf8Len(got) !== input.length - keep) failures.push("from " + keep + ": len of the text is " + utf8Len(got) + ", want " + (input.length - keep));
  for (const [start, end] of [[0, keep], [0, keep - 1], [keep - 1, keep]]) {
    const cut = p.text(start, end);
    if (cut !== "") failures.push("from " + keep + ": the text of [" + start + ", " + end + ") is " + shown(cut) + ", want empty");
  }
}
if (failures.length > 0) {
  console.log(failures.join("\\n"));
  process.exit(1);
}
`
