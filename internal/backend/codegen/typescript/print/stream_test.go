package print_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// tsStreamDriver is a minimal driver of the resumable functions of a stream, appended to the bundle of
// a parser printed as with -stream. drive runs the resumable body of the rule name (bodyGens) on a
// StreamParser to which it appends the input step units at a time, at the start and at each
// suspension (the whole input at once for step 0), and returns its result and position, its compute
// units and the number of suspensions. In code points a unit is the bytes of a character, which the
// parser decodes once the bytes after it tell where it ends. batch runs the batch body of the rule
// (Rule.body) on a Parser of the whole input and returns the same.
const tsStreamDriver = `
const show = (v: R, p: Parser) => (v === undefined ? "fail" : v === null ? "null" : v.toString()) + " @" + p.pos;
export function batch(name: string, input: string | Uint8Array, unit: Unit): { result: string; units: number } {
  const start = rules.find(r => r.name === name)!;
  const q = new Parser(G, input, unit, nseen, 10000);
  return { result: show(start.body(q, 0), q), units: q.units };
}
export function drive(name: string, input: string | Uint8Array, unit: Unit, step: number): { result: string; units: number; yields: number } {
  const start = rules.find(r => r.name === name)!;
  const bytes = typeof input === "string" ? encodeUTF8(input) : input;
  // ends holds the end of each unit in the bytes.
  const ends = [0];
  for (let i = 0; i < bytes.length; ends.push(i)) {
    if (unit === Bytes) i++;
    else {
      decodeRune(bytes, i, bytes.length);
      i += decodedSize;
    }
  }
  const total = ends.length - 1;
  const p = new StreamParser(G, unit, nseen, 10000, null, start.id);
  p.bodyGens = bodyGens;
  if (step <= 0) step = total;
  let at = 0;
  const feed = () => {
    const to = Math.min(at + step, total);
    p.append(bytes.subarray(ends[at]!, ends[to]!));
    at = to;
    if (at === total) p.end();
  };
  feed();
  const gen = bodyGens[start.id]!(p, 0);
  let yields = 0;
  for (let r = gen.next(); ; r = gen.next()) {
    if (r.done === true) return { result: show(r.value, p), units: p.units, yields };
    if (r.value <= 0 || p.final) throw new Error("a demand of " + r.value + " units with the input at " + p.n + " of " + total + (p.final ? ", final" : ""));
    yields++;
    feed();
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

// tsStreamBundle returns the bundle of the parser of g printed as with -stream, with the driver. The
// grammars of the tests that drive the rule main have a start rule top for the stream, whose #stream
// repetition repeats main.
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
@start(default)
rule top = main* #stream
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
	out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "top"}, tsStreamBundle)
	// Each line is the result of an input in a unit (0 code points, 1 bytes), the units of the batch
	// parse and of the stream, and the suspensions of the stream fed one unit at a time. In code
	// points the bytes 226, 130 decode as two characters once the byte after them arrives, so the
	// stream suspends for each of them as it does in bytes.
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
"97,226,130,44" 0: fail @1 units 3/3, suspended 3
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
@start(default)
rule top = main* #stream
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
	out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "top"}, tsStreamBundle)
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

// tsStreamOperatorsGrammar has an Operators Rule whose skip, operands and operators read the input,
// which the stream calls as a plain rule.
const tsStreamOperatorsGrammar = `@start
rule main = expr ws !.
rule expr = #operators {
    skip ws
    operand num
    operand "(" e:expr ws ")" => $e
    level { infix left "+" / "-" }
    level { infix right "**" / "*" }
    level { prefix "-" }
    level { postfix "!" }
}
rule num = [0-9]+ "é"?
rule ws = " "*
@start(default)
rule top = main* #stream
`

// TestStreamOperatorsResumable runs the resumable functions of a stream with an Operators Rule on inputs
// fed in every chunk size, in code points and in bytes, and checks them as TestStreamCallResumable does.
func TestStreamOperatorsResumable(t *testing.T) {
	t.Parallel()
	g, err := parseTest(tsStreamOperatorsGrammar)
	if err != nil {
		t.Fatal(err)
	}
	script := `import { streamLines } from "./parser.ts";
console.log(streamLines(["1", "1+2", "1 + 2*3", "-1!", "2**3**4", "(1+2)*3", " ( 1é + -2 ) ! ** 3 ", "1+", "1*(2", "(", "1 2", ""], 20));
`
	out := codegentest.RunTSGrammarScriptBundle(t, g, script, common.GenOptions{Start: "top"}, tsStreamBundle)
	// Each line is the result of an input in a unit (0 code points, 1 bytes), the units of the batch
	// parse and of the stream, and the suspensions of the stream fed one unit at a time.
	want := `"1" 0: ((["1"] nil) []) @1 units 6/6, suspended 0
