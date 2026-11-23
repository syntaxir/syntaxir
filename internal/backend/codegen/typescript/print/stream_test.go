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
// a parser printed as with -stream. drive runs the resumable body of the rule name (bodyGens) on a
// StreamParser whose input holds the whole input but whose n grows by step units at each suspension
// (the whole input at once for step 0), and returns its result and position, its compute units and
// the number of suspensions. batch runs the batch body of the rule (Rule.body) on a Parser of the
// whole input and returns the same.
const tsStreamDriver = `
const show = (v: R, p: Parser) => (v === undefined ? "fail" : v === null ? "null" : v.toString()) + " @" + p.pos;
export function batch(name: string, input: string | Uint8Array, unit: Unit): { result: string; units: number } {
  const start = rules.find(r => r.name === name)!;
  const q = new Parser(G, input, unit, nseen, 10000);
  return { result: show(start.body(q, 0), q), units: q.units };
}
export function drive(name: string, input: string | Uint8Array, unit: Unit, step: number): { result: string; units: number; yields: number } {
  const start = rules.find(r => r.name === name)!;
  const p = new StreamParser(G, input, unit, nseen, 10000);
  p.bodyGens = bodyGens;
  const total = p.n;
  if (step <= 0) step = total;
  p.n = Math.min(step, total);
  p.final = p.n === total;
  const gen = bodyGens[start.id]!(p, 0);
  let yields = 0;
  for (let r = gen.next(); ; r = gen.next()) {
    if (r.done === true) return { result: show(r.value, p), units: p.units, yields };
    if (r.value <= 0 || p.final) throw new Error("a demand of " + r.value + " units with the input at " + p.n + " of " + total + (p.final ? ", final" : ""));
    yields++;
    p.n = Math.min(p.n + step, total);
    p.final = p.n === total;
  }
}

// streamLines runs the rule main on each input in code points (0) and in bytes (1): fed whole, and fed
// in chunks of each size up to max units, which must end as fed whole, with the result and position
// of the batch parse. A line reports the result, the units of the batch parse and of the stream, and
// the suspensions of the stream fed one unit at a time, or the run that differs.
export function streamLines(inputs: (string | Uint8Array)[], max: number): string {
  const out: string[] = [];
  for (const input of inputs) {
    for (const unit of [CodePoints, Bytes]) {
      const b = batch("main", input, unit);
      const whole = drive("main", input, unit, 0);
      if (whole.yields !== 0) out.push("SUSPENDED " + JSON.stringify([String(input), unit, whole]));
      if (whole.result !== b.result) out.push("MISMATCH batch " + JSON.stringify([String(input), unit, b, whole]));
      for (let step = 1; step <= max; step++) {
        const r = drive("main", input, unit, step);
        if (r.result !== whole.result || r.units !== whole.units) out.push("MISMATCH " + JSON.stringify([String(input), unit, step, r, whole]));
      }
      out.push(JSON.stringify(String(input)) + " " + unit + ": " + b.result + " units " + b.units + "/" + whole.units + ", suspended " + drive("main", input, unit, 1).yields);
    }
  }
  return out.join("\n");
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

// tsStreamPlainGrammar has plain rules only, which the stream calls without the call protocol.
const tsStreamPlainGrammar = `@start
rule main = word ("," word)* !.
rule word = [a-z€]+ "é!"?
`

// TestStreamPlainResumable runs the resumable functions of a stream whose rules are plain on inputs fed
// in every chunk size, in code points and in bytes, and checks that each run suspends where the input
// it reads has not arrived and ends with the result, position and compute units of the stream fed the
// whole input in one chunk, which suspends nowhere and ends with the result and position of the batch
// parse. Without memoized rules, the stream counts the units of the batch parse.
func TestStreamPlainResumable(t *testing.T) {
	t.Parallel()
	g, err := parseTest(tsStreamPlainGrammar)
	if err != nil {
		t.Fatal(err)
	}
	script := `import { streamLines } from "./parser.ts";
console.log(streamLines(["ab,cd", "ab,cdé!,e", "ab,,", "x", "", "ab,cdé", "a€b,€", new Uint8Array([0x61, 0x2c, 0x62, 0xc3]), new Uint8Array([0x61, 0xe2, 0x82, 0x2c])], 12));
`
	out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main"}, tsStreamBundle)
	// Each line is the result of an input in a unit (0 code points, 1 bytes), the units of the batch
	// parse and of the stream, and the suspensions of the stream fed one unit at a time.
	want := `"ab,cd" 0: ((["a" "b"] nil) [("," (["c" "d"] nil))]) @5 units 7/7, suspended 4
