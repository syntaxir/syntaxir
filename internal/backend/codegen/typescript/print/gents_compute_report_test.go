package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// The computeReport option receives the compute units that a parse spent when it ends: after a
// success, a failure and an interruption, on every entry point, whatever the compute limit is. A limit
// at the report accepts the parse, a limit below it stops the parse with a ParseInterruptionError whose
// used equals the report, and a parse starts by setting the report to zero. A parser generated without
// limit controls rejects the option, as it rejects a signal.
func TestGeneratedTSComputeReport(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`rule main = word (" " word)* !.
rule word = [a-z]+`)
	if err != nil {
		t.Fatal(err)
	}
	out := codegentest.RunTSGrammarScriptWith(t, g, `import * as p from "./parser.ts";
declare const process: { exit(code: number): never };
function fail(why: string): never { console.log(why); return process.exit(0); }
const text = "ab cd ef gh";
const entries: [string, (o: p.ParseOptions) => Error | null][] = [
  ["parse", o => p.parseWithOptions(text, o).error],
  ["parseValue", o => p.parseValueWithOptions(text, o).error],
  ["recognize", o => p.recognizeWithOptions(text, o)],
];
for (const [name, run] of entries) {
  const report: p.ComputeReport = { units: -1 };
  if (run({ computeReport: report }) !== null) fail(name + " failed");
  const n = report.units;
  if (!(n > 0)) fail(name + " reported " + n);
  for (const limits of [{ compute: n }, { compute: p.Unlimited }]) {
    const again: p.ComputeReport = { units: -1 };
    if (run({ computeReport: again, limits }) !== null) fail(name + " rejected with " + JSON.stringify(limits));
    if (limits.compute === n && again.units !== n) fail(name + " reported " + again.units + " then " + n);
  }
  const stopped: p.ComputeReport = { units: -1 };
  const error = run({ computeReport: stopped, limits: { compute: n - 1 } });
  if (!(error instanceof p.ParseInterruptionError) || error.kind !== "compute-limit") fail(name + " not stopped: " + error);
  if (stopped.units !== error.used) fail(name + " reported " + stopped.units + ", the error says " + error.used);
  const input: p.ComputeReport = { units: -1 };
  const tooLong = run({ computeReport: input, limits: { input: 1 } });
  if (!(tooLong instanceof p.ParseInterruptionError) || tooLong.kind !== "input-limit" || input.units !== 0) fail(name + " input limit reported " + input.units);
}
const bad: p.ComputeReport = { units: -1 };
if (p.parseWithOptions("ab cd 1", { computeReport: bad }).error === null || !(bad.units > 0)) fail("a failed parse reports " + bad.units);
if (p.parseWithOptions(text, { computeReport: "x" as unknown as p.ComputeReport }).error === null) fail("a report that is no object");
const same: p.ComputeReport = { units: 0 };
p.parseWithOptions(text, { computeReport: same });
const first = same.units;
p.parseWithOptions(text, { computeReport: same });
if (same.units !== first) fail("the report accumulates: " + same.units);
console.log("ok");
`, common.GenOptions{Start: "main", Types: true, Recognize: true})
	if out != "ok\n" {
		t.Fatalf("got %q", out)
	}
}

// A parser generated without limit controls has no code that spends units, so it rejects the report.
func TestGeneratedTSComputeReportNeedsControls(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`rule main = "a"+ !.`)
	if err != nil {
		t.Fatal(err)
	}
	out := codegentest.RunTSGrammarScriptWith(t, g, `import * as p from "./parser.ts";
const r = p.parseWithOptions("aaa", { computeReport: { units: 0 } } as p.ParseOptions);
if (r.error === null || !String(r.error).includes("without limit controls")) throw new Error("accepted: " + r.error);
if (p.parseWithOptions("aaa", {}).error !== null) throw new Error("plain parse failed");
console.log("ok");
`, common.GenOptions{Start: "main", NoLimitControls: true, Types: true, Recognize: true})
	if out != "ok\n" {
		t.Fatalf("got %q", out)
	}
}
