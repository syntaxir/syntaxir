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

// hullScript makes the tuples and lists of cases.json on the TypeScript library (fxTuple of tsCompose and
// Actx.newList, over the printed method actionHull), prints the range of each, and the units that
// actionHullLogical counts over the elements again, in order. The input of the parser is input.txt.
const hullScript = `import { readFileSync } from "node:fs";
import * as p from "./parser.ts";
` + tsCompose + `const CONTROLLED = false;
const P = Object.create(p.Parser.prototype);
P.control = null;
P.memControl = null;
P.units = 0;
P.unitsCheck = Infinity;
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
const used = (): number => (CONTROLLED ? P.units : 0);
// pushed returns the elements that list makes of vs.
const pushed = (vs: p.Value[]): (p.Node | null)[] => {
  const base = P.kidStack.length;
  for (const v of vs) fxPush(c, "list", v);
  return P.kidStack.splice(base);
};
const cases: [string, string[]][] = JSON.parse(readFileSync("cases.json", "utf8"));
console.log(JSON.stringify(cases.map(([kind, elems]) => {
  try {
    const vs = elems.map((e) => values[e]!);
    const n = (kind === "tuple" ? fxTuple(c, ...vs) : c.newList(pushed(vs))) as p.Node;
    const rng = n.start + "-" + n.end;
    const before = used();
    P.actionHullLogical(c, kind === "tuple", n.children);
    return rng + " " + (used() - before);
  } catch (x) {
    return x instanceof FxError ? "error: " + x.msg : "exception: " + String(x);
  }
})));
`

func hullKey(kind string, elems []string) string { return kind + ":" + strings.Join(elems, ",") }

// runTSHulls runs the hull table on the TypeScript library after mutate changed its source, counting
// its units if controlled, and returns the result of each case by its kind and elements.
func runTSHulls(t *testing.T, controlled bool, mutate func(string) string) map[string]string {
	t.Helper()
	node := tooltest.RequireNode(t)
	src := codegentest.TSTableBundle(t)
	if mutate != nil {
		src = mutate(src)
	}
	var cases [][2]any
	for _, kind := range conform.HullKinds {
		for _, c := range conform.HullCases {
			cases = append(cases, [2]any{kind, append([]string{}, c.Elems...)})
		}
	}
	cj, _ := json.Marshal(cases)
	script := strings.NewReplacer("const CONTROLLED = false;", "const CONTROLLED = "+strconv.FormatBool(controlled)+";",
		"RULE_START", strconv.Itoa(conform.StructRuleStart), "RULE_END", strconv.Itoa(conform.StructRuleEnd),
	).Replace(hullScript)
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
		got[hullKey(c[0].(string), c[1].([]string))] = results[i]
	}
	return got
}

// TestValueHullTableTypeScript runs the hull table (conform.HullCases) on the TypeScript library.
func TestValueHullTableTypeScript(t *testing.T) {
	t.Parallel()
	for _, controlled := range []bool{false, true} {
		got := runTSHulls(t, controlled, nil)
		if err := conform.RunHulls("typescript controlled="+strconv.FormatBool(controlled), controlled, func(kind string, elems []string) (string, int) {
			return splitHull(got[hullKey(kind, elems)])
		}); err != nil {
			t.Error(err)
		}
	}
}

// splitHull splits the result of a case into the range and the units.
func splitHull(r string) (string, int) {
	rng, units, _ := strings.Cut(r, " ")
	n, _ := strconv.Atoi(units)
	return rng, n
}

// TestPrintedHullMutantsTypeScript is the proof that the hull table holds the printed code of the
// range of a list or tuple in the TypeScript library: with the source of each mutant, it fails.
func TestPrintedHullMutantsTypeScript(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	if testing.Short() {
		t.Skip("runs the tests on mutated sources")
	}
	t.Parallel()
	mutants := []struct{ name, sig, old, new string }{
		{"an element of a hull counts no unit", "actionHullLogical(c: Actx", "this.unitsSized(kids.length);", ""},
		{"an element that adds no range widens the range", "actionHull(c: Actx", "if (has) {", "if (has || true) {"},
		{"the start of a hull is the latest", "actionHull(c: Actx", "es < lo", "es > lo"},
		{"the end of a hull is the earliest", "actionHull(c: Actx", "ee > hi", "ee < hi"},
		{"a later element replaces the range of a hull", "actionHull(c: Actx", "first || es < lo", "true"},
		{"an empty hull is not at the start of the rule", "actionHull(c: Actx", "let lo = rs;", "let lo = 0;"},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			got := runTSHulls(t, true, func(src string) string {
				out, ok := mutateTSFunc(src, m.sig, m.old, m.new, false)
				if !ok {
					t.Fatalf("the library has no %s with %q", m.sig, m.old)
				}
				return out
			})
			if conform.RunHulls(m.name, true, func(kind string, elems []string) (string, int) { return splitHull(got[hullKey(kind, elems)]) }) == nil {
				t.Error("the hull table did not notice")
			}
		})
	}
}
