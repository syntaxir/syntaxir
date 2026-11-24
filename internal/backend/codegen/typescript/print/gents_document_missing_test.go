package print_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// documentInsertionLimits are the missing-token limits of the Document comparisons: one and two,
// which some inputs reach, and one that none reaches.
var documentInsertionLimits = []int{1, 2, 64}

// tsDocumentEdit is an edit of a Document, in its position unit, with the replacement in base64.
type tsDocumentEdit struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

// tsDocumentInsertionRun is the Document comparison of one case in one unit at one limit: the
// Document starts with the first text, in base64, and takes the edits in order.
type tsDocumentInsertionRun struct {
	Case  int              `json:"case"`
	First string           `json:"first"`
	Edits []tsDocumentEdit `json:"edits"`
	Bytes bool             `json:"bytes"`
	Limit int              `json:"limit"`
}

// tsDocumentInsertionScript runs the Document comparisons of runs.json on the parsers g<i>.ts and
// prints one JSON line for each: the differences of its parses from the parses from scratch of the
// same text with the same limit, and the compute units of each parse. Given "typed", the parses are
// those of parseValue. Each parse of a Document is compared after a first parse, after each edit, again
// without an edit, observed and after the observed parse; a parse that changes the limit must be
// rejected.
const tsDocumentInsertionScript = `import { readFileSync } from "node:fs";
const runs = JSON.parse(readFileSync("runs.json", "utf8"));
const typed = process.argv[2] === "typed";
const bytesOf = (b64) => new Uint8Array(Buffer.from(b64, "base64"));
const replacer = (_key, v) => typeof v === "bigint" ? String(v) + "n" : v;
for (const v of runs) {
  const m = await import("./g" + v.case + ".ts");
  const unit = v.bytes ? m.Bytes : m.CodePoints;
  const res = { diffs: [], units: [] };
  const show = (r) => (typed ? JSON.stringify(r.value, replacer) : m.marshal(r.node)) + " " + (r.error === null ? "null" : r.error.message);
  const made = m.createDocument(bytesOf(v.first), { unit, missingTokens: v.limit });
  if (made.error !== null) {
    res.diffs.push("createDocument: " + made.error.message);
    console.log(JSON.stringify(res));
    continue;
  }
  const d = made.document;
  const parse = (what, extra) => {
    const report = { units: 0 };
    const r = typed ? d.parseValue({ computeReport: report, ...extra }) : d.parse({ computeReport: report, ...extra });
    res.units.push(report.units);
    const text = d.bytes();
    const options = { unit, missingTokens: v.limit, ...extra };
    const fresh = typed ? m.parseValueWithOptions(text, options) : m.parseWithOptions(text, options);
    if (show(r) !== show(fresh)) res.diffs.push(what + ", text " + JSON.stringify(Buffer.from(text).toString("latin1")) + ":\n document     " + show(r) + "\n from scratch " + show(fresh));
  };
  parse("first parse", {});
  const changed = typed ? d.parseValue({ missingTokens: v.limit + 1 }) : d.parse({ missingTokens: v.limit + 1 });
  if (changed.error === null || !changed.error.message.includes("missing-token limit")) res.diffs.push("a parse with the limit " + (v.limit + 1) + " was not rejected: " + show(changed));
  for (const e of v.edits) {
    const err = d.edit(e.start, e.end, bytesOf(e.text));
    if (err !== null) {
      res.diffs.push("edit " + JSON.stringify(e) + ": " + err.message);
      break;
    }
    parse("after an edit", {});
  }
  parse("again", {});
  parse("observed", { trace() {} });
  parse("after the observed parse", {});
  console.log(JSON.stringify(res));
}
`

