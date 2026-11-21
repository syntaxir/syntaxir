package print_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// textScript is a Document construction followed by edits, as the engine ran it. The texts are bytes
// so that JSON keeps invalid UTF-8 as it is.
type textScript struct {
	Bytes   bool             `json:"bytes"`
	Initial []byte           `json:"initial"`
	Edits   []textScriptEdit `json:"edits"`
}

// textScriptEdit replaces [Start,End) with Text, passed to the TypeScript Document as a string when
// IsStr is set (Text is then valid UTF-8) and as bytes otherwise.
type textScriptEdit struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  []byte `json:"text"`
	IsStr bool   `json:"isStr"`
}

// textStep is the outcome of construction (the first step) or of one edit: whether it was accepted,
// the text after it as bytes, and whether a parse of that text succeeded. Raw is what bytes() of the
// TypeScript Document returns, which must equal the text.
type textStep struct {
	Accepted bool   `json:"accepted"`
	Text     []byte `json:"text"`
	Raw      []byte `json:"raw"`
	Parsed   bool   `json:"parsed"`
}

// textPieces make up initial texts and replacements: valid characters of each width, a literal
// U+FFFD, continuation bytes, incomplete prefixes, the rest of a prefix, an invalid lead byte, an
// overlong encoding and an encoded surrogate.
var textPieces = []string{"", "a", "b", " ", "\n", "é", "日", "😀", "€", "\uFFFD", "\x80", "\xa9", "\xc3", "\xe6\xbc", "\xe2\x82", "\xac", "\xf0\x9f\x98", "\xff", "\xc0\xaf", "\xed\xa0\x80"}

func randomTextPieces(rng *rand.Rand, n int) string {
	var b strings.Builder
	for range rng.Intn(n + 1) {
		b.WriteString(textPieces[rng.Intn(len(textPieces))])
	}
	return b.String()
}

// engineTextScript runs a random Document session on the engine in unit and returns it with the
// outcome of each step. Edit ranges are drawn from the engine's current text, including ranges outside
// it and, in Bytes, ends inside a character, so that the session also has rejected edits.
func engineTextScript(t *testing.T, prog *engine.Program, rng *rand.Rand, unit input.PositionUnit, edits int) (textScript, []textStep) {
	t.Helper()
	s := textScript{Bytes: unit == input.Bytes, Initial: []byte(randomTextPieces(rng, 6))}
	d, err := prog.NewDocumentWith("main", string(s.Initial), engine.ParseOptions{Unit: unit})
	if err != nil {
		t.Fatal(err)
	}
	step := func(accepted bool) textStep {
		_, err := d.Parse()
		return textStep{Accepted: accepted, Text: []byte(d.Text()), Parsed: err == nil}
	}
	steps := []textStep{step(true)}
	for range edits {
		length := len(d.Text())
		if unit == input.CodePoints {
			length = utf8.RuneCountInString(d.Text())
		}
		start := rng.Intn(length + 2)
		end := start + rng.Intn(4) - 1
		e := textScriptEdit{Start: start, End: end, Text: []byte(randomTextPieces(rng, 3))}
		e.IsStr = utf8.Valid(e.Text) && rng.Intn(2) == 0
		s.Edits = append(s.Edits, e)
		steps = append(steps, step(d.Edit(e.Start, e.End, string(e.Text)) == nil))
	}
	return s, steps
}

