package print_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// The lean value-free rules of the TypeScript parser are printed from Flow IR, as labeled blocks.
func TestGeneratedTSPrintsLeanRulesFromFlow(t *testing.T) {
	t.Parallel()
	src := `rule main = word+
rule word = [a-z]+ ("-" [a-z]+)*`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := codegentest.GenerateTS(g, common.GenOptions{Start: "main", Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	var parser string
	for _, f := range files {
		if strings.HasSuffix(f.Path, "parser.ts") {
			parser = string(f.Content)
		}
	}
	if !strings.Contains(parser, "L_fail") {
		t.Errorf("the parser prints no labeled block for a lean rule")
	}
}

// Large and repeated parts of a rule run as functions of their own, which the pass outline chooses;
// with the pass off, no part does.
func TestGeneratedTSOutlinesLargeAndRepeatedNodes(t *testing.T) {
	t.Parallel()
	src, _ := codegentest.OutlineKeywords()
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, off := range []bool{false, true} {
		o := common.GenOptions{Start: "main", Recognize: true}
		if off {
			o.NoOpt = []string{"outline"}
		}
		files, err := codegentest.GenerateTS(g, o)
		if err != nil {
			t.Fatal(err)
		}
		var parser string
		for _, f := range files {
			if strings.HasSuffix(f.Path, "parser.ts") {
				parser = string(f.Content)
			}
		}
		if got := strings.Contains(parser, "kw (outlined)"); got == off {
			t.Errorf("outline off %v: a node of rule kw runs as a function of its own: %v", off, got)
		}
		if off && strings.Contains(parser, "(outlined)") {
			t.Errorf("outline off: a node runs as a function of its own")
		}
	}
}

// Outlined nodes with equal structure run as one function, whatever the names of the literal tables they
// read are: no two functions of a generated parser have the same text once the names of constants are
// ignored.
func TestGeneratedTSOutlinedFunctionsAreShared(t *testing.T) {
	t.Parallel()
	// Two rules of the same keywords repeat every outlined node; the letters are literals, which each
	// outlined function reads as a table of its own.
	var alts []string
	for _, w := range []string{"select", "selects", "set", "setup", "from", "frozen", "union", "unique", "update", "use",
		"using", "values", "view", "virtual", "when", "where", "while", "window", "with", "without", "write", "xor"} {
		var ls []string
		for _, r := range w {
			ls = append(ls, strconv.Quote(string(r)))
		}
		alts = append(alts, strings.Join(ls, " "))
	}
	kw := strings.Join(alts, " / ")
	src := "rule main = (kw ws)* (kw2 ws)* $$\nrule kw = " + kw + "\nrule kw2 = " + kw + "\nrule ws = \" \"*"
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := codegentest.GenerateTS(g, common.GenOptions{Start: "main", Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	var parser string
	for _, f := range files {
		if strings.HasSuffix(f.Path, "parser.ts") {
			parser = string(f.Content)
		}
	}
	fn := regexp.MustCompile(`(?s)\n// [^\n]*\(outlined\)\nfunction \w+\(_?p: Parser\): R \{\n(.*?)\n\}\n`)
	name := regexp.MustCompile(`\b(lit|scope|a)\d+\b`)
	outlined := fn.FindAllStringSubmatch(parser, -1)
	if len(outlined) < 2 {
		t.Fatalf("too few outlined functions: %d", len(outlined))
	}
	seen := map[string]bool{}
	for _, m := range outlined {
		// Calls of other outlined functions keep their names: equal callees have one name.
		body := name.ReplaceAllString(m[1], "$1")
		if seen[body] {
			t.Errorf("two outlined functions have the same text:\n%s", m[1])
		}
		seen[body] = true
	}
}

// A recovered error is a SyntaxError with its position, message and expectations, and it carries no stack
// trace; the parser leaves Error.stackTraceLimit as it found it, also when the parse is interrupted at the
// budget limit while it builds the error, and an error that a parse returns keeps its stack trace.
func TestGeneratedTSRecoveredErrorsCarryNoStack(t *testing.T) {
	t.Parallel()
	src := `rule main = item #recover(skip=[^;]+ ";")*
rule item = [a-z]+ ";"`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	out := codegentest.RunTSGrammarScriptWith(t, g, `import * as p from "./parser.ts";
const check = (x: boolean, why: string): void => { if (!x) throw new Error(why); };
const limit = (Error as { stackTraceLimit?: number }).stackTraceLimit;
const r = p.parse("a;1;b;", p.CodePoints);
check(r.error instanceof p.SyntaxErrors && r.error.errors.length === 1, "one recovered error");
const e = r.error.errors[0]!;
check(e instanceof p.SyntaxError && e instanceof Error, "a recovered error is a SyntaxError");
check(e.pos === 2 && e.line === 1 && e.col === 3 && e.expected.length > 0 && e.message.startsWith("1:3: syntax error: expected"), "position and message");
check(e.stack === undefined || !e.stack.includes("\n    at "), "no stack frames: " + e.stack);
check((Error as { stackTraceLimit?: number }).stackTraceLimit === limit, "limit restored");
const bad = p.parse("1", p.CodePoints);
check(bad.error instanceof p.SyntaxErrors || bad.error instanceof p.SyntaxError, "bad input fails");
for (let n = 1; n < 400; n++) {
  p.parseWithOptions("a;1;b;", { unit: p.CodePoints, limits: { compute: n } });
  check((Error as { stackTraceLimit?: number }).stackTraceLimit === limit, "limit restored after the budget limit " + n);
}
// The recovery counts the input it skips, so the signal is observed while the parse recovers, and
// building the recovered error counts nothing (diagnostics are free), so neither a signal getter nor
// an interruption runs while it is built. Neither sees the lowered limit, and the error of an
// interrupted parse keeps its stack.
const long = "a;1;b;" + "x".repeat(20000) + ";";
const polled = (): number[] => {
  const seen: number[] = [];
  const signal = { get aborted(): boolean { seen.push((Error as { stackTraceLimit?: number }).stackTraceLimit ?? -1); return false; } };
  check(p.parseWithOptions(long, { unit: p.CodePoints, signal }).error instanceof p.SyntaxErrors, "the long input recovers");
  return seen;
};
const seen = polled();
check(seen.length > 20 && seen.every((x) => x === limit), "the signal observes the stack limit: " + seen.filter((x) => x !== limit).length);
// makeError reads no signal: a getter that runs while it builds the error would see it lowered.
const proto = (p as unknown as { Parser: { prototype: unknown } }).Parser.prototype as { makeError: (...a: unknown[]) => unknown };
const original = proto.makeError;
let building = false, readWhileBuilding = 0;
proto.makeError = function (this: unknown, ...a: unknown[]): unknown {
  building = true;
  try { return original.apply(this, a); } finally { building = false; }
};
const watched = { get aborted(): boolean { if (building) readWhileBuilding++; return false; } };
check(p.parseWithOptions(long, { unit: p.CodePoints, signal: watched }).error instanceof p.SyntaxErrors, "the watched parse recovers");
check(readWhileBuilding === 0, "building a recovered error reads the signal " + readWhileBuilding + " times");
proto.makeError = original;
let interrupted = 0;
for (let n = 1; n <= 30000; n += 61) {
  const r = p.parseWithOptions(long, { unit: p.CodePoints, limits: { compute: n } });
  if (r.error instanceof p.ParseInterruptionError) {
    interrupted++;
    check(typeof r.error.stack === "string" && r.error.stack.includes("\n    at "), "an interruption keeps its stack at " + n);
  }
}
check(interrupted > 0, "the limits interrupt the parse");
console.log("ok");
`, common.GenOptions{Start: "main"})
	if !strings.Contains(out, "ok") {
		t.Fatalf("output %q", out)
	}
}

// A repetition counts its iterations only for what reads the count: the count of a repetition
// without a minimum is neither declared nor incremented.
func TestGeneratedTSLeavesOutUnreadCounts(t *testing.T) {
	t.Parallel()
	g, err := parseTest("rule main = item* \"b\"\nrule item = \"a\" / \"c\"")
	if err != nil {
		t.Fatal(err)
	}
	files, err := codegentest.GenerateTS(g, common.GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		// The counter of compute units is no count of a repetition.
		if strings.HasSuffix(f.Path, "parser.ts") && strings.Contains(strings.NewReplacer("p.units++;", "", "this.units++;", "").Replace(string(f.Content)), "++;") {
			t.Errorf("the parser increments a count:\n%s", f.Content)
		}
	}
}
