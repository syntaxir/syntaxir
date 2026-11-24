package print_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// documentRunsCase is a Document session of TestTSDocumentRuns: a grammar whose start rule is main, a
// text and edits [start, end, replacement] in code points, each followed by a parse. With Signal set,
// the parses after the edits run with a cancellation signal that counts its observations; the parse
// after the last edit is canceled at the Signal-th observation.
type documentRunsCase struct {
	name, grammar string
	Text          string   `json:"text"`
	Edits         [][3]any `json:"edits"`
	Signal        int      `json:"signal"`
	// resumes is the least number of parses that resume elements.
	resumes int
}

// documentRunsCases exercise the resumption of runs: a long list edited before, inside and after its
// elements; a run that resyncs only where an old element starts; a bounded repetition whose new
// elements after the resumed old ones are recorded in the order of the text; and a long run whose
// reused prefix observes a cancellation.
var documentRunsCases = []documentRunsCase{
	{name: "list", grammar: `@start
rule main = item ("," item)* $$
rule item = num / name
rule num = [0-9]+ #atomic
rule name = [a-z]+ #atomic`, Text: strings.Repeat("ab,12,", 15) + "c",
		Edits: [][3]any{{6, 6, "x,"}, {0, 0, "q,"}, {19, 21, "7"}, {60, 60, ""}}, resumes: 3},
	{name: "resync", grammar: `@start
rule main = item* $$
rule item = "ab" / "a" / "b"`, Text: strings.Repeat("ab", 40),
		Edits: [][3]any{{11, 11, "b"}, {30, 30, "a"}}, resumes: 2},
	{name: "order", grammar: `@start
rule main = item #repeat(0, 50) item* $$
rule item = "a;" / "b;"`, Text: strings.Repeat("a;", 60),
		Edits: [][3]any{{20, 40, ""}, {98, 100, "b;"}}, resumes: 2},
	{name: "cancel", grammar: `@start
rule main = item* $$
rule item = "a;" / "b;"`, Text: strings.Repeat("a;", 20*256),
		Edits: [][3]any{{20 * 512, 20 * 512, "b;"}, {20 * 512, 20*512 + 2, ""}}, Signal: 4, resumes: 1},
}

// documentRunsScript runs the session CASE on the Document of the generated parser and prints, for each
// parse, its evaluations, reuses, memo hits and resumed elements, the end of its result, whether the
// tree equals the tree of a parse of the text from scratch, and, with a signal, the observations of the
// signal and how the parse ended. It prints last whether the tree of the first parse kept its
// positions, which a resumed element that moved must leave as they were, since the element moves in a
// copy.
const documentRunsScript = `
{
  const c: { text: string; edits: [number, number, string][]; signal: number } = CASE;
  const d = createDocument(c.text).document!;
  const spans = (n: Node | null): string => n === null ? "-" : n.start + ":" + n.end + "(" + n.children.map(spans).join(",") + ")";
  const out: string[] = [];
  const show = (cancelAt: number): ParseResult => {
    let calls = 0;
    const signal = { get aborted(): boolean { calls++; return cancelAt !== 0 && calls >= cancelAt; }, reason: "stop" };
    const r = c.signal === 0 ? d.parse() : d.parse({ signal });
    const s = d.stats();
    const line: (string | number | boolean)[] = [s.evaluated, s.reused, s.memoHits, s.resumedElements, r.node === null ? -1 : r.node.end];
    if (r.node !== null) line.push(spans(r.node) === spans(parse(d.text()).node));
    if (c.signal !== 0) line.push(calls, r.error === null ? "ok" : (r.error as ParseInterruptionError).kind);
    out.push(line.join(" "));
    return r;
  };
  const first = show(0).node;
  const before = spans(first);
  c.edits.forEach(([start, end, text], i) => {
    d.edit(start, end, text);
    show(i === c.edits.length - 1 ? c.signal : 0);
  });
  out.push(String(spans(first) === before));
  console.log(out.join("\n"));
}
`

