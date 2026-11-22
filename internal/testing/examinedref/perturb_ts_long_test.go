//go:build sxir_long

package examinedref_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/internal/testing/examinedref"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// The generated TypeScript parsers are held to the examined ends by the same perturbation as the engine
// and the generated Go parsers: the Go test drives them through one Node.js process, which parses each
// input it is sent and returns the trace, so that the perturbations and the lockstep comparison are those
// of examinedref and nothing is defined twice. The generated TypeScript parsers read their whole input
// before they parse; they have no stream route, so the stream high-water check has nothing to read here.

// tsPerturbServer reads requests, one JSON object per line, from its standard input and writes, for each,
// one line with the trace of the parse it asks for, or the error of an edit that the Document refuses.
// A request names the parser g<case>.ts, the entry, the unit and the text (base64). The entries are
// parse, recognize, document (the first parse of a Document) and edit: a Document of base (base64),
// parsed once, then edited by replacing from the change point to its end (end, in the unit) with the
// text and parsed again, the parse traced.
const tsPerturbServer = `import { createInterface } from "node:readline";

const modules = new Map();
const parser = async (i) => {
  if (!modules.has(i)) modules.set(i, await import("./g" + i + ".ts"));
  return modules.get(i);
};
const bytes = (s) => new Uint8Array(Buffer.from(s ?? "", "base64"));
for await (const line of createInterface({ input: process.stdin, crlfDelay: Infinity })) {
  const q = JSON.parse(line);
  const m = await parser(q.case);
  const unit = q.bytes ? m.Bytes : m.CodePoints;
  const events = [];
  const trace = (e) => { events.push(e); };
  const x = bytes(q.text);
  let refused = null;
  switch (q.entry) {
    case "parse": m.parseWithOptions(x, { unit, trace }); break;
    case "recognize": m.recognizeWithOptions(x, { unit, trace }); break;
    case "document": {
      const { document, error } = m.createDocument(x, { unit });
      if (error === null) document.parse({ trace });
      break;
    }
    case "edit": {
      const { document, error } = m.createDocument(bytes(q.base), { unit });
      if (error !== null) break;
      document.parse();
      const err = document.edit(q.change, q.end, x);
      if (err !== null) { refused = String(err.message); break; }
      document.parse({ trace });
      break;
    }
    default: throw new Error("unknown entry " + q.entry);
  }
  process.stdout.write(JSON.stringify({ events, refused }) + "\n");
}
`

// tsPerturbRequest is a request to tsPerturbServer; tsPerturbReply its answer.
type tsPerturbRequest struct {
	Case   int    `json:"case"`
	Entry  string `json:"entry"`
	Bytes  bool   `json:"bytes"`
	Text   []byte `json:"text"`
	Base   []byte `json:"base"`
	Change int    `json:"change"`
	End    int    `json:"end"`
}

type tsPerturbReply struct {
	Events  []instrument.TraceEvent
	Refused *string
}

// tsPerturbEntries are the routes of the generated TypeScript parsers that the test perturbs.
var tsPerturbEntries = []string{"parse", "recognize", "document", "edit"}