"1" 1: ((["1"] nil) []) @1 units 6/6, suspended 0
"1+2" 0: (((["1"] nil) "+" (["2"] nil)) []) @3 units 12/12, suspended 2
"1+2" 1: (((["1"] nil) "+" (["2"] nil)) []) @3 units 12/12, suspended 2
"1 + 2*3" 0: (((["1"] nil) "+" ((["2"] nil) "*" (["3"] nil))) []) @7 units 20/20, suspended 6
"1 + 2*3" 1: (((["1"] nil) "+" ((["2"] nil) "*" (["3"] nil))) []) @7 units 20/20, suspended 6
"-1!" 0: (("-" ((["1"] nil) "!")) []) @3 units 11/11, suspended 2
"-1!" 1: (("-" ((["1"] nil) "!")) []) @3 units 11/11, suspended 2
"2**3**4" 0: (((["2"] nil) "**" ((["3"] nil) "**" (["4"] nil))) []) @7 units 20/20, suspended 6
"2**3**4" 1: (((["2"] nil) "**" ((["3"] nil) "**" (["4"] nil))) []) @7 units 20/20, suspended 6
"(1+2)*3" 0: ((((["1"] nil) "+" (["2"] nil)) "*" (["3"] nil)) []) @7 units 25/25, suspended 6
"(1+2)*3" 1: ((((["1"] nil) "+" (["2"] nil)) "*" (["3"] nil)) []) @7 units 25/25, suspended 6
" ( 1é + -2 ) ! ** 3 " 0: (((((["1"] "é") "+" ("-" (["2"] nil))) "!") "**" (["3"] nil)) [" "]) @20 units 46/46, suspended 19
" ( 1é + -2 ) ! ** 3 " 1: (((((["1"] "é") "+" ("-" (["2"] nil))) "!") "**" (["3"] nil)) [" "]) @21 units 47/47, suspended 20
"1+" 0: fail @1 units 10/10, suspended 1
"1+" 1: fail @1 units 10/10, suspended 1
"1*(2" 0: fail @1 units 17/17, suspended 3
"1*(2" 1: fail @1 units 17/17, suspended 3
"(" 0: fail @0 units 7/7, suspended 0
"(" 1: fail @0 units 7/7, suspended 0
"1 2" 0: fail @2 units 9/9, suspended 2
"1 2" 1: fail @2 units 9/9, suspended 2
"" 0: fail @0 units 3/3, suspended 0
"" 1: fail @0 units 3/3, suspended 0
`
	if out != want {
		t.Fatalf("the stream differs from its parse of the whole input or from the batch parse, or suspends elsewhere:\n%s\nwant:\n%s", out, want)
	}
}

// TestStreamGenFormsStayInTheStream checks that the batch parse never reads the resumable forms of an Operators
// Rule's skip and lines (ResumableOperatorsRule.skipGen, ResumableLine.mGen), which the stream sets on the
// Operators Rule that the batch parse of the same module shares. The types of the batch parse
// (OperatorsRule, OperatorsRuleLine) have no such fields, so a read casts to the resumable types; the test
// checks that each line of the printed bundle that names a field is the declaration of a resumable type, an
// assignment of the stream's initialization, or a line of a generator method of StreamParser.
func TestStreamGenFormsStayInTheStream(t *testing.T) {
	t.Parallel()
	g, err := parseTest(tsStreamOperatorsGrammar)
	if err != nil {
		t.Fatal(err)
	}
	b, err := codegentest.GenerateTSStreamBundle(g, common.GenOptions{Start: "top"})
	if err != nil {
		t.Fatal(err)
	}
	field := regexp.MustCompile(`\b(skipGen|mGen)\b`)
	decl := regexp.MustCompile(`^  (skipGen|mGen)\?: LineGen;$`)
	set := regexp.MustCompile(`^\(R\d+\.operatorsRule!.* as Resumable(OperatorsRule|Line)\)\.(skipGen|mGen) = \w+;$`)
	class := regexp.MustCompile(`^(export )?(abstract )?class (\w+)`)
	member := regexp.MustCompile(`^  (\*)?\w+\(`)
	top := regexp.MustCompile(`^\S`)
	cls, gen := "", false
	reads, sets := 0, 0
	for i, line := range strings.Split(string(b), "\n") {
		switch m := class.FindStringSubmatch(line); {
		case m != nil:
			cls, gen = m[3], false
		case member.MatchString(line):
			gen = strings.HasPrefix(line, "  *")
		case top.MatchString(line) && line != "}":
			cls, gen = "", false
		}
		trim := strings.TrimSpace(line)
		if !field.MatchString(line) || strings.HasPrefix(trim, "*") || strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "/**") {
			continue
		}
		switch {
		case decl.MatchString(line):
		case set.MatchString(line):
			sets++
		case cls == "StreamParser" && gen:
			reads++
		default:
			t.Errorf("line %d names a resumable form outside the stream: %s", i+1, line)
		}
	}
	if reads == 0 || sets == 0 {
		t.Errorf("the bundle reads %d and sets %d resumable forms: the test checks nothing", reads, sets)
	}
}

// TestStreamPrintedTypes checks the parsers printed as with -stream for the grammars of the stream tests,
// with the driver, and the files generated for the grammars of the stream oracle, under the strictest
// checks of tsc.
func TestStreamPrintedTypes(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs tsc")
	}
	tsc := tooltest.RequireTSC(t)
	for _, src := range []string{tsStreamPlainGrammar, tsStreamCallGrammar, tsStreamOperatorsGrammar} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		b, err := tsStreamBundle(g, common.GenOptions{Start: "top"})
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
	// The files of the streams of the oracle grammars, as generated, with and without limit controls
	// and types: createStream reads every declaration of the stream, so that nothing is unused.
	for i, c := range streamOracleCases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatal(err)
		}
		files, err := codegentest.GenerateTSStream(g, common.GenOptions{Start: "main", Expectations: true, NoLimitControls: i%2 == 1, Types: i%3 == 0})
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		var names []string
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(dir, f.Path), f.Content, 0o644); err != nil {
				t.Fatal(err)
			}
			names = append(names, f.Path)
		}
		cmd := exec.Command(tsc, append(append([]string{"--noEmit", "--allowImportingTsExtensions"}, tsStrictFlags...), names...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tsc: %v\n%s\nthe grammar:\n%s", err, out, c.src)
		}
	}
}
