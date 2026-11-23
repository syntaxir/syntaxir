package print_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// The generated TypeScript parsers report the calls of a parse with the events of the engine and the
// generated Go parsers, in the same JSON shape, and add them to profiles of the same shape. The test here
// compares them with the engine as TestEngineObservationsAgreeWithGenerated compares the generated Go
// parsers, on Parse, Recognize, a stream, the parse of a Document and its parse after an edit, in both
// position units, examined ends included.

// tsAgreeConsumer runs the generated TypeScript parsers g<i>.ts, and the stream parsers s<i>.ts of the
// cases that the file of its second argument marks, on the inputs in the file of its first argument
// and prints, for each input, unit and entry, one JSON report in the shape of agreeReport. The stream
// entry feeds the input one byte at a time and its end in an empty chunk, as the engine's stream reads
// it (oneByteReader).
// Each input is its text, the range of its middle character in code points and in bytes, and that
// character (texts in base64, as tsAgreeInput makes them), which the edit entry replaces by itself in a
// Document parsed once before. Each parse is observed by two trace functions and two profiles, given as arrays with a
// null entry that must be ignored; the same parse, observed again, has the trace filter Filter and the
// depth limit MAXDEPTH. The document entry gives the first trace function and profile, and the trace
// filter "main" with the depth limit 1, as the defaults of the Document, and the others in the call of
// its parse, which add to those defaults and replace the filter and the limit. The edit entry gives all
// of them in the call of the parse after the edit. Both report the Document's statistics of the
// observed parse.
const tsAgreeConsumer = `import { readFileSync } from "node:fs";

const inputs = JSON.parse(readFileSync(process.argv[2], "utf8"));
const streams = JSON.parse(readFileSync(process.argv[3], "utf8"));
const maxDepth = MAXDEPTH;
const untimed = (profile) => {
  const s = profile.snapshot();
  s.time = 0;
  for (const r of s.rules) { r.time = 0; r.selfTime = 0; }
  return s;
};
const message = (err) => err === null ? "<nil>" : String(err.message);
// stats holds the evaluations, memo hits and resumed elements of the last parse of the Document of a
// document or edit entry, which sets it after the parse it observes if that parse completed: it
// succeeded or ended with syntax errors. A parse that an action, a limit or another fatal error aborted
// may leave statistics of calls that did not complete.
let stats = null;
for (let i = 0; i < inputs.length; i++) {
  const m = await import("./g" + i + ".ts");
  const keepStats = (document, err) => {
    if (err !== null && !(err instanceof m.SyntaxError) && !(err instanceof m.SyntaxErrors)) return;
    const s = document.stats();
    stats = [s.evaluated, s.memoHits, s.resumedElements];
  };
  const entries = [
    ["parse", (x, defaults, call) => m.parseWithOptions(x, { ...defaults, ...call }).error, true, m],
    ["recognize", (x, defaults, call) => m.recognizeWithOptions(x, { ...defaults, ...call }), true, m],
  ];
  if (streams[i]) {
    const s = await import("./s" + i + ".ts");
    entries.push(["stream", (x, defaults, call) => {
      const { stream, error } = s.createStream({ ...defaults, ...call });
      if (error !== null) return error;
      for (let k = 0; k < x.length; k++) {
        const r = stream.feed(x.subarray(k, k + 1));
        if (r.state !== "waiting") return r.error;
      }
      return stream.feed(new Uint8Array(0), true).error;
    }, true, s]);
  }
  if (typeof m.createDocument === "function") {
    entries.push(["document", (x, defaults, call) => {
      const { document, error } = m.createDocument(x, defaults);
      if (error !== null) return error;
      const err = document.parse(call).error;
      keepStats(document, err);
      return err;
    }, false, m]);
    entries.push(["edit", (x, defaults, call, edit) => {
      const { unit, ...options } = defaults;
      const { document, error } = m.createDocument(x, { unit });
      if (error !== null) return error;
      document.parse();
      const [start, end] = unit === m.Bytes ? edit.bytes : edit.cps;
      const err = document.edit(start, end, edit.ch);
      if (err !== null) return err;
      const parseErr = document.parse({ ...options, ...call }).error;
      keepStats(document, parseErr);
      return parseErr;
    }, true, m]);
  }
  for (let j = 0; j < inputs[i].length; j++) {
    const x = new Uint8Array(Buffer.from(inputs[i][j].text, "base64"));
    const edit = { cps: inputs[i][j].cps, bytes: inputs[i][j].bytes, ch: new Uint8Array(Buffer.from(inputs[i][j].ch, "base64")) };
    for (const unit of [m.CodePoints, m.Bytes]) {
      for (const [entry, run, single, mod] of entries) {
        // A profile is of the bundle that parses: each bundle has its own library.
        const events = [], second = [], prof = new mod.Profile(), prof2 = new mod.Profile();
        const f1 = (e) => { events.push(e); }, f2 = (e) => { second.push(e); };
        stats = null;
        const err = single
          ? run(x, { unit, trace: [f1, null, f2], profile: [prof, null, prof2] }, {}, edit)
          : run(x, { unit, trace: f1, profile: prof }, { trace: [f2], profile: prof2 }, edit);
        const observed = stats;
        const filter = ["main", "no such rule"];
        const first = events.find((e) => e.depth === 2);
        if (first !== undefined) filter[0] = first.rule;
        const filtered = [], filteredProf = new mod.Profile();
        const f = (e) => { filtered.push(e); };
        if (single) run(x, { unit, trace: f, profile: filteredProf, traceRules: filter, traceMaxDepth: maxDepth }, {}, edit);
        else run(x, { unit, trace: f, profile: filteredProf, traceRules: ["main"], traceMaxDepth: 1 }, { traceRules: filter, traceMaxDepth: maxDepth }, edit);
        console.log(JSON.stringify({
          Case: i, Input: j, Bytes: unit === m.Bytes, Entry: entry,
          Events: events, Profile: untimed(prof), Second: second, Profile2: untimed(prof2),
          Filter: filter, Filtered: filtered, FilteredProfile: untimed(filteredProf), Err: message(err), Stats: observed,
        }));
      }
    }
  }
}
`