// TestTSPerturbation checks the examined ends that the generated TypeScript parsers report on each route,
// in both units, by perturbation (examinedref.Perturb). The reparse of a Document after an edit
// (examinedref.PerturbAt) is checked as the edit that replaces the input from the change point on: the
// original's own text and each perturbation's are put there into a Document of the original, so that the
// edits differ in their text alone and keep the same results of the first parse.
func TestTSPerturbation(t *testing.T) {
	t.Parallel()
	node := tooltest.RequireNode(t)
	cases := generatedCases(t)
	dir := t.TempDir()
	write := func(name string, content []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", []byte(`{"type": "module"}`+"\n"))
	write("main.mjs", []byte(tsPerturbServer))
	type grammarFacts struct {
		alpha    string
		dep      examinedref.Dependence
		document bool
	}
	facts := make([]grammarFacts, len(cases))
	for i, c := range cases {
		g, err := compiletest.Parse(c.Src) // marks every rule @start, as the generator tests do
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		// The library mutants of TestEngineExaminedMutants come through codegentest.TSLibPatchEnv.
		code, err := codegentest.GenerateTSBundle(g, codegentest.MutantOptions(common.GenOptions{Start: "main", Recognize: true, Observability: true, Document: true}))
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		code = codegentest.PatchTSLib(t, code)
		write(fmt.Sprintf("g%d.ts", i), code)
		cycles, err := examinedref.LeftCycles(g, false)
		if err != nil {
			t.Fatalf("%s: rule facts: %v", c.Name, err)
		}
		facts[i] = grammarFacts{alpha: examinedref.GrammarAlphabet(g),
			dep:      examinedref.Dependence{Variables: examinedref.ReadsVariables(g), Cycles: cycles},
			document: strings.Contains(string(code), "export function createDocument(")}
	}

	cmd := exec.Command(node, "--stack-size=65500", "main.mjs")
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stdin.Close()
		if err := cmd.Wait(); err != nil {
			t.Errorf("node: %v\n%s", err, codegentest.Clip(stderr.String(), 0))
		}
	}()
	enc, replies := json.NewEncoder(stdin), bufio.NewReader(stdout)
	ask := func(q tsPerturbRequest) tsPerturbReply {
		if err := enc.Encode(q); err != nil {
			t.Fatalf("node: %v\n%s", err, codegentest.Clip(stderr.String(), 0))
		}
		line, err := replies.ReadBytes('\n')
		if err != nil && err != io.EOF || len(line) == 0 {
			t.Fatalf("node: no reply (%v)\n%s", err, codegentest.Clip(stderr.String(), 0))
		}
		var r tsPerturbReply
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("node: malformed reply %q: %v", codegentest.Clip(string(line), 0), err)
		}
		return r
	}
	events := func(r tsPerturbReply) []examinedref.Event {
		out := make([]examinedref.Event, len(r.Events))
		for i, e := range r.Events {
			out[i] = traceEventOf(e)
		}
		return out
	}

	for _, u := range perturbUnits {
		bytes := u.unit == input.Bytes
		totals, skipped, refusals := map[string]*examinedref.Report{}, map[string]int{}, map[string]int{}
		for _, entry := range tsPerturbEntries {
			totals[entry] = &examinedref.Report{}
		}
		for i, c := range cases {
			for _, entry := range tsPerturbEntries {
				if (entry == "document" || entry == "edit") && !facts[i].document {
					continue
				}
				for _, in := range c.Inputs {
					units := examinedref.Units(in, bytes)
					if len(units) > perturbMaxUnits {
						skipped[entry]++
						continue
					}
					var invalid []string
					check := func(q examinedref.Perturbation) {
						if err := validPerturbation(in, bytes, q); err != nil {
							invalid = append(invalid, err.Error())
						}
					}
					var r examinedref.Report
					if entry == "edit" {
						r = examinedref.PerturbAt(func(change int, s string) ([]examinedref.Event, bool) {
							// Perturbations keeps the units before the change point, so the rest of s replaces the
							// rest of the original.
							rest := strings.Join(examinedref.Units(s, bytes)[change:], "")
							reply := ask(tsPerturbRequest{Case: i, Entry: entry, Bytes: bytes, Text: []byte(rest), Base: []byte(in), Change: change, End: len(units)})
							if reply.Refused != nil {
								refusals[*reply.Refused]++
								return nil, false
							}
							return events(reply), true
						}, in, bytes, facts[i].dep, facts[i].alpha, check)
					} else {
						r = examinedref.Perturb(func(s string) []examinedref.Event {
							return events(ask(tsPerturbRequest{Case: i, Entry: entry, Bytes: bytes, Text: []byte(s)}))
						}, in, bytes, facts[i].dep, facts[i].alpha, check)
					}
					prefix := fmt.Sprintf("typescript %s %s: %s", u.name, entry, c.Name)
					reportPerturb(t, prefix+" (perturbation)", invalid, 3)
					reportPerturb(t, prefix, r.Violations, 5)
					totals[entry].Add(r)
				}
			}
		}
		for _, entry := range tsPerturbEntries {
			r := totals[entry]
			t.Logf("%s %s: %d inputs (%d too long), %d perturbations (%d dropped, %d refused): %d identical, %d diverged in a call (%d exits checked, %d exempt, %d growing), %d diverged outside calls",
				u.name, entry, r.Parses, skipped[entry], r.Perturbed, r.Dropped, r.Refused, r.Identical, r.Diverged, r.Checked, r.Exempt, r.Growing, r.Outside)
			if r.Diverged == 0 || r.Identical == 0 || r.Checked == 0 {
				t.Errorf("typescript %s %s: the perturbations never diverged or never kept the trace", u.name, entry)
			}
		}
		for msg, n := range refusals {
			t.Logf("%s edit: %d edits refused: %s", u.name, n, msg)
		}
	}
}
