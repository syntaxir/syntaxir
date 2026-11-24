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

// documentFreeScript parses a Document of the input, edits it and parses it again, and reports the
// compute units of the two parses, then whether the parse after the edit, in Documents of their own,
// passes under a compute limit of its units and fails under one less.
const documentFreeScript = `
{
  const units = (f: (r: { units: number }) => void): number => {
    const r = { units: 0 };
    f(r);
    return r.units;
  };
  // edited returns a Document of "aaaa" that has been parsed and then edited.
  const edited = () => {
    const { document } = createDocument("aaaa");
    const first = units(r => document!.parse({ computeReport: r }));
    document!.edit(1, 2, "aaa");
    return { d: document!, first };
  };
  const { d, first } = edited();
  const after = units(r => d.parse({ computeReport: r }));
  const within = edited().d.parse({ limits: { compute: after } });
  const over = edited().d.parse({ limits: { compute: after - 1 } });
  console.log(first + "," + after, String(within.error), String(over.error instanceof ParseInterruptionError && over.error.kind));
}
`

// TestTSDocumentWorkCountsNoUnits checks that a Document parse of the TypeScript library counts the
// compute units of the evaluations it makes, as the engine's Document does: the first parse those of a
// parse from scratch, a parse after an edit those of what it evaluates again, and the work of the
// Document (the reuse and moves of its entries) none; and that its compute limit is decided on them
// (spec/runtime/parsing.md, Compute Units). As a negative control, the library whose move of a memo
// entry counts a unit must fail.
func TestTSDocumentWorkCountsNoUnits(t *testing.T) {
	const src = `rule main = item*
rule item = "a"`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	prog := mustCompile(t, src)
	d, err := prog.NewDocumentWith("main", "aaaa", engine.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var first, after uint64
	o := d.ParseDefaults()
	o.ComputeReport = &first
	if _, err := d.ParseWith(o); err != nil {
		t.Fatal(err)
	}
	if err := d.Edit(1, 2, "aaa"); err != nil {
		t.Fatal(err)
	}
	o.ComputeReport = &after
	if _, err := d.ParseWith(o); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%d,%d null compute-limit", first, after)
	opts := common.GenOptions{Start: "main", Document: true}
	node := tooltest.RequireNode(t)
	run := func(mutate func(string) string) string {
		code, err := codegentest.GenerateTSBundle(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		src := string(code)
		if mutate != nil {
			src = mutate(src)
		}
		dir := t.TempDir()
		for name, content := range map[string]string{"package.json": `{"type": "module"}`, "parser.ts": src + documentFreeScript, "main.ts": `import "./parser.ts";` + "\n"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		out, _ := tooltest.NodeCombinedOutput(t, node, dir, "main.ts")
		return strings.TrimSpace(string(out))
	}
	if got := run(nil); got != want {
		t.Fatalf("Document units before and after the edit, then the limits: %s, want the engine's %s", codegentest.Clip(got, 0), want)
	}
	if run(func(src string) string {
		const hit = "  documentMove(e: MemoEntry): boolean {\n"
		if !strings.Contains(src, hit) {
			t.Fatalf("the library has no %q", hit)
		}
		return strings.Replace(src, hit, hit+"    this.units++;\n", 1)
	}) == want {
		t.Error("the library whose Document work counts a unit passes")
	}
}

// documentGuardScript reports, for each input of INPUTS, the compute units of a parse from scratch and of
// the first parse of a Document of the input.
const documentGuardScript = `
{
  const units = (f: (r: { units: number }) => void): number => {
    const r = { units: 0 };
    f(r);
    return r.units;
  };
  const out: string[] = [];
  for (const input of INPUTS) {
    const { document } = createDocument(input);
    out.push(units(r => parseWithOptions(input, { computeReport: r })) + "/" + units(r => document!.parse({ computeReport: r })));
  }
  console.log(out.join(" "));
}
`

// TestTSDocumentGuardsSkipOutsideDocuments checks the guards whose skip passes over a call of a rule that
// a Document memoizes, k here: the parser generated for Documents keeps them for the parses that are not
// a Document's (flow.inDocument), whose units are those of the parser generated without Documents, and
// evaluates the alternative in a Document parse, which memoizes k and so counts fewer units where a later
// alternative calls k again. Both counts must be the engine's.
func TestTSDocumentGuardsSkipOutsideDocuments(t *testing.T) {
	const src = `rule main = (k "x" / k "y" / "z")*
rule k = "a"`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	inputs := []string{"", "z", "ax", "ay", "zzaxzay", "aya", "q"}
	prog := mustCompile(t, src)
	var want []string
	for _, in := range inputs {
		var plain, doc uint64
		// An input that the grammar does not take fails with a syntax error, which counts its units too.
		_, _ = prog.ParseWith("main", in, engine.ParseOptions{ComputeReport: &plain})
		d, err := prog.NewDocumentWith("main", in, engine.ParseOptions{})
		if err != nil {
			t.Fatal(err)
		}
		o := d.ParseDefaults()
		o.ComputeReport = &doc
		_, _ = d.ParseWith(o)
		want = append(want, fmt.Sprintf("%d/%d", plain, doc))
	}
	js, _ := json.Marshal(inputs)
	script := strings.ReplaceAll(documentGuardScript, "INPUTS", string(js))
	node := tooltest.RequireNode(t)
	run := func(document bool) string {
		code, err := codegentest.GenerateTSBundle(g, common.GenOptions{Start: "main", Document: document})
		if err != nil {
			t.Fatal(err)
		}
		src := string(code)
		if document && !strings.Contains(src, "p.inDocument") {
			t.Fatal("the parser for Documents has no guard that only the parses outside Documents take")
		}
		if !document {
			// Without Documents, the Document parse of the script is a parse from scratch.
			src += "\nconst createDocument = (input: string) => ({ document: { parse: (o: ParseOptions) => parseWithOptions(input, o) } });\n"
		}
		dir := t.TempDir()
		for name, content := range map[string]string{"package.json": `{"type": "module"}`, "parser.ts": src + script, "main.ts": `import "./parser.ts";` + "\n"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		out, err := tooltest.NodeCombinedOutput(t, node, dir, "main.ts")
		if err != nil {
			t.Fatalf("node: %v\n%s", err, codegentest.Clip(string(out), 0))
		}
		return strings.TrimSpace(string(out))
	}
	var plainWant []string
	for _, w := range want {
		p, _, _ := strings.Cut(w, "/")
		plainWant = append(plainWant, p+"/"+p)
	}
	if got := run(false); got != strings.Join(plainWant, " ") {
		t.Errorf("units of each input without Documents, from scratch/from scratch: %s, want the engine's %s", got, strings.Join(plainWant, " "))
	}
	if got := run(true); got != strings.Join(want, " ") {
		t.Errorf("units of each input with Documents, from scratch/Document: %s, want the engine's %s", got, strings.Join(want, " "))
	}
}