// tsDocumentInsertionMismatches generates a TypeScript parser with a Document for each case, with the
// library that patch changes, runs the Document comparisons of every case at documentInsertionLimits,
// and describes each parse that differs from the parse from scratch, and each change of the limit that
// is not rejected. With units, it also describes each run whose parses count other compute units than
// the engine's Document along the same edits. With typed, the parsers are generated with types and the
// parses are those of parseValue.
func tsDocumentInsertionMismatches(t *testing.T, node string, cases []codegentest.Case, patch func(string) string, units, typed bool) []string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type": "module"}`+"\n")
	write("main.mjs", tsDocumentInsertionScript)
	var runs []tsDocumentInsertionRun
	var names []string
	var want [][]uint64
	for i, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		code, err := codegentest.GenerateTSBundle(g, common.GenOptions{Start: "main", Document: true, Observability: true, Types: typed})
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		write(fmt.Sprintf("g%d.ts", i), patch(string(code)))
		var prog *engine.Program
		if units {
			prog = compileBare(t, c.Src, check.Options{NoProjections: true})
		}
		for _, bytes := range []bool{false, true} {
			for _, limit := range documentInsertionLimits {
				v := tsDocumentInsertionRun{Case: i, First: base64.StdEncoding.EncodeToString([]byte(c.Inputs[0])), Bytes: bytes, Limit: limit}
				text := c.Inputs[0]
				var edits []string
				for _, next := range append(c.Inputs[1:], c.Inputs[0]) {
					start, end, repl := codegentest.MinimalEdit(text, next, bytes)
					v.Edits = append(v.Edits, tsDocumentEdit{start, end, base64.StdEncoding.EncodeToString([]byte(repl))})
					edits = append(edits, repl)
					text = next
				}
				runs = append(runs, v)
				names = append(names, fmt.Sprintf("%s (bytes %v, limit %d)", c.Name, bytes, limit))
				if units {
					want = append(want, engineDocumentInsertionUnits(t, prog, v, edits))
				} else {
					want = append(want, nil)
				}
			}
		}
	}
	data, err := json.Marshal(runs)
	if err != nil {
		t.Fatal(err)
	}
	write("runs.json", string(data))
	args := []string{"main.mjs"}
	if typed {
		args = append(args, "typed")
	}
	cmd := exec.Command(node, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, codegentest.Clip(string(out), 0))
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(runs) {
		t.Fatalf("got %d results, want %d\n%s", len(lines), len(runs), codegentest.Clip(string(out), 0))
	}
	var mismatches []string
	for k, line := range lines {
		var res struct {
			Diffs []string
			Units []uint64
		}
		if err := json.Unmarshal([]byte(line), &res); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		for _, d := range res.Diffs {
			mismatches = append(mismatches, names[k]+": "+d)
		}
		if units && fmt.Sprint(res.Units) != fmt.Sprint(want[k]) {
			mismatches = append(mismatches, fmt.Sprintf("%s: compute units of the parses\n generated %v\n engine    %v", names[k], res.Units, want[k]))
		}
	}
	return mismatches
}

// engineDocumentInsertionUnits runs v, whose replacements are edits, with an engine Document, parse
// for parse as the script does, and returns the compute units of each parse.
func engineDocumentInsertionUnits(t *testing.T, prog *engine.Program, v tsDocumentInsertionRun, edits []string) []uint64 {
	t.Helper()
	o := engine.ParseOptions{Unit: input.CodePoints, Expectations: true, MissingTokens: v.Limit, Tune: state.Tuning{MinRec: 1}}
	if v.Bytes {
		o.Unit = input.Bytes
	}
	first, err := base64.StdEncoding.DecodeString(v.First)
	if err != nil {
		t.Fatal(err)
	}
	d, err := prog.NewDocumentWith("main", string(first), o)
	if err != nil {
		t.Fatalf("new document: %v", err)
	}
	var out []uint64
	parse := func(opts ...sxirruntime.ParseOption) {
		var units uint64
		po := o
		engine.Apply(&po, append(opts, sxirruntime.WithComputeReport(&units)))
		d.ParseWith(po)
		out = append(out, units)
	}
	parse()
	for i, e := range v.Edits {
		if err := d.Edit(e.Start, e.End, edits[i]); err != nil {
			t.Fatalf("edit %+v: %v", e, err)
		}
		parse()
	}
	parse()
	parse(sxirruntime.WithTrace(func(instrument.TraceEvent) {}))
	parse()
	return out
}

// everyRun patches the library of a generated parser to record the runs of every repetition, however
// short, as the engine's Documents do with state.Tuning.MinRec 1: the corpus inputs are short. The
// library of a parser that records no runs has no threshold to patch; patched counts the libraries
// patched.
func everyRun(patched *int) func(string) string {
	return func(src string) string {
		const old = "const minRecordedRun = 16;\n"
		if !strings.Contains(src, old) {
			return src
		}
		*patched++
		return strings.Replace(src, old, "const minRecordedRun = 1;\n", 1)
	}
}

// TestTSDocumentsInsertMissingTokens checks that a generated TypeScript Document with a missing-token
// limit parses as a parse from scratch of its text does, on the incremental insertion corpus, in both
// units, at several limits: the same tree and error after each edit from one input of a case to the
// next and back to the first, after a parse without an edit, and in an observed parse. A parse whose
// options change the limit is rejected. Each parse also counts the compute units of the engine's
// Document along the same edits. The Documents resume the runs of their repetitions, so that the
// parses after an edit resume them. The typed Documents insert as the typed parses from scratch do,
// with the units of the engine's Document.
func TestTSDocumentsInsertMissingTokens(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	node := tooltest.RequireNode(t)
	cases := codegentest.IncrementalInsertionCorpus()
	patched := 0
	for _, m := range tsDocumentInsertionMismatches(t, node, cases, everyRun(&patched), true, false) {
		t.Error(m)
	}
	if patched == 0 {
		t.Fatal("no Document of the corpus records runs")
	}
	// A typed Document converts the result of its Node parse without charging, so it counts the
	// units of the engine's Document too.
	for _, m := range tsDocumentInsertionMismatches(t, node, append(codegentest.TypedInsertionCases(), cases...), everyRun(&patched), true, true) {
		t.Error("typed: " + m)
	}
}