// tsAgreeInput is an input of tsAgreeConsumer: its text, and the range of its middle character in code
// points and in bytes with that character (middleChar), which the edit entry replaces.
type tsAgreeInput struct {
	Text  []byte `json:"text"`
	CPs   [2]int `json:"cps"`
	Bytes [2]int `json:"bytes"`
	Ch    []byte `json:"ch"`
}

// runTSAgreeProbes generates the TypeScript parsers of the cases with observability, the recognizer and
// Document where it can, and the stream parsers of those whose start rule has a #stream repetition,
// changed as codegentest.TSLibPatchEnv says, and returns the reports of tsAgreeConsumer.
func runTSAgreeProbes(t *testing.T, node string, cases []codegentest.Case) []agreeReport {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, content []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", []byte(`{"type": "module"}`+"\n"))
	var inputs [][]tsAgreeInput
	streams := make([]bool, len(cases))
	for i, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		code, err := codegentest.GenerateTSBundle(g, codegentest.MutantOptions(common.GenOptions{Start: "main", Recognize: true, Observability: true, Document: true}))
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		write(fmt.Sprintf("g%d.ts", i), codegentest.PatchTSLib(t, code))
		// A TypeScript stream does not parse with Documents: its parser is generated apart.
		code, err = codegentest.GenerateTSBundle(g, codegentest.MutantOptions(common.GenOptions{Start: "main", Observability: true, Stream: true}))
		switch {
		case err == nil:
			write(fmt.Sprintf("s%d.ts", i), codegentest.PatchTSLib(t, code))
			streams[i] = true
		case !strings.Contains(err.Error(), "needs a #stream repetition"):
			t.Fatalf("%s: %v", c.Name, err)
		}
		var in []tsAgreeInput
		for _, s := range c.Inputs {
			pos, end, ch := middleChar(s, input.CodePoints)
			bpos, bend, _ := middleChar(s, input.Bytes)
			in = append(in, tsAgreeInput{Text: []byte(s), CPs: [2]int{pos, end}, Bytes: [2]int{bpos, bend}, Ch: []byte(ch)})
		}
		inputs = append(inputs, in)
	}
	data, _ := json.Marshal(inputs)
	write("inputs.json", data)
	data, _ = json.Marshal(streams)
	write("streams.json", data)
	write("main.mjs", []byte(strings.ReplaceAll(tsAgreeConsumer, "MAXDEPTH", fmt.Sprint(agreeMaxDepth))))
	cmd := exec.Command(node, "--stack-size=65500", "main.mjs", "inputs.json", "streams.json")
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, codegentest.Clip(stderr.String(), 0))
	}
	var reports []agreeReport
	for _, l := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		var r agreeReport
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("node: malformed report %q: %v", codegentest.Clip(l, 0), err)
		}
		reports = append(reports, r)
	}
	return reports
}

