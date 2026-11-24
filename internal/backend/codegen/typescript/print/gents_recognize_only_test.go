package print_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestGeneratedTSRecognizeOnlyMatchesFullBuild checks that a recognize-only TypeScript parser
// recognizes exactly what the recognize function of the full build recognizes, and charges the same
// compute units, on a representative part of the generator corpus (the full corpus runs in the long
// tier). The recognize-only parser has no parse entry point, and it compiles under the strict
// options of the typed consumers.
func TestGeneratedTSRecognizeOnlyMatchesFullBuild(t *testing.T) {
	t.Parallel()
	checkRecognizeOnly(t, codegentest.Representative(codegentest.GenCorpus(t)))
}

// checkRecognizeOnly runs checkRecognizeOnlyCase on each case, as subtests.
func checkRecognizeOnly(t *testing.T, cases []codegentest.Case) {
	t.Helper()
	if testing.Short() {
		t.Skip("runs generated TypeScript consumers")
	}
	node := tooltest.RequireNode(t)
	tsc := tooltest.RequireTSC(t)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			checkRecognizeOnlyCase(t, node, tsc, c)
		})
	}
}

// checkRecognizeOnlyCase checks the recognize-only parser of one case against the full build.
func checkRecognizeOnlyCase(t *testing.T, node, tsc string, c codegentest.Case) {
	g, err := parseTest(c.Src)
	if err != nil {
		t.Fatal(err)
	}
	full, err := codegentest.GenerateTS(g, common.GenOptions{Start: "main", Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	only, err := codegentest.GenerateTS(g, common.GenOptions{Start: "main", RecognizeOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range only {
		for _, entry := range []string{"export function parse(", "export function parseRule(", "export function parseWithOptions(", "export function parseRuleWithOptions(", "export function parseValue("} {
			if strings.Contains(string(f.Content), entry) {
				t.Fatalf("recognize-only %s has %s", f.Path, entry)
			}
		}
	}
	dir := t.TempDir()
	writeTSFiles(t, filepath.Join(dir, "full"), full)
	writeTSFiles(t, filepath.Join(dir, "only"), only)
	inputs := make([]string, 0, len(c.Inputs))
	for _, in := range c.Inputs {
		quoted, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, string(quoted))
	}
	if err := os.WriteFile(filepath.Join(dir, "consumer.ts"), []byte(strings.ReplaceAll(tsRecognizeOnlyConsumer, "INPUTS", "["+strings.Join(inputs, ",")+"]")), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"--target", "es2020", "--lib", "es2020", "--strict", "--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--noFallthroughCasesInSwitch", "--noImplicitOverride", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess", "--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly", "--verbatimModuleSyntax", "--allowImportingTsExtensions", "--noEmit", "full/parser.ts", "only/parser.ts", "consumer.ts"}
	cmd := exec.Command(tsc, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("strict recognize-only consumer: %v\n%s", err, out)
	}
	if out, err := tooltest.NodeCombinedOutput(t, node, dir, codegentest.NodeTSArgs("consumer.ts")...); err != nil {
		t.Fatalf("recognize-only Node consumer: %v\n%s", err, out)
	}
}

// tsRecognizeOnlyConsumer compares, for each input and unit, the result of the recognize of the full build and of
// the recognize-only parser, and the compute units that recognizeWithOptions needs under each: the smallest limit under
// which it succeeds, and the used units of the interruption below it.
const tsRecognizeOnlyConsumer = `import * as full from "./full/parser.ts";
import * as only from "./only/parser.ts";
const inputs: string[] = INPUTS;
const stable = (x: unknown): string => JSON.stringify(x, (_key, v: unknown) => typeof v === "bigint" ? "int:" + v : v);
const report = (e: unknown): string => e === null ? "ok" : String((e as Error).name) + ":" + String((e as Error).message) + stable(e);
const units = (run: (limit: number) => string): [number, string] => {
  let hi = 16;
  while (run(hi) !== "ok") { if (hi >= 2 ** 40) return [0, ""]; hi *= 2; }
  let lo = 0;
  while (hi - lo > 1) { const mid = lo + Math.floor((hi - lo) / 2); if (run(mid) === "ok") hi = mid; else lo = mid; }
  const at: string[] = [];
  for (let l = 1; l < hi; l += 1 + Math.floor(hi / 40)) at.push(run(l));
  return [hi, at.join(",")];
};
const failures: string[] = [];
for (const unit of [full.CodePoints, full.Bytes]) {
  inputs.forEach((input, i) => {
    const a = report(full.recognize(input, unit));
    const b = report(only.recognize(input, unit));
    if (a !== b) failures.push("unit " + unit + " input " + i + ": recognize " + a + ", recognize-only " + b);
    const [fu, fat] = units(l => report(full.recognizeWithOptions(input, { unit, limits: { compute: l } })));
    const [ou, oat] = units(l => report(only.recognizeWithOptions(input, { unit, limits: { compute: l } })));
    if (fu !== ou || fat !== oat) failures.push("unit " + unit + " input " + i + ": full charges " + fu + " units, recognize-only " + ou + "\nfull at limits below:      " + fat + "\nrecognize-only at limits below: " + oat);
  });
}
if (failures.length !== 0) throw new Error(failures.join("\n"));
`
