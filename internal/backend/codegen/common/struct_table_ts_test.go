package common_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/mutanttest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// structScript makes the structs of cases.json on the TypeScript library (fxStruct of tsCompose, over the printed
// methods actionCover and actionMade) and prints the range of each, and whether the context recorded it, in
// order. The input of the parser is input.txt.
const structScript = `import { readFileSync } from "node:fs";
import * as p from "./parser.ts";
` + tsCompose + `const CONTROLLED = false;
const P = Object.create(p.Parser.prototype);
P.control = CONTROLLED ? new p.BudgetControl({ signal: null, profiles: [], budget: 0 } as unknown as p.ResolvedParseOptions) : null;
P.memControl = P.control !== null && P.control.memoryLimited() ? P.control : null;
P.created = [];
P.kidStack = [];
P.src = readFileSync("input.txt", "utf8");
P.offs = null;
P.unit = p.Bytes;
const c = new p.Actx(P);
c.start = RULE_START;
c.end = RULE_END;
const text = new p.Node("string", "", 2, 4, "xyz", p.noNodes, p.noFields, false, true);
const values: Record<string, p.Value> = {
  "nil": null, "1": 1, '"ab"': "ab", "text": text,
  "node": new p.Node("tuple", "", 2, 5, "", [text, text], p.noFields, false, false),
  "late": new p.Node("string", "", 7, 9, "q", p.noNodes, p.noFields, false, true),
};
const cases: string[][] = JSON.parse(readFileSync("cases.json", "utf8"));
console.log(JSON.stringify(cases.map((fields) => {
  try {
    const before = P.created.length;
    const kv: p.Value[] = [];
    fields.forEach((f, i) => kv.push("F" + i, values[f]!));
    const n = fxStruct(c, "S", ...kv) as p.Node;
    const recorded = P.created.length === before + 1 && P.created[before] === n;
    return n.start + "-" + n.end + (recorded ? "RECORDED" : "");
  } catch (x) {
    return x instanceof FxError ? "error: " + x.msg : "exception: " + String(x);
  }
})));
`

// runTSStructs runs the struct table on the TypeScript library after mutate changed its source, with a
// budget control if controlled, and returns the result of each case by its fields.
func runTSStructs(t *testing.T, controlled bool, mutate func(string) string) map[string]string {
	t.Helper()
	node := tooltest.RequireNode(t)
	src := codegentest.TSTableBundle(t)
	if mutate != nil {
		src = mutate(src)
	}
	var cases [][]string
	for _, c := range conform.StructCases {
		cases = append(cases, append([]string{}, c.Fields...))
	}
	cj, _ := json.Marshal(cases)
	script := strings.NewReplacer("const CONTROLLED = false;", "const CONTROLLED = "+strconv.FormatBool(controlled)+";",
		"RULE_START", strconv.Itoa(conform.StructRuleStart), "RULE_END", strconv.Itoa(conform.StructRuleEnd),
		"RECORDED", conform.StructRecorded).Replace(structScript)
	dir := t.TempDir()
	for name, content := range map[string]string{"package.json": `{"type": "module"}`, "parser.ts": src, "main.ts": script,
		"cases.json": string(cj), "input.txt": conform.BuiltinInput} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := runTSTable(t, node, dir)
	var results []string
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(results) != len(cases) {
		t.Fatalf("%d results for %d cases", len(results), len(cases))
	}
	got := map[string]string{}
	for i, c := range cases {
		got[strings.Join(c, ",")] = results[i]
	}
	return got
}

// TestValueStructTableTypeScript runs the struct table (conform.StructCases) on the TypeScript library.
func TestValueStructTableTypeScript(t *testing.T) {
	t.Parallel()
	for _, controlled := range []bool{false, true} {
		got := runTSStructs(t, controlled, nil)
		if err := conform.RunStructs("typescript controlled="+strconv.FormatBool(controlled), func(fields []string) string { return got[strings.Join(fields, ",")] }); err != nil {
			t.Error(err)
		}
	}
}

// TestPrintedStructMutantsTypeScript is the proof that the struct table holds the printed code of the
// range of a struct in the TypeScript library: with the source of each mutant, it fails.
func TestPrintedStructMutantsTypeScript(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	if testing.Short() {
		t.Skip("runs the tests on mutated sources")
	}
	t.Parallel()
	mutants := []struct{ name, sig, old, new string }{
		{"a node field value does not cover", "actionCover(n: Node", "if (!ok) {", "if (true) {"},
		{"the start of the range is the latest", "actionCover(n: Node", "cs < ns", "cs > ns"},
		{"the end of the range is the earliest", "actionCover(n: Node", "ce > ne", "ce < ne"},
		{"a later node replaces the range", "actionCover(n: Node", "first || cs < ns", "true"},
		{"a struct without a node keeps no range", "actionMade(c: Actx", "if (first) {", "if (false) {"},
		{"a struct is not recorded", "actionMade(c: Actx", "this.created.push(n);", ""},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			got := runTSStructs(t, false, func(src string) string {
				out, ok := mutateTSFunc(src, m.sig, m.old, m.new, false)
				if !ok {
					t.Fatalf("the library has no %s with %q", m.sig, m.old)
				}
				return out
			})
			if conform.RunStructs(m.name, func(fields []string) string { return got[strings.Join(fields, ",")] }) == nil {
				t.Error("the struct table did not notice")
			}
		})
	}
}