// tsAgreementMismatches checks each report of the TypeScript parsers against the same parse on each
// engine backend with callsAgree and profilesAgree, or with sameCallExamined for the parse after an edit,
// the options of both producers with optionsHonored and the Document statistics with statsCounted, and
// returns the differences and what it compared, by entry.
func tsAgreementMismatches(t *testing.T, cases []codegentest.Case, reports []agreeReport) (map[string][]string, map[string]*agreementStats) {
	t.Helper()
	diffs, stats := map[string][]string{}, map[string]*agreementStats{}
	progs := map[int]*engine.Program{}
	for _, r := range reports {
		c := cases[r.Case]
		in := c.Inputs[r.Input]
		prog := progs[r.Case]
		if prog == nil {
			prog = mustCompile(t, c.Src)
			progs[r.Case] = prog
		}
		unit := input.CodePoints
		if r.Bytes {
			unit = input.Bytes
		}
		st := stats[r.Entry]
		if st == nil {
			st = &agreementStats{}
			stats[r.Entry] = st
		}
		st.reports++
		where := fmt.Sprintf("%s %q bytes=%v %s", c.Name, in, r.Bytes, r.Entry)
		if msg := optionsHonored("typescript", r.Events, r.Profile, r.Second, r.Profile2, r.Filter, r.Filtered, r.FilteredProfile); msg != "" {
			diffs[r.Entry] = append(diffs[r.Entry], where+": "+msg)
		}
		if msg := statsCounted("typescript", r); msg != "" {
			diffs[r.Entry] = append(diffs[r.Entry], where+": "+msg)
		}
		for _, b := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
			events, snap, err := engineObservation(t, prog, r, in, b, nil, 0)
			where := fmt.Sprintf("%s %v", where, b)
			if (err == nil) != (r.Err == "<nil>") {
				diffs[r.Entry] = append(diffs[r.Entry], fmt.Sprintf("%s: engine error %v, typescript %s", where, err, r.Err))
				continue
			}
			filtered, _, _ := engineObservation(t, prog, r, in, b, r.Filter, agreeMaxDepth)
			st.filtered += len(filtered)
			if len(filtered) > 0 && len(filtered) < len(events) {
				st.partial++
			}
			if r.Entry == "edit" {
				n, d := sameCallExamined(events, r.Events, in, unit)
				st.compared += n
				for _, msg := range d {
					diffs[r.Entry] = append(diffs[r.Entry], where+": "+msg)
				}
				if swapped := swapGrowthExamined(events, in, unit); swapped != nil && len(d) == 0 {
					st.swaps++
					if _, d := sameCallExamined(swapped, r.Events, in, unit); len(d) == 0 {
						diffs[r.Entry] = append(diffs[r.Entry], where+": swapped examined ends of growth steps not told apart")
					}
				}
				continue
			}
			aside, msg := callsAgree(events, r.Events, in, unit)
			if msg == "" {
				msg = profilesAgree(snap, r.Profile, aside)
			}
			if msg == "" {
				if _, m := callsAgree(filtered, r.Filtered, in, unit); m != "" {
					msg = "filtered: " + m
				}
			}
			if msg != "" {
				diffs[r.Entry] = append(diffs[r.Entry], where+": "+msg)
				continue
			}
			st.compared += aside.compared
			st.paired += aside.paired
			st.unpaired += aside.unpaired
			for _, e := range aside.a {
				if e.Kind == instrument.TraceEnter {
					st.engineOnly++
				}
			}
			for _, e := range aside.b {
				if e.Kind == instrument.TraceEnter {
					st.generatedOnly++
				}
			}
		}
	}
	return diffs, stats
}

// TestTSObservationsAgreeWithEngine checks the events and profiles of the generated TypeScript parsers
// against those of every engine backend (callsAgree, profilesAgree) on Parse, Recognize, a stream and
// the parse of a Document, and the events of the parse of a Document after an edit (sameCallExamined), in both units,
// and that they honor several trace functions and profiles, given as arrays or as the defaults of a
// Document and its parse, and the trace filter and depth limit, given in a parse or replacing the
// defaults of a Document (optionsHonored), and that the Document statistics of both equal their profiles
// (statsCounted).
func TestTSObservationsAgreeWithEngine(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript consumers")
	}
	node := tooltest.RequireNode(t)
	codegentest.NeedsBudgetControls(t)
	cases := examinedCases(t)
	diffs, stats := tsAgreementMismatches(t, cases, runTSAgreeProbes(t, node, cases))
	for _, entry := range []string{"parse", "recognize", "stream", "document", "edit"} {
		st := stats[entry]
		if st == nil || st.compared == 0 {
			t.Errorf("no event of %s compared", entry)
			st = &agreementStats{}
		}
		if entry == "edit" {
			if st.swaps == 0 {
				t.Error("no edit observation had growth steps to swap")
			}
			t.Logf("edit: %d observations with swapped growth steps told apart", st.swaps)
		}
		t.Logf("%s: %d reports, %d events compared; %d skipped events paired, %d unpaired; %d calls of the engine only, %d of the TypeScript parser only",
			entry, st.reports, st.compared, st.paired, st.unpaired, st.engineOnly, st.generatedOnly)
		t.Logf("%s: %d filtered engine events; %d parses filtered in part", entry, st.filtered, st.partial)
		if st.partial == 0 {
			t.Errorf("no parse of %s was filtered in part", entry)
		}
		for i, msg := range diffs[entry] {
			if i == 10 {
				t.Errorf("%d more differences of %s", len(diffs[entry])-i, entry)
				break
			}
			t.Error(msg)
		}
	}
}
