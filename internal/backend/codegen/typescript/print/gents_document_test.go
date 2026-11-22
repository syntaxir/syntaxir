package print_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// documentEdit is an edit of a Document in the script of TestTSDocumentMatchesFreshParse: [Start,End)
// in the position unit, replaced with the bytes B64, or with the string Str when IsStr is set.
type documentEdit struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	B64   string `json:"b64"`
	Str   string `json:"str"`
	IsStr bool   `json:"isStr"`
	// Bad marks an edit that the Document rejects, leaving the text as it was.
	Bad bool `json:"bad"`
}

// documentCase is the sequence of edits of one Document, from the text B64, in the unit Bytes.
type documentCase struct {
	B64   string         `json:"b64"`
	Bytes bool           `json:"bytes"`
	Edits []documentEdit `json:"edits"`
}

// documentFragments are the replacements of the random edits: text of the grammar, and bytes that
// join with their neighbors into characters or stay invalid UTF-8.
var documentFragments = []string{
	"a = 1+2*3\n", "@x\n", "a,b,c\n", "\n", "x", "1", "(", ")", "+", " = ", ",", "@", "-", "é", "😀",
	"\xc3", "\xa9", "\xf0\x9f", "\x98\x80", "\xff", "zz = (4-5)*6\n",
}

// documentLines are the lines of the grammar that the random edits insert at line starts.
var documentLines = []string{"a = 1+2*3\n", "@x\n", "a,b,c\n", "\n", "zz = (4-5)*6\n", "q = -(7)\n"}

// documentByteStart returns the byte offset in text of the position start in the unit.
func documentByteStart(text string, start int, bytes bool) int {
	if bytes {
		return start
	}
	return documentByteStarts(text)[start]
}

// documentStarts returns the positions in the unit at which a character of text starts, and its end.
func documentStarts(text string, bytes bool) []int {
	var out []int
	n := 0
	for off := range text {
		if bytes {
			out = append(out, off)
		} else {
			out = append(out, n)
		}
		n++
	}
	if bytes {
		return append(out, len(text))
	}
	return append(out, n)
}

// documentApply applies an edit of [start,end) in the unit to text.
func documentApply(text string, start, end int, repl string, bytes bool) string {
	if !bytes {
		offs := append([]int{}, documentByteStarts(text)...)
		start, end = offs[start], offs[end]
	}
	return text[:start] + repl + text[end:]
}

// documentByteStarts returns the byte offsets of the characters of text, and its length.
func documentByteStarts(text string) []int {
	var out []int
	for off := range text {
		out = append(out, off)
	}
	return append(out, len(text))
}

// normalizeCodePoints returns s with each byte that is not part of a valid UTF-8 encoding replaced by
// U+FFFD, the text that a CodePoints Document holds for s.
func normalizeCodePoints(s string) string {
	var b strings.Builder
	for _, r := range s {
		b.WriteRune(r)
	}
	return b.String()
}

// documentInside reports whether the byte offset k is inside a valid character of text.
func documentInside(text string, k int) bool {
	for start := k - 1; start >= max(0, k-utf8.UTFMax+1); start-- {
		if utf8.RuneStart(text[start]) {
			r, size := utf8.DecodeRuneInString(text[start:])
			return !(r == utf8.RuneError && size == 1) && start+size > k
		}
	}
	return false
}

