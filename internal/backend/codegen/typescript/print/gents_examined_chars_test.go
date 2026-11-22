package print_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// tsExaminedCharsConsumer runs the parse, the recognizer and, in a typed module, the typed parse of
// each module on its input, given as bytes, in its unit, and prints one JSON report per run with the
// exit of atom at 0.
const tsExaminedCharsConsumer = `
const out = [];
for (const [p, b64, bytes] of cases) {
  const entries = [["parse", (s, o) => p.parseWithOptions(s, o)], ["recognize", (s, o) => p.recognizeWithOptions(s, o)]];
  if (typeof p.parseValueWithOptions === "function") entries.push(["value", (s, o) => p.parseValueWithOptions(s, o)]);
  const input = Uint8Array.from(Buffer.from(b64, "base64"));
  for (const [entry, run] of entries) {
    let exit = null;
    run(input, { unit: bytes ? p.Bytes : p.CodePoints, trace: (e) => {
      if (e.kind === "exit" && e.rule === "atom" && e.pos === 0) exit = { matched: e.matched, examined: e.examined };
    } });
    out.push(JSON.stringify({ entry, exit }));
  }
}
process.stdout.write(out.join("\n") + "\n");
`

// TestGeneratedTSExaminedCharacterComparisons checks the examined end of the call of atom at 0 in each
// case of codegentest.ExaminedChars, as the parse, the recognizer and the typed parse of the generated
// TypeScript parser report it, without and with limit controls.
func TestGeneratedTSExaminedCharacterComparisons(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	chars := codegentest.ExaminedChars()
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type": "module"}`+"\n")
	var main strings.Builder
	var runs []string // the case and form of each module, in order
	var list []string
	for i, c := range chars {
		g, err := parseTest(c.Src())
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		for _, limits := range []bool{false, true} {
			o := common.GenOptions{Start: "main", Types: true, Recognize: true, Observability: true, NoLimitControls: !limits}
			code, err := codegentest.GenerateTSBundle(g, o)
			if err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
			mod := fmt.Sprintf("m%d_%v", i, limits)
			write(mod+".ts", string(code))
			fmt.Fprintf(&main, "import * as %s from \"./%[1]s.ts\";\n", mod)
			list = append(list, fmt.Sprintf("[%s, %q, %v]", mod, base64.StdEncoding.EncodeToString([]byte(c.Input)), c.Bytes))
			runs = append(runs, fmt.Sprintf("%s (limit controls %v)", c.Name, limits))
		}
	}
	write("main.mjs", main.String()+"const cases = ["+strings.Join(list, ", ")+"];\n"+tsExaminedCharsConsumer)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "main.mjs")
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, stderr.String())
	}
	type exit struct {
		Matched  bool
		Examined int
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 3*len(runs) {
		t.Fatalf("got %d reports, want %d\n%s", len(lines), 3*len(runs), out)
	}
	for k, l := range lines {
		var r struct {
			Entry string
			Exit  *exit
		}
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("malformed report %q: %v", l, err)
		}
		c, run := chars[k/6], runs[k/3]
		switch {
		case r.Exit == nil:
			t.Errorf("%s, %s: no exit of atom at 0", run, r.Entry)
		case r.Exit.Matched != c.Matched || r.Exit.Examined != c.Examined:
			t.Errorf("%s, %s: atom@0 matched %v, examined %d; want matched %v, examined %d", run, r.Entry, r.Exit.Matched, r.Exit.Examined, c.Matched, c.Examined)
		}
	}
}
