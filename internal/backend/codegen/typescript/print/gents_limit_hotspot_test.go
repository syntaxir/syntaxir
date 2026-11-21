package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// A compute-limit error names the rule that the profile of the parse ranks first, and has no hotspot
// for a parse without a profile.
func TestGeneratedTSLimitHotspot(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`rule main = y / x "a" / x "b" / x "c"
rule y = "x"+ "!"
rule x = "x"+`)
	if err != nil {
		t.Fatal(err)
	}
	out := codegentest.RunTSGrammarScriptWith(t, g, `import * as p from "./parser.ts";
declare const process: { exit(code: number): never };
function fail(why: string): never { console.log(why); return process.exit(0); }
const input = "x".repeat(40) + "c";
// One unit less than the parse needs, so the check that fails comes after every evaluation.
let need = 1;
while (p.parseWithOptions(input, { limits: { compute: need } }).error !== null) { if (++need > 5000) fail("needs more than 5000 units"); }
const profile = new p.Profile();
const e = p.parseWithOptions(input, { limits: { compute: need - 1 }, profile }).error;
if (!(e instanceof p.ParseInterruptionError) || e.kind !== "compute-limit") fail("error " + e);
if (e.hotspot !== "x" || !e.message.includes("hotspot rule x")) fail("hotspot " + e.hotspot + ": " + e.message);
const f = p.parseWithOptions(input, { limits: { compute: need - 1 } }).error;
if (!(f instanceof p.ParseInterruptionError) || f.hotspot !== "" || f.message.includes("hotspot")) fail("without a profile: " + f);
console.log("ok");
`, common.GenOptions{Start: "main", Observability: true, Types: true, Recognize: true})
	if out != "ok\n" {
		t.Fatalf("got %q", out)
	}
}