// TestTSDocumentMatchesFreshParse edits generated TypeScript Documents at random, in both units and
// with replacements that are invalid UTF-8: in Bytes they join with their neighbors into characters,
// and in CodePoints the Document normalizes them, one U+FFFD per invalid byte. It requires each parse to return
// what the engine returns for a parse of the whole text, each typed parse what parseValue returns, and
// the text to be the edited one. Rejected edits change nothing. The Documents must reuse results:
// over the script they evaluate fewer rules than a new Document of each text does, and a parse with
// no edit since the last one evaluates none. Only main is a start rule, so the other rules are
// entered only through the calls of the printed code. As a negative control, libraries with a broken
// edit or a broken retention of results must fail the comparison or the reuse.
func TestTSDocumentMatchesFreshParse(t *testing.T) {
	src := strings.Replace(codegentest.IncrementalGrammar, "rule main", "@start\nrule main", 1)
	g, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	ref := referenceProgram(t, src, check.Options{NoProjections: true})
	rng := rand.New(rand.NewSource(64))
	var cases []documentCase
	var wants []string
	for _, bytes := range []bool{false, true} {
		for c := 0; c < 6; c++ {
			text := "a = 1+2\n@pos\nx,y\n\nb = (3)*-4\n"
			dc := documentCase{B64: base64.StdEncoding.EncodeToString([]byte(text)), Bytes: bytes}
			// undo is the edit that undoes the last applied one, when the texts and the replacement
			// are valid UTF-8, and failed tells whether the text does not match.
			var undo *documentEdit
			failed := false
			for e := 0; e < 40; e++ {
				starts := documentStarts(text, bytes)
				var ed documentEdit
				var repl string
				switch {
				case failed && undo != nil && rng.Intn(4) != 0:
					// The text is repaired, so that the parses that follow reach the end of the text.
					ed, repl = *undo, undo.Str
				case utf8.ValidString(text) && rng.Intn(3) == 0:
					// A line inserted at a line start, or replacing the line there.
					var lines []int
					for k, off := range documentByteStarts(text) {
						if k == 0 || text[off-1] == '\n' {
							lines = append(lines, k)
						}
					}
					k := rng.Intn(len(lines))
					i, j := lines[k], lines[k]
					if k+1 < len(lines) && rng.Intn(2) == 0 {
						j = lines[k+1]
					}
					repl = documentLines[rng.Intn(len(documentLines))]
					ed = documentEdit{Start: starts[i], End: starts[j], Str: repl, IsStr: true}
				default:
					i := rng.Intn(len(starts))
					j := i + rng.Intn(min(3, len(starts)-i))
					repl = documentFragments[rng.Intn(len(documentFragments))]
					ed = documentEdit{Start: starts[i], End: starts[j]}
					if utf8.ValidString(repl) && rng.Intn(2) == 0 {
						ed.Str, ed.IsStr = repl, true
					}
					switch rng.Intn(8) {
					case 0:
						ed.Start, ed.End, ed.Bad = starts[j], starts[i]-1, true
					case 1:
						ed.End, ed.Bad = starts[len(starts)-1]+1, true
					case 2:
						if bytes {
							// A position inside a character, if the text has one.
							for k := 1; k < len(text); k++ {
								if documentInside(text, k) {
									ed.Start, ed.End, ed.Bad = k, k, true
									break
								}
							}
						}
					}
				}
				ed.B64 = base64.StdEncoding.EncodeToString([]byte(repl))
				if !bytes {
					// In CodePoints the Document holds the replacement normalized.
					repl = normalizeCodePoints(repl)
				}
				if !ed.Bad {
					old := text
					text = documentApply(text, ed.Start, ed.End, repl, bytes)
					undo = nil
					if utf8.ValidString(old) && utf8.ValidString(text) && utf8.ValidString(repl) {
						removed := old[len(text[:documentByteStart(text, ed.Start, bytes)]):]
						removed = removed[:len(old)-len(text)+len(repl)]
						n := len(repl)
						if !bytes {
							n = utf8.RuneCountInString(repl)
						}
						undo = &documentEdit{Start: ed.Start, End: ed.Start + n, Str: removed, IsStr: true}
					}
				}
				dc.Edits = append(dc.Edits, ed)
				n, err := ref(text, bytes, false, nil)
				failed = err != nil
				wants = append(wants, fmt.Sprintf("%s %s", base64.StdEncoding.EncodeToString([]byte(text)), codegentest.DeepResultJSON(t, n, err)))
			}
			cases = append(cases, dc)
		}
	}
	data, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	script := `import { createDocument, parse, parseValue, marshal, Bytes, CodePoints } from "./parser.ts";
const cases = ` + string(data) + `;
const b64 = (u) => Buffer.from(u).toString("base64");
// evaluated and reused sum the statistics of the Documents' parses, full the rule evaluations of a
// first parse of a new Document of each text, and again those of parses with no edit before them.
let evaluated = 0, reused = 0, full = 0, again = 0;
for (const c of cases) {
  const unit = c.bytes ? Bytes : CodePoints;
  const made = createDocument(new Uint8Array(Buffer.from(c.b64, "base64")), { unit, expectations: true });
  if (made.error !== null) throw made.error;
  const d = made.document;
  for (const e of c.edits) {
    try {
      const before = b64(d.bytes());
      const err = d.edit(e.start, e.end, e.isStr ? e.str : new Uint8Array(Buffer.from(e.b64, "base64")));
      if ((err !== null) !== e.bad) throw new Error("edit " + JSON.stringify(e) + ": " + err);
      if (err !== null && b64(d.bytes()) !== before) throw new Error("a rejected edit changed the text");
      const r = d.parse();
      const s = d.stats();
      evaluated += s.evaluated;
      reused += s.reused;
      const text = d.bytes();
      const n = createDocument(text, { unit, expectations: true }).document;
      n.parse();
      full += n.stats().evaluated;
      const fresh = parse(text, unit);
      if (marshal(fresh.node) !== marshal(r.node) || String(fresh.error) !== String(r.error)) throw new Error("Document parse differs from parse");
      const typed = d.parseValue();
      const freshTyped = parseValue(text, unit);
      if (JSON.stringify(typed.value) !== JSON.stringify(freshTyped.value) || String(typed.error) !== String(freshTyped.error)) throw new Error("Document parseValue differs from parseValue");
      const out = r.error === null ? '{"node":' + marshal(r.node) + "}" : '{"err":' + JSON.stringify(r.error.message) + ',"node":' + marshal(r.node) + "}";
      console.log(b64(text) + " " + out);
    } catch (err) {
      console.log("throw " + JSON.stringify(String(err)));
    }
  }
  d.parse();
  again += d.stats().evaluated;
}
console.log("stats " + JSON.stringify({ evaluated, reused, full, again }));
`
	opts := common.GenOptions{Start: "main", Document: true, Types: true}
	// run runs the script on the library that mutate changes, and returns the first difference from
	// the engine.
	run := func(mutate func(string) string) error {
		out := codegentest.RunTSGrammarScriptBundle(t, g, script, opts, func(g *ast.Grammar, opts common.GenOptions) ([]byte, error) {
			code, err := codegentest.GenerateTSBundle(g, opts)
			if err != nil || mutate == nil {
				return code, err
			}
			return []byte(mutate(string(code))), nil
		})
		got := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
		last := got[len(got)-1]
		if !strings.HasPrefix(last, "stats ") {
			return fmt.Errorf("no statistics:\n%s", codegentest.Clip(out, 0))
		}
		got = got[:len(got)-1]
		var stats struct{ Evaluated, Reused, Full, Again int }
		if err := json.Unmarshal([]byte(strings.TrimPrefix(last, "stats ")), &stats); err != nil {
			return err
		}
		if mutate == nil {
			t.Logf("statistics: %+v", stats)
		}
		for i := range wants {
			if i >= len(got) {
				return fmt.Errorf("got %d results, want %d:\n%s", len(got), len(wants), codegentest.Clip(out, 0))
			}
			if got[i] != wants[i] {
				at := codegentest.DiffAt(got[i], wants[i])
				return fmt.Errorf("parse %d:\n got %s\nwant %s", i, codegentest.Clip(got[i], at), codegentest.Clip(wants[i], at))
			}
		}
		if len(got) != len(wants) {
			return fmt.Errorf("got %d results, want %d:\n%s", len(got), len(wants), codegentest.Clip(out, 0))
		}
		if stats.Reused == 0 || stats.Evaluated >= stats.Full || stats.Again != 0 {
			return fmt.Errorf("the Documents reuse too little: %+v", stats)
		}
		return nil
	}
	if err := run(nil); err != nil {
		t.Fatal(err)
	}
	for _, m := range documentMutants {
		mutate := func(src string) string {
			want := max(m.copies, 1)
			if strings.Count(src, m.old) != want {
				t.Fatalf("mutant %q: the library has %d copies of %q, want %d", m.name, strings.Count(src, m.old), m.old, want)
			}
			return strings.ReplaceAll(src, m.old, m.new)
		}
		if run(mutate) == nil {
			t.Errorf("mutant %q passes the comparison", m.name)
		}
	}
}

