package common_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// documentStatsScript is the text of a Document and the edits of TestDocumentStatsAcrossTargets, in
// code points: [start, end, replacement].
type documentStatsScript struct {
	Text  string
	Edits [][3]any
}

// documentStatsCase is a grammar whose only start rule is main, a text of it, and the fragments that
// the edits of its script insert.
type documentStatsCase struct {
	name, src, text string
	frags           []string
}

// documentStatsCases have direct and indirect left recursion, an Operators Rule, line-start anchors
// and lookahead, whose results the Documents reuse by the same rules.
var documentStatsCases = []documentStatsCase{
	{"incremental", strings.Replace(codegentest.IncrementalGrammar, "rule main", "@start\nrule main", 1),
		"a = 1+2\n@pos\nx,y\n\nb = (3)*-4\nc = 5\n",
		[]string{"a = 1+2*3\n", "@x\n", "a,b,c\n", "\n", "x", "1", "(", ")", "+", " = ", ",", "@", "-", "é", "zz = (4-5)*6\n"}},
	{"left recursion", `@start
rule main = (stmt ";")* $$
rule stmt = id "=" e / e
rule e = f "+" t / f "-" t / t
rule f = e
rule t = t "*" n / n
rule n = !"0" [0-9]+ / "0" / "(" e ")" / id
rule id = [a-z]+ #atomic`,
		"x=1+2;3-(4+5)*6;0;y=x*x-1;",
		[]string{"1", "+", "-", "*", "(", ")", ";", "x", "=", "0", "7*8;", "a=(1);", ""}},
}

// documentStatsScriptOf returns the script of 60 seeded edits of the case c, which keep most of the
// texts before the edits in the language of the grammar: while the text does not match, the edits
// mostly undo the last of them.
func documentStatsScriptOf(t *testing.T, c documentStatsCase) documentStatsScript {
	t.Helper()
	rng := rand.New(rand.NewSource(7))
	script := documentStatsScript{Text: c.text}
	ref := compileBare(t, c.src)
	text := []rune(c.text)
	// undo holds the edits that undo the edits made since the text last matched.
	var undo [][3]any
	matched := 0
	for range 60 {
		i := rng.Intn(len(text) + 1)
		j := i + rng.Intn(min(3, len(text)-i)+1)
		r := c.frags[rng.Intn(len(c.frags))]
		if _, err := ref.ParseWith("main", string(text), engine.ParseOptions{}); err == nil {
			matched++
			undo = undo[:0]
		}
		if n := len(undo); n > 0 && rng.Intn(4) != 0 {
			i, j, r = undo[n-1][0].(int), undo[n-1][1].(int), undo[n-1][2].(string)
			undo = undo[:n-1]
		} else {
			undo = append(undo, [3]any{i, i + len([]rune(r)), string(text[i:j])})
		}
		script.Edits = append(script.Edits, [3]any{i, j, r})
		text = append(append(append([]rune{}, text[:i]...), []rune(r)...), text[j:]...)
	}
	if matched < 20 {
		t.Fatalf("only %d of the texts before the edits match", matched)
	}
	return script
}

// TestDocumentStatsAcrossTargets runs the same edits on the Documents of the generated Go and
// TypeScript parsers of a grammar and requires the same statistics after each parse (evaluations,
// reuses, memo hits and resumed elements of repetitions), with the same end of the result and the same
// success.
func TestDocumentStatsAcrossTargets(t *testing.T) {
	t.Parallel()
	codegentest.PinOrdinary(t)
	tooltest.RequireNode(t)
	if testing.Short() {
		t.Skip("builds a generated Go program")
	}
	for _, c := range documentStatsCases {
		t.Run(c.name, func(t *testing.T) {
			g, err := parser.Parse(c.src)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(documentStatsScriptOf(t, c))
			if err != nil {
				t.Fatal(err)
			}
			goOut := documentStatsGo(t, g, string(data))
			tsOut := documentStatsTS(t, g, string(data))
			compareDocumentStats(t, "Go", goOut, "TypeScript", tsOut, "evaluated reused memo-hits resumed end failed")
			t.Logf("parses: %d", strings.Count(goOut, "\n"))
		})
	}
}

// TestDocumentStatsEngineAndGo runs the same edits on the Documents of the engine and of the generated
// Go parser of a grammar and requires the same statistics after each parse (evaluations, reuses, memo
// hits and resumed elements of repetitions), with the same end of the result and the same success: a
// Go Document reuses the results that the engine does.
func TestDocumentStatsEngineAndGo(t *testing.T) {
	t.Parallel()
	codegentest.PinOrdinary(t)
	if testing.Short() {
		t.Skip("builds a generated Go program")
	}
	for _, c := range documentStatsCases {
		t.Run(c.name, func(t *testing.T) {
			g, err := parser.Parse(c.src)
			if err != nil {
				t.Fatal(err)
			}
			script := documentStatsScriptOf(t, c)
			data, err := json.Marshal(script)
			if err != nil {
				t.Fatal(err)
			}
			goOut := documentStatsGo(t, g, string(data))
			d, err := compileBare(t, c.src).NewDocument("main", script.Text)
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			show := func() {
				n, err := d.Parse()
				end := -1
				if n != nil {
					end = n.End()
				}
				s := d.Stats()
				fmt.Fprintln(&b, s.Evaluated, s.Reused, s.MemoHits, s.ResumedElements, end, err != nil)
			}
			show()
			for _, e := range script.Edits {
				if err := d.Edit(e[0].(int), e[1].(int), e[2].(string)); err != nil {
					fmt.Fprintln(&b, "edit", err)
				}
				show()
			}
			compareDocumentStats(t, "engine", b.String(), "Go", goOut, "evaluated reused memo-hits resumed end failed")
		})
	}
}