// documentRunsMutants change the resumption of runs in the TypeScript library in one way each: in the
// methods printed from the run compositions (runReuse, runPrefix, runResync, runTail) or in the run
// records of DocumentParser. Each is the text replaced and its replacement.
var documentRunsMutants = []struct{ name, old, new string }{
	{"a reused element observes no signal", "    this.control?.step();\n    const v = this.runTake(", "    const v = this.runTake("},
	{"a resumed element counts only as a memo hit", "    this.resumed++;\n", ""},
	{"the reused prefix ignores the edit", "if (r.old[r.k]!.examined > r.editStart) {", "if (false) {"},
	{"the reused tail does not move", "this.runReuse(r, i, r.editDelta,", "this.runReuse(r, i, 0,"},
	{"a run resyncs at an old element that starts elsewhere", "r.seek === n || r.old[r.seek]!.start !== at", "r.seek === n"},
	{"a moved element keeps the positions of its value", "      el.v = this.moveRunValue(el.v, shift);\n", ""},
	{"a moved element moves in place", "      el.v = this.moveRunValue(el.v, shift);\n", "      moveNodeInPlace(el.v, shift);\n"},
	{"the elements after the edit are recorded before the old ones", "(after ? r.post : r.mid)", "(r.mid)"},
	{"no run of the parse before is taken", "    r.old = rec.elems;\n    t!.delete(at);\n", ""},
	{"no run is recorded", "    t.set(r.start, { gen: this.gen, elems });\n", ""},
}

// documentRunsInPlace moves the positions of the nodes of n by shift in place, for the mutant that moves
// a resumed element without a copy.
const documentRunsInPlace = `
function moveNodeInPlace(n: Node | null, shift: number): void {
  if (n === null) return;
  n.start += shift;
  n.end += shift;
  for (const c of n.children) moveNodeInPlace(c, shift);
}
`

// TestTSDocumentRuns checks that a TypeScript Document resumes the runs of long repetitions after an
// edit, reusing the elements before the edit and, back in step after it, the moved elements after it,
// with the trees of a parse from scratch (TestDocumentRunsAcrossTargets compares the counts with the
// engine's); that a moved element leaves the tree of an earlier parse unchanged; and that a parse that
// reuses a long run observes a cancellation. As negative controls, each mutant of documentRunsMutants
// changes what some session prints.
func TestTSDocumentRuns(t *testing.T) {
	node := tooltest.RequireNode(t)
	opts := common.GenOptions{Start: "main", Document: true, NoOpt: []string{codegen.UsedFeatures}}
	type session struct{ src, script, want string }
	run := func(s session, mutate func(string) string) string {
		src := s.src
		if mutate != nil {
			src = mutate(src)
		}
		dir := t.TempDir()
		for name, content := range map[string]string{"package.json": `{"type": "module"}`, "parser.ts": src + s.script, "main.ts": `import "./parser.ts";` + "\n"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		out, _ := tooltest.NodeCombinedOutput(t, node, dir, "main.ts")
		return string(out)
	}
	var sessions []session
	for _, c := range documentRunsCases {
		g, err := parseTest(c.grammar)
		if err != nil {
			t.Fatal(err)
		}
		code, err := codegentest.GenerateTSBundle(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		s := session{src: string(code), script: strings.Replace(documentRunsScript, "CASE", string(data), 1) + documentRunsInPlace}
		s.want = run(s, nil)
		t.Logf("%s:\n%s", c.name, s.want)
		resumes := 0
		lines := strings.Split(strings.TrimSpace(s.want), "\n")
		for _, l := range lines[:len(lines)-1] {
			f := strings.Fields(l)
			if len(f) < 6 || f[5] != "true" {
				if c.Signal != 0 && len(f) >= 7 && f[len(f)-1] == "canceled" {
					continue
				}
				t.Fatalf("%s: a parse failed or differs from a parse from scratch:\n%s", c.name, codegentest.Clip(s.want, 0))
			}
			if f[3] != "0" {
				resumes++
			}
		}
		if resumes < c.resumes || lines[len(lines)-1] != "true" {
			t.Fatalf("%s: the Document resumed runs in %d parses, want at least %d, or changed an earlier tree:\n%s", c.name, resumes, c.resumes, codegentest.Clip(s.want, 0))
		}
		if c.Signal != 0 && !strings.HasSuffix(lines[len(lines)-2], " canceled") {
			t.Fatalf("%s: the parse that reuses a long run was not canceled:\n%s", c.name, codegentest.Clip(s.want, 0))
		}
		sessions = append(sessions, s)
	}
	for _, m := range documentRunsMutants {
		t.Run(m.name, func(t *testing.T) {
			for _, s := range sessions {
				got := run(s, func(src string) string {
					if strings.Count(src, m.old) != 1 {
						t.Fatalf("the library has %d copies of %q, want 1", strings.Count(src, m.old), m.old)
					}
					return strings.Replace(src, m.old, m.new, 1)
				})
				if got != s.want {
					return
				}
			}
			t.Errorf("no session notices the mutant %q", m.name)
		})
	}
}