// documentMutants are the broken edits of the negative control of TestTSDocumentMatchesFreshParse:
// each replaces old in the library with new, in each of its copies (one unless copies says more: the
// printed memoAdvance and its form without charges, advanceEntry, are two).
var documentMutants = []struct {
	name, old, new string
	copies         int
}{
	// The replacement is dropped: the edit only deletes the range.
	{"dropped splice", "out.set(r, startByte);", "", 1},
	// A position in code points is taken as one byte per character after the start.
	{"mis-mapped unit offset", "endByte = documentByteOffset(b, end);", "endByte = startByte + (end - start);", 1},
	// No result is kept between parses.
	{"memo not kept", "d.slots = document.slots;", "", 1},
	// A result whose examined input reaches into an edit is kept as it is.
	{"edit ignores examined input", "if (examined <= log[i]!.start) {", "if (true) {", 2},
	// A result after an edit keeps its old end.
	{"result not moved", "e!.end += shift;", "", 2},
	// The entries at the edited positions are kept without the edit.
	{"edited entries kept", "if (this.advanceEntry(e, this.edits)) {", "if (true) {", 1},
	// The value of a moved result keeps its old positions.
	{"value not moved", "if (e.shift === 0) return true;", "return true;", 1},
}

// TestTSDocumentReuseRecordsExamined checks that a result reused inside a rule that is evaluated
// again adds what it examined to that rule's result: w looks past its end, p ends with w, and after
// p is evaluated again with w reused, an edit that w looked at must make p evaluated again. As a
// negative control, the library without that record must fail.
func TestTSDocumentReuseRecordsExamined(t *testing.T) {
	g, err := parser.Parse("@start\nrule main = p \";\" [a-z?]*\nrule p = \"s\" w\nrule w = [0-9]+ !\";x\"")
	if err != nil {
		t.Fatal(err)
	}
	script := `import { createDocument, marshal } from "./parser.ts";
const d = createDocument("s12;?").document;
d.parse();
for (const [s, e, r] of [[0, 1, "s"], [4, 5, "x"]]) {
  d.edit(s, e, r);
  const a = d.parse();
  const f = createDocument(d.text()).document.parse();
  console.log(d.text(), marshal(a.node) === marshal(f.node) && String(a.error) === String(f.error), JSON.stringify(d.stats()));
}
`
	want := "s12;? true {\"evaluated\":2,\"reused\":1,\"memoHits\":1,\"resumedElements\":0}\ns12;x true {\"evaluated\":3,\"reused\":0,\"memoHits\":0,\"resumedElements\":0}\n"
	run := func(mutate func(string) string) string {
		return codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main", Document: true}, func(g *ast.Grammar, opts common.GenOptions) ([]byte, error) {
			code, err := codegentest.GenerateTSBundle(g, opts)
			if err != nil || mutate == nil {
				return code, err
			}
			return []byte(mutate(string(code))), nil
		})
	}
	if got := run(nil); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	const old = "this.documentTouch(e!.examined);\n      if (e!.from < e!.pos) {"
	got := run(func(src string) string {
		if strings.Count(src, old) != 1 {
			t.Fatalf("the library has %d copies of %q, want 1", strings.Count(src, old), old)
		}
		return strings.Replace(src, old, "if (e!.from < e!.pos) {", 1)
	})
	if got == want {
		t.Error("the library that does not record what a reused result examined passes")
	}
}