// TestDocumentRunsAcrossTargets runs the same edits on the Documents of the engine and of the
// generated Go and TypeScript parsers of a grammar whose repetition is long enough for its runs to be
// recorded and resumed, and requires the same statistics after each parse (evaluations, reuses, memo
// hits and resumed elements of repetitions), with the same end of the result and the same success: the
// generated Documents resume the runs of repetitions that the engine does.
func TestDocumentRunsAcrossTargets(t *testing.T) {
	t.Parallel()
	codegentest.PinOrdinary(t)
	tooltest.RequireNode(t)
	if testing.Short() {
		t.Skip("builds a generated Go program")
	}
	cases := []documentStatsCase{{"long run", `@start
rule main = (item ";")* $$
rule item = id ("=" [0-9]+)? / "(" main ")"
rule id = [a-z]+ #atomic`,
		strings.Repeat("a;bb=1;c;", 12),
		[]string{"x;", "y=2;", "q", "k", "3", "(z;)", "w;", "ab;", "c=4;"}},
		// A repetition of a choice of rules, whose last element fails at the end of the text: after
		// an edit, the Documents call every alternative there again, each a failed memo hit.
		{"choice of rules", `@start
rule main = (setting / blank)* $$
rule setting = key " = " value "\n"
rule key = [a-z_]+ #atomic
rule value = [^\n]+ #atomic
rule blank = "\n"`,
			strings.Repeat("key_a = value 1\n\nkey_b = value 22\n", 12),
			[]string{"7", "x", "\n", "key_c = v\n", " = ", "q"}}}
	resumed := 0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, err := parser.Parse(c.src)
			if err != nil {
				t.Fatal(err)
			}
			script := documentStatsScriptOf(t, c)
			data, err := json.Marshal(script)
			if err != nil {
				t.Fatal(err)
			}
			goOut := documentStatsGo(t, g, string(data))
			tsOut := documentStatsTS(t, g, string(data))
			d, err := compileBare(t, c.src).NewDocument("main", script.Text)
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			show := func() {
				n, err := d.Parse()
				end := -1
				if n != nil {
					end = n.End()
				}
				s := d.Stats()
				fmt.Fprintln(&b, s.Evaluated, s.Reused, s.MemoHits, s.ResumedElements, end, err != nil)
				resumed += s.ResumedElements
			}
			show()
			for _, e := range script.Edits {
				if err := d.Edit(e[0].(int), e[1].(int), e[2].(string)); err != nil {
					fmt.Fprintln(&b, "edit", err)
				}
				show()
			}
			compareDocumentStats(t, "engine", b.String(), "Go", goOut, "evaluated reused memo-hits resumed end failed")
			compareDocumentStats(t, "engine", b.String(), "TypeScript", tsOut, "evaluated reused memo-hits resumed end failed")
		})
	}
	if resumed == 0 {
		t.Fatal("the Documents of the long run resumed no element of a repetition")
	}
}

// documentStatsTS runs the script on a Document of the generated TypeScript parser of g and returns
// what it prints for each parse, as documentStatsGo does for Go.
func documentStatsTS(t *testing.T, g *ast.Grammar, script string) string {
	t.Helper()
	return codegentest.RunTSGrammarScriptWith(t, g, `import { createDocument } from "./parser.ts";
const c = `+script+`;
const d = createDocument(c.Text).document;
const show = () => { const r = d.parse(); const s = d.stats(); console.log(s.evaluated, s.reused, s.memoHits, s.resumedElements, r.node === null ? -1 : r.node.end, r.error !== null); };
show();
for (const e of c.Edits) {
  const err = d.edit(e[0], e[1], e[2]);
  if (err !== null) console.log("edit", String(err));
  show();
}
`, common.GenOptions{Start: "main", Document: true})
}

// compareDocumentStats requires the lines that two Documents print for the parses of a script to be
// equal.
func compareDocumentStats(t *testing.T, aName, a, bName, b, fields string) {
	t.Helper()
	if a == b {
		return
	}
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := range min(len(al), len(bl)) {
		if al[i] != bl[i] {
			t.Fatalf("parse %d: %s %q, %s %q (%s)", i, aName, al[i], bName, bl[i], fields)
		}
	}
	t.Fatalf("%s has %d lines, %s %d", aName, len(al), bName, len(bl))
}

// documentStatsGo runs the script data on the Document of the generated Go parser of g and returns
// what it prints for each parse: its evaluations, reuses, memo hits and resumed elements, the end of
// its result and whether it failed.
func documentStatsGo(t *testing.T, g *ast.Grammar, data string) string {
	t.Helper()
	code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: "fixture", Start: "main", Document: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":       codegentest.GoMod("fixture"),
		"parser.go":    string(code),
		"main/main.go": strings.Replace(documentStatsMain, "SCRIPT", strconv.Quote(data), 1),
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "run", "./main")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	return string(out)
}

// documentStatsMain is the program that runs a script on the Document of a generated Go parser.
const documentStatsMain = `package main

import (
	"encoding/json"
	"fmt"

	p "fixture"
)

const data = SCRIPT

func main() {
	var c struct {
		Text  string
		Edits [][3]any
	}
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		panic(err)
	}
	d, err := p.NewDocument(c.Text)
	if err != nil {
		panic(err)
	}
	show := func() {
		n, err := d.Parse()
		end := -1
		if n != nil {
			end = n.End()
		}
		s := d.Stats()
		fmt.Println(s.Evaluated, s.Reused, s.MemoHits, s.ResumedElements, end, err != nil)
	}
	show()
	for _, e := range c.Edits {
		if err := d.Edit(int(e[0].(float64)), int(e[1].(float64)), e[2].(string)); err != nil {
			fmt.Println("edit", err)
		}
		show()
	}
}
`
