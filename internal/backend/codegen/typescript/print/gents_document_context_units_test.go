package print_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// documentContextGrammar grows b at the start of a in the context of a's growth, so that the steps
// of b's growth are kept and compared by value to give equal results one identity. num ends in an
// empty run of blanks, whose node a Document copies when it moves a reused num and builds anew when
// it evaluates one, so that steps compare equal trees made in different parses.
const documentContextGrammar = `rule main = a !.
rule a = b "+" num / num
rule b = a "-" num / num
rule num = [0-9]+ blanks
rule blanks = " "*`

// longNumbersGrammar is documentContextGrammar without num, whose numbers longNumbersText makes 20
// digits long, and longNumbersEdits edits them at the start, the end and the middle.
const longNumbersGrammar = `rule main = a !.
rule a = b "+" num / num
rule b = a "-" num / num`

var longNumbersText = strings.Repeat("1", 20) + "+" + strings.Repeat("2", 20) + "-" + strings.Repeat("3", 20)

var longNumbersEdits = []contextEdit{
	{Start: 0, End: 1, Text: "7"}, {Start: 0, End: 1, Text: "8"}, {Start: 61, End: 62, Text: "9"},
	{Start: 30, End: 30, Text: "5"}, {Start: 30, End: 31, Text: ""},
}

// contextEdit replaces the text from Start to End of a Document with Text, as the edits of
// codegentest.GrowthValuesEdits do.
type contextEdit = struct {
	Start, End int
	Text       string
}

// documentContextEdits edits the text of documentContextGrammar before, inside and after the
// growths, with and without changing its length, and back.
var documentContextEdits = []contextEdit{
	{Start: 0, End: 1, Text: "7"}, {Start: 8, End: 9, Text: "6"}, {Start: 4, End: 4, Text: "8"}, {Start: 0, End: 0, Text: "9"},
	{Start: 2, End: 2, Text: " "}, {Start: 5, End: 6, Text: ""}, {Start: 10, End: 11, Text: "-3"}, {Start: 3, End: 3, Text: "x"},
	{Start: 3, End: 4, Text: ""}, {Start: 0, End: 2, Text: "1"}, {Start: 6, End: 6, Text: "+2-2"},
}

// documentContextScript parses a Document of TEXT, then after each edit of EDITS, and reports for
// each parse its compute units, whether it succeeded, and the results it evaluated and reused.
const documentContextScript = `
{
  const { document } = createDocument(TEXT);
  const out: string[] = [];
  const parse = () => {
    const r = { units: 0 };
    const res = document!.parse({ computeReport: r });
    const s = document!.stats();
    out.push(r.units + " " + (res.error === null) + " " + s.evaluated + " " + s.reused);
  };
  parse();
  for (const [start, end, text] of EDITS as [number, number, string][]) {
    const err = document!.edit(start, end, text);
    if (err !== null) throw err;
    parse();
  }
  console.log(out.join("\n"));
}
`

// TestTSDocumentContextUnitsAgreeWithEngine holds TypeScript Documents whose results grow in the
// contexts of other growths to the engine's Documents over a series of edits: each parse counts the
// same compute units and evaluates and reuses as many results. The units of a parse depend on which
// step results the comparison finds equal; the cases with long numbers put runs of 16 digits or more,
// which a parse records, into the steps. TestLibNodesEqualComparesValues holds the comparison itself.
func TestTSDocumentContextUnitsAgreeWithEngine(t *testing.T) {
	node := tooltest.RequireNode(t)
	for _, c := range []struct {
		name, grammar, text string
		edits               []contextEdit
	}{
		{"moved", documentContextGrammar, "1+2-3+4-5+6", documentContextEdits},
		{"values", codegentest.GrowthValuesGrammar, codegentest.GrowthValuesText, codegentest.GrowthValuesEdits},
		{"scan", longNumbersGrammar + "\nrule num = [0-9]+", longNumbersText, longNumbersEdits},
		{"run", longNumbersGrammar + "\nrule num = digit+\nrule digit = [0-9]", longNumbersText, longNumbersEdits},
	} {
		t.Run(c.name, func(t *testing.T) {
			g, err := parseTest(c.grammar)
			if err != nil {
				t.Fatal(err)
			}
			prog := mustCompile(t, c.grammar)
			d, err := prog.NewDocumentWith("main", c.text, engine.ParseOptions{})
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for i := 0; i <= len(c.edits); i++ {
				if i > 0 {
					e := c.edits[i-1]
					if err := d.Edit(e.Start, e.End, e.Text); err != nil {
						t.Fatal(err)
					}
				}
				var units uint64
				o := d.ParseDefaults()
				o.ComputeReport = &units
				_, err := d.ParseWith(o)
				s := d.Stats()
				want = append(want, fmt.Sprintf("%d %v %d %d", units, err == nil, s.Evaluated, s.Reused))
			}
			text, err := json.Marshal(c.text)
			if err != nil {
				t.Fatal(err)
			}
			var edits []any
			for _, e := range c.edits {
				edits = append(edits, []any{e.Start, e.End, e.Text})
			}
			quoted, err := json.Marshal(edits)
			if err != nil {
				t.Fatal(err)
			}
			script := strings.NewReplacer("TEXT", string(text), "EDITS", string(quoted)).Replace(documentContextScript)
			run := func() string {
				code, err := codegentest.GenerateTSBundle(g, common.GenOptions{Start: "main", Document: true})
				if err != nil {
					t.Fatal(err)
				}
				src := string(code)
				dir := t.TempDir()
				for name, content := range map[string]string{"package.json": `{"type": "module"}`, "parser.ts": src + script, "main.ts": `import "./parser.ts";` + "\n"} {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				out, _ := tooltest.NodeCombinedOutput(t, node, dir, "main.ts")
				return strings.TrimSpace(string(out))
			}
			w := strings.Join(want, "\n")
			if got := run(); got != w {
				t.Fatalf("Document parses:\n%s\nwant the engine's\n%s", codegentest.Clip(got, 0), w)
			}
		})
	}
}
