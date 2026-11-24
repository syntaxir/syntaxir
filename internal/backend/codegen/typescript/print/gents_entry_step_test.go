package print_test

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// A rule call counts one unit at its entry. A rule that reads no captures and takes callBody's unmemoized
// or first-call branch (the start rule called through p.call, a left-recursive rule, a memoized rule)
// runs its body without a capture frame; the call counts the unit there once, and the plain evaluation
// of the body does not count it again, so the parse counts the units of the engine's.
func TestGeneratedTSRuleEntryChargesOneStep(t *testing.T) {
	t.Parallel()
	if !state.BudgetControlsEnabled {
		t.Skip("budget controls are unavailable in the reference build")
	}
	src := `rule main = expr $$
rule expr = expr "+" "a" / "a"`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	var want uint64
	if _, err := mustCompile(t, src).ParseWith("main", "a+a+a", engine.ParseOptions{ComputeReport: &want}); err != nil {
		t.Fatal(err)
	}
	out := codegentest.RunTSGrammarScriptWith(t, g, `import * as p from "./parser.ts";
const report = { units: 0 };
const r = p.parseWithOptions("a+a+a", { unit: p.CodePoints, computeReport: report });
if (r.error !== null) throw new Error("parse failed");
console.log(report.units);
`, common.GenOptions{Start: "main"})
	if out != fmt.Sprintf("%d\n", want) {
		t.Fatalf("the parse counted %q units, want the engine's %d", out, want)
	}
}
