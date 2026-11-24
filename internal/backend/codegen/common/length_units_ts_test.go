package common_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// lengthScript calls the printed method actionLength of the TypeScript library on each string of
// strings.json, in bytes and in code points, under a budget control, and prints for each call the length
// and the compute units it counted, in order.
const lengthScript = `import { readFileSync } from "node:fs";
import * as p from "./parser.ts";
const strs: string[] = JSON.parse(readFileSync("strings.json", "utf8"));
const out: [number, number][] = [];
for (const codePoints of [false, true]) {
  for (const s of strs) {
    const P = Object.create(p.Parser.prototype);
    P.control = new p.BudgetControl({ signal: null, profiles: [], budget: 0 } as unknown as p.ResolvedParseOptions);
    P.memControl = P.control.memoryLimited() ? P.control : null;
    P.units = 0;
    P.unitsCheck = Infinity;
    P.unit = codePoints ? p.CodePoints : p.Bytes;
    const [v, e] = P.actionLength(s);
    if (e !== null) throw new Error("len fails: " + String(e));
    out.push([Number(v), P.units]);
  }
}
console.log(JSON.stringify(out));
`

// runTSLengths runs the length table on the TypeScript library after mutate changed its source.
func runTSLengths(t *testing.T, mutate func(string) string) func(s string, codePoints bool) (int, uint64) {
	t.Helper()
	node := tooltest.RequireNode(t)
	src := codegentest.TSTableBundle(t)
	if mutate != nil {
		src = mutate(src)
	}
	sj, _ := json.Marshal(conform.LengthStrings)
	dir := t.TempDir()
	for name, content := range map[string]string{"package.json": `{"type": "module"}`, "parser.ts": src, "main.ts": lengthScript, "strings.json": string(sj)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := runTSTable(t, node, dir)
	var results [][2]uint64
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	n := len(conform.LengthStrings)
	if len(results) != 2*n {
		t.Fatalf("%d results for %d calls", len(results), 2*n)
	}
	return func(s string, codePoints bool) (int, uint64) {
		k := slices.Index(conform.LengthStrings, s)
		if codePoints {
			k += n
		}
		return int(results[k][0]), results[k][1]
	}
}

// TestValueLengthUnitsTypeScript runs the length table (conform.RunLengths) on the TypeScript library.
func TestValueLengthUnitsTypeScript(t *testing.T) {
	t.Parallel()
	if err := conform.RunLengths("typescript", runTSLengths(t, nil)); err != nil {
		t.Error(err)
	}
}

// TestValueLengthUnitsDetectsBrokenTypeScript is the negative control: with the count of len left out of the
// library, the length table fails.
func TestValueLengthUnitsDetectsBrokenTypeScript(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	t.Parallel()
	call := runTSLengths(t, func(src string) string {
		out, ok := mutateTSFunc(src, "unitLen(s: string)", "if (this.control !== null) this.unitsSized(n);", "", false)
		if !ok {
			t.Fatal("the library has no unitLen that counts its units")
		}
		return out
	})
	if conform.RunLengths("len counts no units", call) == nil {
		t.Error("the length table did not notice")
	}
}