// TestTSDocumentProfile checks the observation of a Document parse: a profile of each parse counts the
// calls that its trace reports, and its evaluations and memo hits are the evaluations and the reuses of
// the Document's stats, on the parse from scratch and on the parse after an edit, which reuses results.
// As a negative control, a library whose Document parse does not report its evaluations must fail.
func TestTSDocumentProfile(t *testing.T) {
	g, err := parseTest("rule main = item+ !.\nrule item = word \",\"\nrule word = [a-z]+\n")
	if err != nil {
		t.Fatal(err)
	}
	script := `import { createDocument, Profile } from "./parser.ts";
const { document: d, error } = createDocument("ab,cd,ef,gh,");
if (error !== null) throw error;
for (const edit of [null, [3, 5, "xy"]]) {
  if (edit !== null && d.edit(edit[0], edit[1], edit[2]) !== null) throw new Error("edit failed");
  const profile = new Profile();
  const events = [];
  const result = d.parse({ profile, trace: (e) => events.push(e) });
  const s = profile.snapshot(), stats = d.stats();
  const exits = events.filter((e) => e.kind === "exit");
  console.log(result.error === null, s.parses, s.calls === exits.length, s.entered === events.filter((e) => e.kind === "enter").length,
    s.memoHits === exits.filter((e) => e.memo).length, s.evals === stats.evaluated, s.memoHits === stats.memoHits, JSON.stringify(stats));
}
`
	want := "true 1 true true true true true {\"evaluated\":11,\"reused\":0,\"memoHits\":0,\"resumedElements\":0}\ntrue 1 true true true true true {\"evaluated\":3,\"reused\":4,\"memoHits\":4,\"resumedElements\":0}\n"
	run := func(mutate func(string) string) string {
		return codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main", Document: true, Observability: true}, func(g *ast.Grammar, opts common.GenOptions) ([]byte, error) {
			code, err := codegentest.GenerateTSBundle(g, opts)
			if err != nil || mutate == nil {
				return code, err
			}
			return []byte(mutate(string(code))), nil
		})
	}
	if got := run(nil); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	const old = "this.evaluated++;\n    this.observation?.evaluated();"
	got := run(func(src string) string {
		if strings.Count(src, old) != 2 {
			t.Fatalf("the library has %d copies of %q, want 2", strings.Count(src, old), old)
		}
		return strings.ReplaceAll(src, old, "this.evaluated++;")
	})
	if got == want {
		t.Error("the library whose Document parse does not report its evaluations passes")
	}
}

