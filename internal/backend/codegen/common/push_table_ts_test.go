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

// pushScript runs the calls of calls.json on the printed methods actionPush and actionPushItems of the TypeScript
// library and prints, in order, the canonical result of each (conform.MemberCanon), the number of elements it
// pushed and the units it counted. The input of the parser is input.txt.
const pushScript = `import { readFileSync } from "node:fs";
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
const values: Record<string, p.Value> = {
  "nil": null, "1": 1, '"ab"': "ab", "true": true, "[1, 2]": fxList(c, 1, 2), "[]": fxList(c),
  "func": ((x: p.Value) => x) as unknown as p.Value,
};
const canon = (v: p.Value): string => {
  if (v === null || v === undefined) return "nil";
  if (typeof v === "number" || typeof v === "bigint" || typeof v === "boolean") return String(v);
  if (typeof v === "string") return JSON.stringify(v);
  if (v instanceof p.Node) {
    if (v.type === p.typeBox) return "box " + canon(v.box);
    const elems = v.children.map((k) => canon(p.unboxNode(k)));
    if (v.type === p.typeListValue) return "[" + elems.join(", ") + "]";
    return v.type;
  }
  return "not a value: " + String(v);
};
const used = (): number => (CONTROLLED ? P.units : 0);
const calls: [string, string][] = JSON.parse(readFileSync("calls.json", "utf8"));
console.log(JSON.stringify(calls.map(([fn, v]) => {
  const base = P.kidStack.length;
  const before = used();
  const e = fn === "push" ? P.actionPush(c, "list", values[v]!) : P.actionPushItems(c, "concat", values[v]!);
  const units = used() - before;
  const pushed: (p.Node | null)[] = P.kidStack.slice(base);
  P.kidStack.length = base;
  let r: string;
  if (e !== null) r = "error: " + (e as string);
  else if (fn === "push") r = pushed.length === 1 ? canon(pushed[0]) : "nil";
  else r = canon(c.listNode(pushed));
  return r + "\t" + pushed.length + "\t" + units;
})));
`

// runTSPushes runs the push table on the TypeScript library after mutate changed its source, counting
// its units if controlled, and returns the result of each call by its call and value.
func runTSPushes(t *testing.T, controlled bool, mutate func(string) string) map[[2]string]string {
	t.Helper()
	node := tooltest.RequireNode(t)
	src := codegentest.TSTableBundle(t)
	if mutate != nil {
		src = mutate(src)
	}
	var calls [][2]string
	for _, c := range conform.PushCases {
		calls = append(calls, [2]string{c.Call, c.Value})
	}
	cj, _ := json.Marshal(calls)
	dir := t.TempDir()
	script := strings.Replace(pushScript, "const CONTROLLED = false;", "const CONTROLLED = "+strconv.FormatBool(controlled)+";", 1)
	for name, content := range map[string]string{"package.json": `{"type": "module"}`, "parser.ts": src, "main.ts": script,
		"calls.json": string(cj), "input.txt": conform.BuiltinInput} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := runTSTable(t, node, dir)
	var results []string
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(results) != len(calls) {
		t.Fatalf("%d results for %d calls", len(results), len(calls))
	}
	got := map[[2]string]string{}
	for i, c := range calls {
		got[c] = results[i]
	}
	return got
}

// pushResult splits the result of a call into the canonical value, the elements pushed and the units.
func pushResult(got map[[2]string]string, call, v string) (string, int, int) {
	parts := strings.Split(got[[2]string{call, v}], "\t")
	if len(parts) != 3 {
		return got[[2]string{call, v}], -1, -1
	}
	pushed, _ := strconv.Atoi(parts[1])
	units, _ := strconv.Atoi(parts[2])
	return parts[0], pushed, units
}

// checkTSPushes runs the push table with the results of the TypeScript library. A push counts no compute
// units: the code of an action counts the elements that concat builds itself (UNITSOF, units.items), as in
// the engine, so the table's units, which a target whose pushItems counted them would charge, are not
// checked; controlled, every call must have counted none.
func checkTSPushes(name string, controlled bool, got map[[2]string]string) error {
	var counted []string
	err := conform.RunPushes(name, false, func(call, v string) (string, int, int) {
		r, pushed, units := pushResult(got, call, v)
		if controlled && units != 0 {
			counted = append(counted, fmt.Sprintf("%s: %s(%s) counted %d units, want none", name, call, v, units))
		}
		return r, pushed, units
	})
	if err == nil && len(counted) > 0 {
		err = fmt.Errorf("push table:\n%s", strings.Join(counted, "\n"))
	}
	return err
}

// TestValuePushTableTypeScript runs the push table (conform.PushCases) on the TypeScript library.
func TestValuePushTableTypeScript(t *testing.T) {
	t.Parallel()
	for _, controlled := range []bool{false, true} {
		if err := checkTSPushes("typescript controlled="+strconv.FormatBool(controlled), controlled, runTSPushes(t, controlled, nil)); err != nil {
			t.Error(err)
		}
	}
}

// TestPrintedPushMutantsTypeScript is the proof that the push table holds the printed code of the compositions
// push and pushItems of the TypeScript library: with the source of each mutant, it fails.
func TestPrintedPushMutantsTypeScript(t *testing.T) {
	mutanttest.SkipUnlessChanged(t, mutanttest.Pipeline...)
	if testing.Short() {
		t.Skip("runs the tests on mutated sources")
	}
	t.Parallel()
	mutants := []struct{ name, sig, old, new string }{
		{"a bool is not an element", "actionPush(c: Actx", "k === 1 || k === 2 || k === 3", "k === 1 || k === 2"},
		{"a function is an element", "actionPush(c: Actx", "return this.actionError(11, v, fn, 0, 0);", "return null;"},
		{"nil pushes no element", "actionPush(c: Actx", "this.kidStack.push(null);", ""},
		{"concat pushes no elements", "actionPushItems(c: Actx", "c.pushAll(items);", ""},
		{"concat takes a value that is not a list", "actionPushItems(c: Actx", "if (ok) {", "if (true) {"},
		{"concat counts the elements it pushes", "pushAll(items:", "this.p.kidStack.push(it);", "this.p.kidStack.push(it); this.p.units++;"},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			got := runTSPushes(t, true, func(src string) string {
				out, ok := mutateTSFunc(src, m.sig, m.old, m.new, false)
				if !ok {
					t.Fatalf("the library has no %s with %q", m.sig, m.old)
				}
				return out
			})
			if checkTSPushes(m.name, true, got) == nil {
				t.Error("the push table did not notice")
			}
		})
	}
}
