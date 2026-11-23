package print_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// tsStreamDriver is a minimal driver of the resumable functions of a stream, appended to the bundle of
// a parser printed as with -stream: it runs the resumable body of the rule name (bodyGens) on a
// StreamParser whose input holds the whole input but whose n grows by step units at each suspension,
// and its batch body (Rule.body) on a Parser of the whole input, and returns both
// results with the position and the compute units after them, and the number of suspensions.
const tsStreamDriver = `
export function drive(name: string, input: string | Uint8Array, unit: Unit, step: number): { batch: string; stream: string; yields: number } {
  const start = rules.find(r => r.name === name)!;
  const show = (v: R, p: Parser) => (v === undefined ? "fail" : v === null ? "null" : v.toString()) + " @" + p.pos + " units " + p.units;
  const q = new Parser(G, input, unit, nseen, 10000);
  const batch = show(start.body(q, 0), q);
  const p = new StreamParser(G, input, unit, nseen, 10000);
  p.bodyGens = bodyGens;
  const total = p.n;
  p.n = Math.min(step, total);
  p.final = p.n === total;
  const gen = bodyGens[start.id]!(p, 0);
  let yields = 0;
  for (let r = gen.next(); ; r = gen.next()) {
    if (r.done === true) return { batch, stream: show(r.value, p), yields };
    if (r.value <= 0 || p.final) throw new Error("a demand of " + r.value + " units with the input at " + p.n + " of " + total + (p.final ? ", final" : ""));
    yields++;
    p.n = Math.min(p.n + step, total);
    p.final = p.n === total;
  }
}
`

// tsStreamBundle returns the bundle of the parser of g printed as with -stream, with the driver.
func tsStreamBundle(g *ast.Grammar, opts common.GenOptions) ([]byte, error) {
	b, err := codegentest.GenerateTSStreamBundle(g, opts)
	if err != nil {
		return nil, err
	}
	return append(b, tsStreamDriver...), nil
}

// TestStreamPlainResumable runs the resumable functions of a stream whose rules are plain on inputs fed
// in every chunk size, in code points and in bytes, and checks that each run suspends where the input
// it reads has not arrived and ends with the result, position and compute units of the batch parse
// of the whole input; feeding the input in one chunk suspends nowhere.
func TestStreamPlainResumable(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`@start
rule main = word ("," word)* !.
rule word = [a-z€]+ "é!"?
`)
	if err != nil {
		t.Fatal(err)
	}
	script := `import { drive, CodePoints, Bytes } from "./parser.ts";
const inputs = ["ab,cd", "ab,cdé!,e", "ab,,", "x", "", "ab,cdé", "a€b,€", new Uint8Array([0x61, 0x2c, 0x62, 0xc3]), new Uint8Array([0x61, 0xe2, 0x82, 0x2c])];
const out = [];
for (const input of inputs) {
  for (const unit of [CodePoints, Bytes]) {
    let n = 0;
    for (let step = 1; step <= 12; step++) {
      const r = drive("main", input, unit, step);
      if (r.batch !== r.stream) out.push("MISMATCH " + JSON.stringify([String(input), unit, step, r]));
      if (step === 12 && r.yields !== 0) out.push("SUSPENDED " + JSON.stringify([String(input), unit, r]));
      if (step === 1) n = r.yields;
    }
    out.push(JSON.stringify(String(input)) + " " + unit + ": " + drive("main", input, unit, 1).batch + ", suspended " + n);
  }
}
console.log(out.join("\n"));
`
	out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main"}, tsStreamBundle)
	// Each line is the batch result of an input in a unit (0 code points, 1 bytes) and the suspensions
	// of the stream fed one unit at a time.
	want := `"ab,cd" 0: ((["a" "b"] nil) [("," (["c" "d"] nil))]) @5 units 7, suspended 4
"ab,cd" 1: ((["a" "b"] nil) [("," (["c" "d"] nil))]) @5 units 7, suspended 4
"ab,cdé!,e" 0: ((["a" "b"] nil) [("," (["c" "d"] "é!")) ("," (["e"] nil))]) @9 units 12, suspended 8
"ab,cdé!,e" 1: ((["a" "b"] nil) [("," (["c" "d"] "é!")) ("," (["e"] nil))]) @10 units 13, suspended 9
"ab,," 0: fail @2 units 6, suspended 3
"ab,," 1: fail @2 units 6, suspended 3
"x" 0: ((["x"] nil) []) @1 units 2, suspended 0
"x" 1: ((["x"] nil) []) @1 units 2, suspended 0
"" 0: fail @0 units 1, suspended 0
"" 1: fail @0 units 1, suspended 0
"ab,cdé" 0: fail @5 units 8, suspended 5
"ab,cdé" 1: fail @5 units 9, suspended 6
"a€b,€" 0: ((["a" "€" "b"] nil) [("," (["€"] nil))]) @5 units 7, suspended 4
"a€b,€" 1: ((["a" "€" "b"] nil) [("," (["€"] nil))]) @9 units 11, suspended 8
"97,44,98,195" 0: fail @3 units 6, suspended 3
"97,44,98,195" 1: fail @3 units 6, suspended 3
"97,226,130,44" 0: fail @1 units 3, suspended 2
"97,226,130,44" 1: fail @1 units 3, suspended 3
`
	if out != want {
		t.Fatalf("the stream differs from the batch parse, or suspends elsewhere:\n%s\nwant:\n%s", out, want)
	}
}

// TestStreamPrintedTypes checks the parser printed as with -stream, with the driver, under the
// strictest checks of tsc.
func TestStreamPrintedTypes(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs tsc")
	}
	tsc := tooltest.RequireTSC(t)
	g, err := parseTest(`@start
rule main = word ("," word)* !.
rule word = [a-z€]+ "é!"?
`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := tsStreamBundle(g, common.GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "parser.ts"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(tsc, append(append([]string{"--noEmit"}, tsStrictFlags...), "parser.ts")...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc: %v\n%s", err, out)
	}
}
