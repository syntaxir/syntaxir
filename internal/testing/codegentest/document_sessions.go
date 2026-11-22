package codegentest

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// DocumentSession is a conformance case of Document reuse: a grammar whose start rule is main, a text
// and the steps that edit and parse it. Every target runs it through its own runner, which prints the
// transcript of the session (DocumentSessionLine for each parse), and the transcripts of the targets
// are compared.
type DocumentSession struct {
	Name    string `json:"-"`
	Grammar string `json:"-"`
	// Text is the text the Document is made with; Bytes selects the Bytes unit for it and its edits,
	// which are in code points otherwise.
	Text  string         `json:"text"`
	Bytes bool           `json:"bytes"`
	Steps []DocumentStep `json:"steps"`
	// Long marks a case of the long tier.
	Long bool `json:"-"`
}

// DocumentStep is a step of a DocumentSession: with Parse set, a parse; otherwise Times repetitions
// (at least one) of the edit that replaces [Start, End) with Text.
type DocumentStep struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
	Times int    `json:"times"`
	Parse bool   `json:"parse"`
}

// DocumentSessionLine is the format of a line of a transcript: for one parse, the evaluated and
// reused counts of the Document's statistics, the units of Compute the parse charges (the smallest
// explicit Compute limit it completes under, with the session replayed for each probe without
// limits), the end of the result (-1 without one) and whether the parse failed.
const DocumentSessionLine = "evaluated reused units end failed"

// edit returns the step that replaces [start, end) with text once.
func edit(start, end int, text string) DocumentStep {
	return DocumentStep{Start: start, End: end, Text: text, Times: 1}
}

// parse is the step that parses the text.
var parse = DocumentStep{Parse: true}

// documentListGrammar is a list of numbers and names whose items a Document reuses at moved
// positions.
const documentListGrammar = `@start
rule main = item ("," item)* $$
rule item = num / name
rule num = [0-9]+ #atomic
rule name = [a-z]+ #atomic`

// DocumentSessions are the shared conformance cases of Document reuse. Each exercises a part of the
// contract of memo entries (spec/runtime/documents.md, Reuse): entries before an edit stay, entries
// after it move with their values, entries that examined the unit before their position, pinned
// entries and entries with recovered errors are evaluated again after an edit before them, an entry
// more than 128 edits behind is evaluated again without the edits being applied, and a log of 65,536
// edits drops the memo.
var DocumentSessions = []DocumentSession{
	{Name: "moves", Grammar: documentListGrammar, Text: "a,12,bc,3,d",
		Steps: []DocumentStep{parse, edit(0, 0, "x"), parse, edit(3, 4, "45"), parse, edit(0, 1, ""), parse, edit(6, 6, "zz,"), parse}},
	{Name: "moves bytes", Grammar: strings.ReplaceAll(documentListGrammar, "[a-z]", "[a-zé]"), Text: "é,12,bc,3,d", Bytes: true,
		Steps: []DocumentStep{parse, edit(0, 2, "x"), parse, edit(0, 0, "éé"), parse}},
	{Name: "same length", Grammar: documentListGrammar, Text: "a,12,bc,3,d",
		Steps: []DocumentStep{parse, edit(2, 3, "7"), parse, edit(0, 1, "q"), parse}},
	// line examines the unit before its position (^), so an edit that ends there invalidates it.
	{Name: "unit before", Grammar: `@start
rule main = (line / blank)* $$
rule blank = "\n"
rule line = ^ word ("," word)* "\n"
rule word = [a-z]+ #atomic`, Text: "ab,cd\nef\n\ngh,ij\n",
		Steps: []DocumentStep{parse, edit(5, 5, "x"), parse, edit(6, 6, "\n"), parse, edit(0, 0, "k"), parse}},
	// at reads the position of its result, so it is pinned.
	{Name: "pinned", Grammar: `type At = { At int }
@start
rule main = (at / word)* $$
rule at At = "@" n:word => At{At: $n.Start}
rule word = [a-z]+ ","? #atomic`, Text: "ab,@cd,ef,@gh",
		Steps: []DocumentStep{parse, edit(0, 0, "x"), parse, edit(13, 13, "i"), parse}},
	{Name: "recovered", Grammar: `@start
rule main = (stmt)* $$
rule stmt = (name "=" num ";") #recover(skip=[^;]+ ";")
rule name = [a-z]+ #atomic
rule num = [0-9]+ #atomic`, Text: "a=1;b=?;c=3;",
		Steps: []DocumentStep{parse, edit(0, 0, "x"), parse, edit(6, 7, "2"), parse, edit(0, 1, ""), parse}},
	// The edits at the end replace a unit with itself: 128 of them leave every entry before them
	// valid, with 128 edits to apply; one more makes each of them lag.
	{Name: "lag 128", Grammar: documentListGrammar, Text: "a,12,bc,3,d",
		Steps: []DocumentStep{parse, {Start: 10, End: 11, Text: "d", Times: 128}, parse, parse}},
	{Name: "lag 129", Grammar: documentListGrammar, Text: "a,12,bc,3,d",
		Steps: []DocumentStep{parse, {Start: 10, End: 11, Text: "d", Times: 129}, parse, parse}},
	{Name: "lag moved", Grammar: documentListGrammar, Text: "a,12,bc,3,d",
		Steps: []DocumentStep{parse, {Start: 0, End: 0, Text: "x", Times: 60}, parse, {Start: 0, End: 1, Text: "", Times: 60}, edit(10, 11, "e"), parse}},
	{Name: "log full", Grammar: documentListGrammar, Text: "a,12,bc,3,d", Long: true,
		Steps: []DocumentStep{parse, {Start: 10, End: 11, Text: "d", Times: 1<<16 - 1}, parse, edit(10, 11, "d"), parse, parse}},
}