// TestTSDocumentTextAgreesWithEngine replays random Document sessions of the engine on a generated
// TypeScript Document and compares, after construction and after each edit, whether the edit was
// accepted, the text that text() and bytes() return and whether a parse succeeds. In CodePoints both normalize
// invalid UTF-8 in the initial text and in replacements, one U+FFFD per invalid byte; in Bytes both
// keep the exact bytes. As a negative control, a library that keeps invalid bytes in CodePoints must
// diverge.
func TestTSDocumentTextAgreesWithEngine(t *testing.T) {
	const src = "rule main = (word / \" \" / \"\\n\")* $$\nrule word = [a-zé日😀€]+"
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	prog := mustCompile(t, src)
	const seeds, edits = 8, 400
	var scripts []textScript
	var want [][]textStep
	for seed := range seeds {
		rng := rand.New(rand.NewSource(int64(seed)))
		for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
			s, steps := engineTextScript(t, prog, rng, unit, edits)
			scripts = append(scripts, s)
			want = append(want, steps)
		}
	}
	data, err := json.Marshal(scripts)
	if err != nil {
		t.Fatal(err)
	}
	script := `import { createDocument, Bytes, CodePoints } from "./parser.ts";
const scripts = ` + string(data) + `;
const bytes = (b64) => new Uint8Array(Buffer.from(b64 ?? "", "base64"));
const b64 = (u) => Buffer.from(u).toString("base64");
// textBytes returns the bytes of a text of the Document: its UTF-8 encoding, with the surrogates
// U+DC80-U+DCFF as the bytes they stand for.
const textBytes = (s) => {
  const out = [];
  for (const ch of s) {
    const c = ch.codePointAt(0);
    if (c >= 0xdc80 && c <= 0xdcff) out.push(c - 0xdc00);
    else out.push(...Buffer.from(ch, "utf8"));
  }
  return new Uint8Array(out);
};
const out = [];
for (const s of scripts) {
  const made = createDocument(bytes(s.initial), { unit: s.bytes ? Bytes : CodePoints });
  if (made.error !== null) throw made.error;
  const d = made.document;
  const record = (accepted) => {
    const r = d.parse();
    return { accepted, text: b64(textBytes(d.text())), raw: b64(d.bytes()), parsed: r.error === null };
  };
  const steps = [record(true)];
  for (const e of s.edits ?? []) {
    const repl = e.isStr ? Buffer.from(e.text ?? "", "base64").toString("utf8") : bytes(e.text);
    steps.push(record(d.edit(e.start, e.end, repl) === null));
  }
  out.push(steps);
}
console.log(JSON.stringify(out));
`
	opts := common.GenOptions{Start: "main", Document: true}
	// run runs the scripts on the library that mutate changes and returns the divergences from the
	// engine, and how many of them are in acceptance, text() or parse success rather than in bytes()
	// alone.
	run := func(mutate func(string) string) (diffs []string, observable int) {
		out := codegentest.RunTSGrammarScriptBundle(t, g, script, opts, func(g *ast.Grammar, opts common.GenOptions) ([]byte, error) {
			code, err := codegentest.GenerateTSBundle(g, opts)
			if err != nil || mutate == nil {
				return code, err
			}
			return []byte(mutate(string(code))), nil
		})
		var got [][]textStep
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("node: malformed output: %v\n%s", err, codegentest.Clip(out, 0))
		}
		if len(got) != len(want) {
			t.Fatalf("the TypeScript Document ran %d scripts, want %d", len(got), len(want))
		}
		for i, s := range scripts {
			for j, w := range want[i] {
				g := got[i][j]
				differs := g.Accepted != w.Accepted || string(g.Text) != string(w.Text) || g.Parsed != w.Parsed
				if differs {
					observable++
				}
				if differs || string(g.Raw) != string(w.Text) {
					diffs = append(diffs, fmt.Sprintf("script %d (bytes=%v) step %d: TypeScript accepted=%v text %q bytes %q parsed=%v; engine accepted=%v text %q parsed=%v",
						i, s.Bytes, j, g.Accepted, g.Text, g.Raw, g.Parsed, w.Accepted, w.Text, w.Parsed))
				}
			}
		}
		return diffs, observable
	}

	var compared, rejected, normalized int
	for i, s := range scripts {
		for j, w := range want[i] {
			compared++
			if !w.Accepted {
				rejected++
			}
			if j > 0 && w.Accepted && !s.Bytes && !utf8.Valid(s.Edits[j-1].Text) {
				normalized++
			}
		}
	}
	if rejected == 0 || normalized == 0 {
		t.Fatalf("the sessions have %d rejected edits and %d normalized replacements; want some of each", rejected, normalized)
	}
	diffs, _ := run(nil)
	t.Logf("%d steps compared, %d rejected edits, %d accepted CodePoints edits with invalid UTF-8; %d divergences", compared, rejected, normalized, len(diffs))
	for i, d := range diffs {
		if i == 10 {
			t.Errorf("%d more divergences", len(diffs)-i)
			break
		}
		t.Error(d)
	}

	// The negative control: a library that keeps the invalid bytes of CodePoints texts.
	const old, keep = "if (invalid === 0) return b;", "return b;"
	mutated, observable := run(func(src string) string {
		if strings.Count(src, old) != 1 {
			t.Fatalf("the library has %d copies of %q, want 1", strings.Count(src, old), old)
		}
		return strings.Replace(src, old, keep, 1)
	})
	t.Logf("a library that keeps invalid bytes in CodePoints: %d divergences, %d of them in acceptance, text() or parse success", len(mutated), observable)
	if observable == 0 {
		t.Errorf("a library that keeps invalid bytes in CodePoints diverges from the engine only in bytes() (%d steps)", len(mutated))
	}
}
