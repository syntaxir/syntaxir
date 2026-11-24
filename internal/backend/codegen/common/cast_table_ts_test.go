package common_test

import (
	"encoding/json"
	"fmt"
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

// castScript checks the cases of cases.json on the TypeScript library (Parser.fieldConforms, printed from the
// compositions conforms, valueConforms, elemsConform and tupleConform) and prints, for each, whether the
// value is of the type and the units the check counted, in order. A case holds a type and a value of the cast
// table (conform.CastType and conform.CastValue).
const castScript = `import { readFileSync } from "node:fs";
import * as p from "./parser.ts";
` + tsCompose + `const CONTROLLED = false;
const P = Object.create(p.Parser.prototype);
P.control = null;
P.memControl = null;
P.units = 0;
P.unitsCheck = Infinity;
P.created = [];
P.kidStack = [];
P.g = { structFields: new Map() };
const c = new p.Actx(P);
type V = { Kind: string; Name: string; Elems: V[] | null };
const text = () => new p.Node("string", "", 2, 4, "xyz", p.noNodes, p.noFields, false, true);
const build = (v: V): p.Value => {
  const elems = (): p.Value[] => (v.Elems ?? []).map((e) => (e.Kind === "nilnode" ? null : build(e)));
  switch (v.Kind) {
    case "nil": case "nilnode": return null;
    case "int": return 1;
    case "string": return "s";
    case "bool": return true;
    case "text": return text();
    case "tuple": return new p.Node("tuple", "", 2, 5, "", [text(), text()], p.noFields, false, false);
    case "rep": return new p.Node("list", "", 2, 5, "", [text(), text()], p.noFields, false, false);
    case "err": return new p.Node("error", "", 2, 4, "x", p.noNodes, p.noFields, false, true);
    case "struct": return fxStruct(c, v.Name);
    case "box": fxPush(c, "list", 1); return P.kidStack.pop()! as p.Value;
    case "list": return fxList(c, ...elems());
    case "tupleValue": return fxTuple(c, ...elems());
  }
  throw new Error("no value " + v.Kind);
};
type T = { Kind: number; Name: string; Elem: T | null; Alts: T[] | null };
const field = (t: T): p.FieldType => ({
  kind: String.fromCharCode(t.Kind), name: t.Name, elem: t.Elem === null ? null : field(t.Elem),
  alts: (t.Alts ?? []).map(field), text: "",
});
const cases: [T, V][] = JSON.parse(readFileSync("cases.json", "utf8"));
console.log(JSON.stringify(cases.map(([t, v]) => {
  const val = build(v);
  const used = (): number => (CONTROLLED ? P.units : 0);
  const before = used();
  const ok = P.fieldConforms(field(t), val);
  return [ok, used() - before];
})));
`

// runTSCasts runs the cast table on the TypeScript library after mutate changed its source, counting
// its units if controlled, and returns the result of each case that the TypeScript library runs, in order.
func runTSCasts(t *testing.T, controlled bool, mutate func(string) string) [][2]any {
	t.Helper()
	node := tooltest.RequireNode(t)
	src := codegentest.TSTableBundle(t)
	if mutate != nil {
		src = mutate(src)
	}
	var cases [][2]any
	for _, c := range conform.CastCases {
		if c.Only != "" && c.Only != "typescript" {
			continue
		}
		cases = append(cases, [2]any{conform.ParseCastType(c.Type), conform.ParseCastValue(c.Value)})
	}
	cj, _ := json.Marshal(cases)
	script := strings.Replace(castScript, "const CONTROLLED = false;", "const CONTROLLED = "+strconv.FormatBool(controlled)+";", 1)
	dir := t.TempDir()
	for name, content := range map[string]string{"package.json": `{"type": "module"}`, "parser.ts": src, "main.ts": script,
		"cases.json": string(cj)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := runTSTable(t, node, dir)
	var results [][2]any
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(results) != len(cases) {
		t.Fatalf("%d results for %d cases", len(results), len(cases))
	}
	return results
}

// checkTSCasts runs the cast table with the results of the TypeScript library, which come in the order of
// the cases that it runs. A cast counts no compute units (the elements it examines are no event), so
// controlled, every check must have counted none.
func checkTSCasts(controlled bool, results [][2]any) error {
	i := 0
	if err := conform.RunCasts("typescript", false, func(*conform.CastType, conform.CastValue) (bool, int) {
		r := results[i]
		i++
		return r[0].(bool), 0
	}); err != nil || !controlled {
		return err
	}
	var counted []string
	i = 0
	for _, c := range conform.CastCases {
		if c.Only != "" && c.Only != "typescript" {
			continue
		}
		if n := int(results[i][1].(float64)); n != 0 {
			counted = append(counted, fmt.Sprintf("typescript: %s is %s: counted %d units, want none", c.Value, c.Type, n))
		}
		i++
	}
	if len(counted) > 0 {
		return fmt.Errorf("cast table:\n%s", strings.Join(counted, "\n"))
	}
	return nil
}

// TestValueCastTableTypeScript runs the cast table (conform.CastCases) on the TypeScript library.
func TestValueCastTableTypeScript(t *testing.T) {
	t.Parallel()
	for _, controlled := range []bool{false, true} {
		got := runTSCasts(t, controlled, nil)
		if err := checkTSCasts(controlled, got); err != nil {
			t.Errorf("controlled=%v: %v", controlled, err)
		}
	}
}

// TestPrintedCastMutantsTypeScript is the proof that the cast table holds the printed code of the cast
// check of the TypeScript library: with the source of each mutant, it fails.
func TestPrintedCastMutantsTypeScript(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	if testing.Short() {
		t.Skip("runs the tests on mutated sources")
	}
	t.Parallel()
	mutants := []struct{ name, sig, old, new string }{
		{"the check of a list counts units", "elemsConform(t: FieldType", "): boolean {", "): boolean { this.units++;"},
		{"the check of a tuple counts units", "tupleConform(t: FieldType", "): boolean {", "): boolean { this.units++;"},
		{"a tuple of another length conforms", "tupleConform(t: FieldType", "if (count !== t.alts.length) {", "if (count > t.alts.length) {"},
		{"a list type takes any value", "fieldConforms(t: FieldType", "if (!(shape === 6)) {", "if (false) {"},
		{"a tuple type takes a list value", "fieldConforms(t: FieldType", "if (!(shape === 5)) {", "if (!(shape === 5 || shape === 6)) {"},
		{"an optional type does not take nil", "fieldConforms(t: FieldType", "return kx === 0 || this.fieldConforms(t.elem!, x);", "return this.fieldConforms(t.elem!, x);"},
		{"a join takes the value of no alternative", "fieldConforms(t: FieldType", "if (this.fieldConforms(t.alts[i]!, x)) {", "if (!this.fieldConforms(t.alts[i]!, x)) {"},
		{"a named type takes any value", "fieldConforms(t: FieldType", "return (n.type === t.name);", "return true;"},
		{"a node whose value is a list has any elements", "valueConforms(t: FieldType", "if (!(shape === 2)) {", "if (false) {"},
		{"a box is not opened by a cast", "fieldConforms(t: FieldType", "const x = unboxValue(v);", "const x = v;"},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			got := runTSCasts(t, true, func(src string) string {
				out, ok := mutateTSFunc(src, m.sig, m.old, m.new, false)
				if !ok {
					t.Fatalf("the library has no %s with %q", m.sig, m.old)
				}
				return out
			})
			if checkTSCasts(true, got) == nil {
				t.Error("the cast table did not notice")
			}
		})
	}
}