// Script returns the session as the JSON that the runners read.
func (s DocumentSession) Script() string {
	data, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// Parse returns the grammar of the session.
func (s DocumentSession) Parse(t testing.TB) *ast.Grammar {
	t.Helper()
	g, err := parser.Parse(s.Grammar)
	if err != nil {
		t.Fatalf("%s: %v", s.Name, err)
	}
	return g
}

// documentSessionOptions are the options of the generated parsers of the sessions.
var documentSessionOptions = common.GenOptions{Start: "main", Document: true}

// RunDocumentSessionTS returns the transcript of the session on the Document of the generated
// TypeScript parser.
func RunDocumentSessionTS(t *testing.T, s DocumentSession) string {
	t.Helper()
	return RunTSGrammarScriptWith(t, s.Parse(t), strings.Replace(documentSessionTS, "SCRIPT", s.Script(), 1), documentSessionOptions)
}

// documentSessionTS is the program that runs a session on the Document of a generated TypeScript
// parser.
const documentSessionTS = `import * as p from "./parser.ts";
const c: { text: string; bytes: boolean; steps: { start: number; end: number; text: string; times: number; parse: boolean }[] } = SCRIPT;
const unlimited = { limits: { compute: Number.MAX_SAFE_INTEGER } };
// replay makes the Document and runs the steps up to the parse k, which it parses with options; the
// parses before it have no limit.
const replay = (k: number, options: p.ParseOptions) => {
  const made = p.createDocument(c.text, { unit: c.bytes ? p.Bytes : p.CodePoints });
  if (made.error !== null) throw made.error;
  const d = made.document!;
  let n = 0;
  for (const s of c.steps) {
    if (!s.parse) {
      for (let i = 0; i < s.times; i++) {
        const err = d.edit(s.start, s.end, s.text);
        if (err !== null) throw err;
      }
      continue;
    }
    if (n++ === k) return { d, r: d.parse(options) };
    d.parse(unlimited);
  }
  throw new Error("no parse " + k);
};
// fits reports whether the parse k fits the Compute limit; a parse that fits must end as the
// unlimited parse, whose error is want.
const fits = (k: number, limit: number, want: string) => {
  const e = replay(k, { limits: { compute: limit } }).r.error;
  if (e instanceof p.ParseInterruptionError && e.kind === "compute-limit") return false;
  if (String(e) !== want) throw new Error("parse " + k + " under the limit " + limit + ": " + String(e) + ", want " + want);
  return true;
};
const parses = c.steps.filter(s => s.parse).length;
for (let k = 0; k < parses; k++) {
  const { d, r } = replay(k, unlimited);
  const want = String(r.error);
  let hi = 16;
  while (!fits(k, hi, want)) hi *= 2;
  let lo = 0;
  while (hi - lo > 1) {
    const mid = lo + Math.floor((hi - lo) / 2);
    if (fits(k, mid, want)) hi = mid; else lo = mid;
  }
  const s = d.stats();
  console.log(s.evaluated, s.reused, hi, r.node === null ? -1 : r.node.end, r.error !== null);
}
`

// RunDocumentSessionGo returns the transcript of the session on the Document of the generated Go
// parser.
func RunDocumentSessionGo(t *testing.T, s DocumentSession) string {
	t.Helper()
	opts := documentSessionOptions
	opts.Package = "fixture"
	code, err := GenerateCode(t, s.Parse(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":       GoMod("fixture"),
		"parser.go":    string(code),
		"main/main.go": strings.Replace(documentSessionGo, "SCRIPT", strconv.Quote(s.Script()), 1),
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

// documentSessionGo is the program that runs a session on the Document of a generated Go parser.
const documentSessionGo = `package main

import (
	"encoding/json"
	"errors"
	"fmt"

	p "fixture"
)

const data = SCRIPT

type step struct {
	Start, End, Times int
	Text              string
	Parse             bool
}

var c struct {
	Text  string
	Bytes bool
	Steps []step
}

// replay makes the Document and runs the steps up to the parse k, which it parses with the
// Compute limit compute; the parses before it have no limit.
func replay(k int, compute uint64) (p.Document, *p.Node, error) {
	unit := p.CodePoints
	if c.Bytes {
		unit = p.Bytes
	}
	d, err := p.NewDocument(c.Text, p.WithUnit(unit))
	if err != nil {
		panic(err)
	}
	n := 0
	for _, s := range c.Steps {
		if !s.Parse {
			for range s.Times {
				if err := d.Edit(s.Start, s.End, s.Text); err != nil {
					panic(err)
				}
			}
			continue
		}
		if n == k {
			node, err := d.Parse(p.WithLimits(p.Limits{Compute: compute}))
			return d, node, err
		}
		n++
		d.Parse(p.WithLimits(p.Limits{Compute: p.Unlimited}))
	}
	panic(fmt.Sprint("no parse ", k))
}

// fits reports whether the parse k fits the Compute limit; a parse that fits must end as the
// unlimited parse, whose error is want.
func fits(k int, limit uint64, want string) bool {
	_, _, err := replay(k, limit)
	var le *p.LimitError
	if errors.As(err, &le) && le.Kind == p.LimitCompute {
		return false
	}
	if fmt.Sprint(err) != want {
		panic(fmt.Sprintf("parse %d under the limit %d: %v, want %s", k, limit, err, want))
	}
	return true
}

func main() {
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		panic(err)
	}
	parses := 0
	for _, s := range c.Steps {
		if s.Parse {
			parses++
		}
	}
	for k := range parses {
		d, node, err := replay(k, p.Unlimited)
		want := fmt.Sprint(err)
		hi := uint64(16)
		for !fits(k, hi, want) {
			hi *= 2
		}
		lo := uint64(0)
		for hi-lo > 1 {
			if mid := lo + (hi-lo)/2; fits(k, mid, want) {
				hi = mid
			} else {
				lo = mid
			}
		}
		end := -1
		if node != nil {
			end = node.End()
		}
		s := d.Stats()
		fmt.Println(s.Evaluated, s.Reused, hi, end, err != nil)
	}
}
`
