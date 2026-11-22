//go:build sxir_long

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
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/examinedref"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/instrument"
)

// tsExaminedConsumer runs every entry of every generated module on its inputs, given as bytes, in both
// units, and prints one JSON report per parse: the exit and skipped events, and whether the error ended
// the parse before the start call returned (one that is not a syntax error, such as a runtime error of
// a user action).
const tsExaminedConsumer = `
const inputs = JSON.parse(await (await import("node:fs/promises")).readFile(process.argv[2], "utf8"));
const out = [];
for (let i = 0; i < modules.length; i++) {
  const p = modules[i];
  const entries = [["parse", (s, o) => p.parseWithOptions(s, o).error], ["recognize", (s, o) => p.recognizeWithOptions(s, o)]];
  if (typeof p.parseValueWithOptions === "function") entries.push(["value", (s, o) => p.parseValueWithOptions(s, o).error]);
  for (let j = 0; j < inputs[i].length; j++) {
    const bytes = Uint8Array.from(Buffer.from(inputs[i][j], "base64"));
    for (const unit of [p.CodePoints, p.Bytes]) for (const [entry, run] of entries) {
      const events = [];
      const err = run(bytes, { unit, trace: (e) => { if (e.kind !== "enter") events.push(e); } });
      const aborted = err !== null && err !== undefined && err.name !== "SyntaxError" && err.name !== "SyntaxErrors";
      out.push(JSON.stringify({ Case: i, Input: j, Bytes: unit === p.Bytes, Entry: entry, Events: events, Err: String(err), Aborted: aborted }));
    }
  }
}
process.stdout.write(out.join("\n") + "\n");
`

// TestTSObservedExaminedEndsAreExact checks the calls that the generated TypeScript parsers report on
// every route of an observed parse (parseWithOptions, recognizeWithOptions and, for typed modules,
// parseValueWithOptions) against the independent reference (examinedref) of the same input and unit:
// every exit is a call of the reference with the same match, end and examined end, every skipped call
// fails in the reference, and the start call returns what the reference's does.
func TestTSObservedExaminedEndsAreExact(t *testing.T) {
	t.Parallel()
	node := tooltest.RequireNode(t)
	var cases []codegentest.Case
	for _, c := range append(codegentest.Representative(codegentest.GenCorpus(t)), codegentest.ExaminedShapes()...) {
		// Generated parsers do not insert tokens, and refuse the grammars that would.
		if !strings.Contains(c.Src, "#missing") && len(c.Inputs) > 0 {
			cases = append(cases, c)
		}
	}
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type": "module"}`+"\n")
	var imports strings.Builder
	var inputs [][]string
	for i, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		// The library mutants of examinedref's TestEngineExaminedMutants come through
		// codegentest.TSLibPatchEnv.
		o := codegentest.MutantOptions(common.GenOptions{Start: "main", Types: true, Recognize: true, Observability: true})
		code, err := codegentest.GenerateTSBundle(g, o)
		if err != nil {
			// A grammar whose values have no static types has no typed entry.
			o.Types = false
			if code, err = codegentest.GenerateTSBundle(g, o); err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
		}
		write(fmt.Sprintf("m%d.ts", i), string(codegentest.PatchTSLib(t, code)))
		fmt.Fprintf(&imports, "import * as m%d from \"./m%d.ts\";\n", i, i)
		var in []string
		for _, s := range c.Inputs {
			in = append(in, base64.StdEncoding.EncodeToString([]byte(s)))
		}
		inputs = append(inputs, in)
	}
	var mods []string
	for i := range cases {
		mods = append(mods, fmt.Sprintf("m%d", i))
	}
	write("consumer.mjs", imports.String()+"const modules = ["+strings.Join(mods, ", ")+"];\n"+tsExaminedConsumer)
	data, _ := json.Marshal(inputs)
	write("inputs.json", string(data))
	cmd := exec.Command(node, "consumer.mjs", "inputs.json")
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, stderr.String())
	}

	type report struct {
		Case, Input int
		Bytes       bool
		Entry       string
		Events      []instrument.TraceEvent
		Err         string
		Aborted     bool
	}
	type refRun struct {
		res   examinedref.Result
		exits []examinedref.Exit
		err   error
	}
	type key struct {
		c, j  int
		bytes bool
	}
	refs := map[key]refRun{}
	compared := map[string]int{}
	shown := map[string]int{}
	for _, l := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		var r report
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("malformed report %q: %v", l, err)
		}
		c := cases[r.Case]
		in := c.Inputs[r.Input]
		k := key{r.Case, r.Input, r.Bytes}
		ref, ok := refs[k]
		if !ok {
			g, err := parseTest(c.Src)
			if err != nil {
				t.Fatal(err)
			}
			o := examinedref.Options{Unit: input.CodePoints}
			if r.Bytes {
				o.Unit = input.Bytes
			}
			ref.res, ref.exits, ref.err = examinedref.EvaluateWith(g, "main", in, o)
			refs[k] = ref
		}
		if ref.err != nil {
			t.Fatalf("%s %q: reference: %v", c.Name, in, ref.err)
		}
		var calls []examinedref.Observed
		for _, e := range r.Events {
			if o, ok := examinedref.ObservedOf(e); ok {
				calls = append(calls, o)
			}
		}
		diffs := examinedref.CompareParse(ref.res, ref.exits, "main", calls, r.Aborted, tsEntryActions(r.Entry))
		compared[r.Entry] += len(calls)
		for _, d := range diffs {
			if r.Aborted {
				d += fmt.Sprintf(" (aborted by %s)", r.Err)
			}
			if shown[r.Entry]++; shown[r.Entry] <= 20 {
				t.Errorf("%s %q bytes=%v %s: %s", c.Name, in, r.Bytes, r.Entry, d)
			}
		}
	}
	for entry, n := range shown {
		if n > 20 {
			t.Errorf("%d more differences of %s", n-20, entry)
		}
	}
	t.Logf("calls compared by entry: %v", compared)
	for _, entry := range []string{"parse", "recognize", "value"} {
		if compared[entry] == 0 {
			t.Errorf("no call of %s compared", entry)
		}
	}
}

// tsEntryActions tells which Value Actions an entry of the probe runs: recognition runs only those
// that predicates read, and the other entries build values and run them all.
func tsEntryActions(entry string) examinedref.Actions {
	if entry == "recognize" {
		return examinedref.ReadActions
	}
	return examinedref.EveryAction
}