// TestTSDocumentOptions checks the options of a Document: invocation options apply after the
// defaults, an invocation cannot change the unit or the expectations (which the test parsers record
// by default), an invalid default fails
// construction, the input is copied, and a parse or an edit from a trace callback of a parse fails.
func TestTSDocumentOptions(t *testing.T) {
	g, err := parseTest(`rule main = [a-z]+ "!"`)
	if err != nil {
		t.Fatal(err)
	}
	script := `import { createDocument, Bytes, CodePoints } from "./parser.ts";
const input = new TextEncoder().encode("ab!");
const { document: d, error } = createDocument(input, { unit: Bytes, limits: { compute: 100000 } });
if (error !== null) throw error;
input[0] = 0x21;
console.log(d.text(), d.parse().error === null);
console.log(String(d.parse({ unit: CodePoints }).error));
console.log(String(d.parse({ expectations: false }).error));
console.log(String(d.parse({ limits: { input: 2 } }).error));
console.log(String(createDocument("abc!", { limits: { input: 2 } }).error));
console.log(String(createDocument("abc!", { bogus: 1 }).error));
let inner = null;
const traced = d.parse({ trace: () => { if (inner === null) inner = [String(d.parse().error), String(d.edit(0, 0, "x"))]; } });
console.log(traced.error === null, inner.join(" | "), d.text());
console.log(String(d.edit(0, 9, "x")), d.text());
`
	out := codegentest.RunTSGrammarScriptWith(t, g, script, common.GenOptions{Start: "main", Document: true, Observability: true})
	want := `ab! true
Error: document parse cannot change unit
Error: document parse cannot change expectations
ParseInterruptionError: input-limit exceeded: limit 2, length 3
ParseInterruptionError: input-limit exceeded: limit 2, length 4
Error: unknown parse option: bogus
true Error: document is already parsing | Error: document is already parsing ab!
Error: invalid range [0,9) for text of length 3 ab!
`
	if out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

// TestTSDocumentTouches checks that each terminal records the input it examines: a result that
// looked at the unit before a line start, at characters past a lookahead, or at the unit past a
// literal must not be reused after an edit there. The grammars are built so that the rule whose
// result would be wrongly kept sits where only that touch tells the edit matters, and the Document
// must return what a new Document of the edited text returns. As a negative control, a library
// without the touch must fail each case.
func TestTSDocumentTouches(t *testing.T) {
	type edit struct {
		text       string
		start, end int
		repl       string
	}
	cases := []struct {
		name    string
		grammar string
		edits   []edit
		// mutant removes the touch that the case needs from the library.
		old, new string
	}{
		{
			"line start",
			"@start\nrule main = (l \"\\n\"?)* $$\nrule l = ^ \"a\" / \"b\"",
			[]edit{{"a\na", 1, 2, ""}, {"a\na\na", 1, 2, ""}, {"a\nb\na", 3, 4, ""}},
			"this.documentTouchLine();", "",
		},
		{
			"lookahead",
			"@start\nrule main = (l / [a-z] / \"1\")*\nrule l = \"a\" &([a-z] [a-z])",
			[]edit{{"aaa", 2, 3, "1"}, {"aaab", 3, 4, "1"}},
			"this.documentTouch(this.pos + this.psize);", "",
		},
		{
			"literal lookahead",
			"@start\nrule main = (l / [a-z])*\nrule l = \"a\" &\"bc\"",
			[]edit{{"abc", 2, 3, "x"}},
			"      this.documentTouch(this.pos);\n      return v;", "      return v;",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, err := parser.Parse(c.grammar)
			if err != nil {
				t.Fatal(err)
			}
			var js []string
			for _, e := range c.edits {
				js = append(js, fmt.Sprintf("[%q, %d, %d, %q]", e.text, e.start, e.end, e.repl))
			}
			script := `import { createDocument, marshal } from "./parser.ts";
for (const [text, s, e, r] of [` + strings.Join(js, ", ") + `]) {
  const d = createDocument(text).document;
  d.parse();
  d.edit(s, e, r);
  const a = d.parse();
  const f = createDocument(d.text()).document.parse();
  console.log(JSON.stringify(d.text()), marshal(a.node) === marshal(f.node) && String(a.error) === String(f.error));
}
`
			run := func(mutate func(string) string) string {
				return codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main", Document: true}, func(g *ast.Grammar, opts common.GenOptions) ([]byte, error) {
					code, err := codegentest.GenerateTSBundle(g, opts)
					if err != nil || mutate == nil {
						return code, err
					}
					return []byte(mutate(string(code))), nil
				})
			}
			if got := run(nil); strings.Contains(got, "false") || strings.Count(got, "true") != len(c.edits) {
				t.Fatalf("the Document differs from a new Document:\n%s", got)
			}
			got := run(func(src string) string {
				if strings.Count(src, c.old) != 1 {
					t.Fatalf("the library has %d copies of %q, want 1", strings.Count(src, c.old), c.old)
				}
				return strings.Replace(src, c.old, c.new, 1)
			})
			if !strings.Contains(got, "false") {
				t.Errorf("the library without the touch passes:\n%s", got)
			}
		})
	}
}