"ab,cd" 1: ((["a" "b"] nil) [("," (["c" "d"] nil))]) @5 units 7/7, suspended 4
"ab,cdé!,e" 0: ((["a" "b"] nil) [("," (["c" "d"] "é!")) ("," (["e"] nil))]) @9 units 12/12, suspended 8
"ab,cdé!,e" 1: ((["a" "b"] nil) [("," (["c" "d"] "é!")) ("," (["e"] nil))]) @10 units 13/13, suspended 9
"ab,," 0: fail @2 units 6/6, suspended 3
"ab,," 1: fail @2 units 6/6, suspended 3
"x" 0: ((["x"] nil) []) @1 units 2/2, suspended 0
"x" 1: ((["x"] nil) []) @1 units 2/2, suspended 0
"" 0: fail @0 units 1/1, suspended 0
"" 1: fail @0 units 1/1, suspended 0
"ab,cdé" 0: fail @5 units 8/8, suspended 5
"ab,cdé" 1: fail @5 units 9/9, suspended 6
"a€b,€" 0: ((["a" "€" "b"] nil) [("," (["€"] nil))]) @5 units 7/7, suspended 4
"a€b,€" 1: ((["a" "€" "b"] nil) [("," (["€"] nil))]) @9 units 11/11, suspended 8
"97,44,98,195" 0: fail @3 units 6/6, suspended 3
"97,44,98,195" 1: fail @3 units 6/6, suspended 3
"97,226,130,44" 0: fail @1 units 3/3, suspended 2
"97,226,130,44" 1: fail @1 units 3/3, suspended 3
`
	if out != want {
		t.Fatalf("the stream differs from its parse of the whole input or from the batch parse, or suspends elsewhere:\n%s\nwant:\n%s", out, want)
	}
}

// tsStreamCallGrammar has rules that the stream calls through the call protocol: a left-recursive
// leader (list), whose seed grows, rules with captures (pair, item), and a rule that two alternatives
// call at one position (pair), whose second call reuses the memoized result.
const tsStreamCallGrammar = `@start
rule main = list !.
rule list = list "," item / item
rule item = p:pair ";" { $p } / pair / word
rule pair = k:word "=" v:word
rule word = [a-z€]+ "é!"?
`

// TestStreamCallResumable runs the resumable functions of a stream whose rules are called through the
// call protocol, memoized and left-recursive, on inputs fed in every chunk size, in code points and in
// bytes, and checks that each run ends with the result, position and compute units of the stream fed
// the whole input in one chunk, which suspends nowhere and ends with the result and position of the
// batch parse. The stream memoizes each rule on its first call at a position, where the batch parse
// defers the memoization to the second call, so its units are fewer where a rule is called again.
func TestStreamCallResumable(t *testing.T) {
	t.Parallel()
	g, err := parseTest(tsStreamCallGrammar)
	if err != nil {
		t.Fatal(err)
	}
	script := `import { streamLines } from "./parser.ts";
console.log(streamLines(["a", "a,b", "a=b", "a=b;,c,d=e", "a=b,c=é", "ab=cé!;,€=x", "a,", "a=", "a=b;;", ""], 16));
`
	out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "main"}, tsStreamBundle)
	// Each line is the result of an input in a unit (0 code points, 1 bytes), the units of the batch
	// parse and of the stream, and the suspensions of the stream fed one unit at a time.
	want := `"a" 0: (((["a"] nil))) @1 units 19/13, suspended 0
"a" 1: (((["a"] nil))) @1 units 19/13, suspended 0
"a,b" 0: ((((["a"] nil)) "," ((["b"] nil)))) @3 units 29/23, suspended 2
"a,b" 1: ((((["a"] nil)) "," ((["b"] nil)))) @3 units 29/23, suspended 2
"a=b" 0: ((((["a"] nil) "=" (["b"] nil)))) @3 units 21/14, suspended 2
"a=b" 1: ((((["a"] nil) "=" (["b"] nil)))) @3 units 21/14, suspended 2
"a=b;,c,d=e" 0: (((((((["a"] nil) "=" (["b"] nil)) ";")) "," ((["c"] nil))) "," (((["d"] nil) "=" (["e"] nil))))) @10 units 44/35, suspended 9
"a=b;,c,d=e" 1: (((((((["a"] nil) "=" (["b"] nil)) ";")) "," ((["c"] nil))) "," (((["d"] nil) "=" (["e"] nil))))) @10 units 44/35, suspended 9
"a=b,c=é" 0: fail @5 units 34/27, suspended 6
"a=b,c=é" 1: fail @5 units 34/27, suspended 7
"ab=cé!;,€=x" 0: ((((((["a" "b"] nil) "=" (["c"] "é!")) ";")) "," (((["€"] nil) "=" (["x"] nil))))) @11 units 43/28, suspended 10
"ab=cé!;,€=x" 1: ((((((["a" "b"] nil) "=" (["c"] "é!")) ";")) "," (((["€"] nil) "=" (["x"] nil))))) @14 units 49/31, suspended 13
"a," 0: fail @1 units 26/20, suspended 1
"a," 1: fail @1 units 26/20, suspended 1
"a=" 0: fail @1 units 24/16, suspended 1
"a=" 1: fail @1 units 24/16, suspended 1
"a=b;;" 0: fail @4 units 22/15, suspended 4
"a=b;;" 1: fail @4 units 22/15, suspended 4
"" 0: fail @0 units 9/8, suspended 0
"" 1: fail @0 units 9/8, suspended 0
`
	if out != want {
		t.Fatalf("the stream differs from its parse of the whole input or from the batch parse, or suspends elsewhere:\n%s\nwant:\n%s", out, want)
	}
}

// TestStreamPrintedTypes checks the parsers printed as with -stream for the grammars of the stream tests,
// with the driver, under the strictest checks of tsc.
func TestStreamPrintedTypes(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs tsc")
	}
	tsc := tooltest.RequireTSC(t)
	for _, src := range []string{tsStreamPlainGrammar, tsStreamCallGrammar} {
		g, err := parseTest(src)
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
			t.Fatalf("tsc: %v\n%s\nthe grammar:\n%s", err, out, src)
		}
	}
}