// TestTSDocumentSkipRuns reparses the recovery grammars with skip-run sites after random one-character
// edits and compares each Document with a new one. Walks reuse skip-run records in these parses
// (hundreds to thousands of hits per grammar). No mutant is attached: removing the touch of a hit
// record's examined range changes no result on these grammars, so the touch is unproven by a mutant.
func TestTSDocumentSkipRuns(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	rng := rand.New(rand.NewSource(76))
	type grammarCase struct {
		src, script string
	}
	var cases []grammarCase
	for _, sg := range codegentest.SkipRunGrammars {
		var items [][3]any
		for _, in := range codegentest.SkipRunInputs(sg.Alphabet, 40, rng) {
			rs := []rune(in)
			if len(rs) == 0 {
				continue
			}
			for k := 0; k < 6; k++ {
				p := rng.Intn(len(rs))
				items = append(items, [3]any{in, p, string(sg.Alphabet[rng.Intn(len(sg.Alphabet))])})
			}
		}
		b, err := json.Marshal(items)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, grammarCase{"@start\n" + sg.Src, `import { createDocument, marshal } from "./parser.ts";
let bad = 0, n = 0;
for (const [text, p, r] of ` + string(b) + `) {
  const d = createDocument(text).document;
  d.parse();
  d.edit(p, p + 1, r);
  const a = d.parse();
  const f = createDocument(d.text()).document.parse();
  n++;
  if (marshal(a.node) !== marshal(f.node) || String(a.error) !== String(f.error)) bad++;
}
console.log("mismatches", bad, "of", n);
`})
	}
	run := func() (mismatches int) {
		for _, c := range cases {
			g, err := parser.Parse(c.src)
			if err != nil {
				t.Fatal(err)
			}
			got := codegentest.RunTSGrammarScriptBundle(t, g, c.script, common.GenOptions{Start: "main", Document: true}, codegentest.GenerateTSBundle)
			var bad, n int
			if _, err := fmt.Sscanf(strings.TrimSpace(got), "mismatches %d of %d", &bad, &n); err != nil || n == 0 {
				t.Fatalf("unexpected output %q: %v", got, err)
			}
			mismatches += bad
		}
		return mismatches
	}
	if bad := run(); bad != 0 {
		t.Fatalf("%d Documents differ from a new Document", bad)
	}
}
