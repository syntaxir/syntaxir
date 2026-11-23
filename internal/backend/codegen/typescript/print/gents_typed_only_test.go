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

// TestGeneratedTSTypedOnlyMatchesFullBuild checks that a typed-only TypeScript parser returns the same typed values,
// the same recognition with Recognize, and the same compute units as the full build, on the typed grammars of the
// generator corpus (the long tier runs the larger typed corpus). The typed-only parser has no parse, parseRule or
// parseWithOptions, and it compiles under the strict options of the typed consumers.
func TestGeneratedTSTypedOnlyMatchesFullBuild(t *testing.T) {
	t.Parallel()
	ordinary := codegentest.Case{Name: "ordinary-typed", Src: codegentest.OrdinaryOnlyTyped, Inputs: codegentest.OrdinaryOnlyTypedInputs}
	// The cases are few, so the default tier runs all of them: codegentest.Representative would sample them by hash.
	checkTypedOnly(t, append([]codegentest.Case{ordinary}, codegentest.TypedJoinMemberCases()...))
}

// checkTypedOnly runs checkTypedOnlyCase on each case, as subtests.
func checkTypedOnly(t *testing.T, cases []codegentest.Case) {
	t.Helper()
	if testing.Short() {
		t.Skip("runs generated TypeScript consumers")
	}
	node := tooltest.RequireNode(t)
	tsc := tooltest.RequireTSC(t)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			checkTypedOnlyCase(t, node, tsc, c)
		})
	}
}

// checkTypedOnlyCase checks the typed-only parser of one case against the full build.
func checkTypedOnlyCase(t *testing.T, node, tsc string, c codegentest.Case) {
	g, err := parseTest(c.Src)
	if err != nil {
		t.Fatal(err)
	}
	full, err := codegentest.GenerateTS(g, common.GenOptions{Start: "main", Types: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	only, err := codegentest.GenerateTS(g, common.GenOptions{Start: "main", Types: true, TypedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	onlyRec, err := codegentest.GenerateTS(g, common.GenOptions{Start: "main", Types: true, TypedOnly: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range only {
		for _, entry := range []string{"export function parse(", "export function parseRule(", "export function parseWithOptions(", "export function parseRuleWithOptions(", "export function recognize("} {
			if strings.Contains(string(f.Content), entry) {
				t.Fatalf("typed-only %s has %s", f.Path, entry)
			}
		}
	}
	for _, f := range onlyRec {
		for _, entry := range []string{"export function parse(", "export function parseRule(", "export function parseWithOptions(", "export function parseRuleWithOptions("} {
			if strings.Contains(string(f.Content), entry) {
				t.Fatalf("typed-only with Recognize %s has %s", f.Path, entry)
			}
		}
	}
	dir := t.TempDir()
	writeTSFiles(t, filepath.Join(dir, "full"), full)
	writeTSFiles(t, filepath.Join(dir, "only"), only)
	writeTSFiles(t, filepath.Join(dir, "onlyrec"), onlyRec)
	inputs := make([]string, 0, len(c.Inputs))
	for _, in := range c.Inputs {
		quoted, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, string(quoted))
	}
	if err := os.WriteFile(filepath.Join(dir, "consumer.ts"), []byte(strings.ReplaceAll(tsTypedOnlyConsumer, "INPUTS", "["+strings.Join(inputs, ",")+"]")), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"--target", "es2020", "--lib", "es2020", "--strict", "--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--noFallthroughCasesInSwitch", "--noImplicitOverride", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess", "--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly", "--verbatimModuleSyntax", "--allowImportingTsExtensions", "--noEmit", "full/parser.ts", "only/parser.ts", "onlyrec/parser.ts", "consumer.ts"}
	cmd := exec.Command(tsc, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("strict typed-only consumer: %v\n%s", err, out)
	}
	cmd = exec.Command(node, codegentest.NodeTSArgs("consumer.ts")...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("typed-only Node consumer: %v\n%s", err, out)
	}
}

// tsTypedOnlyConsumer compares, for each input and unit, the typed result of parseValue of the full build and of the
// typed-only parser, the recognition of both recognizers, and the compute units that parseValueWithOptions and
// recognizeWithOptions need under each: the smallest limit under which they succeed, and the used units of the
// interruption below it.
const tsTypedOnlyConsumer = `import * as full from "./full/parser.ts";
import * as only from "./only/parser.ts";
import * as onlyRec from "./onlyrec/parser.ts";
const inputs: string[] = INPUTS;
const stable = (x: unknown): string => JSON.stringify(x, (_key, v: unknown) => typeof v === "bigint" ? "int:" + v : v);
const report = (e: unknown): string => e === null ? "ok" : String((e as Error).name) + ":" + String((e as Error).message) + stable(e);
const typed = (r: { value: unknown; error: unknown }): string => stable(r.value) + "|" + report(r.error);
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
    const a = typed(full.parseValue(input, unit));
    const b = typed(only.parseValue(input, unit));
    if (a !== b) failures.push("unit " + unit + " input " + i + ": parseValue " + a + ", typed-only " + b);
    const [fu, fat] = units(l => typed(full.parseValueWithOptions(input, { unit, limits: { compute: l } })));
    const [ou, oat] = units(l => typed(only.parseValueWithOptions(input, { unit, limits: { compute: l } })));
    if (fu !== ou || fat !== oat) failures.push("unit " + unit + " input " + i + ": full parse charges " + fu + " units, typed-only " + ou + "\nfull at limits below:  " + fat + "\ntyped-only at limits below: " + oat);
    const ra = report(full.recognize(input, unit));
    const rb = report(onlyRec.recognize(input, unit));
    if (ra !== rb) failures.push("unit " + unit + " input " + i + ": recognize " + ra + ", typed-only with Recognize " + rb);
    const [ru, rat] = units(l => report(full.recognizeWithOptions(input, { unit, limits: { compute: l } })));
    const [tu, tat] = units(l => report(onlyRec.recognizeWithOptions(input, { unit, limits: { compute: l } })));
    if (ru !== tu || rat !== tat) failures.push("unit " + unit + " input " + i + ": full recognize charges " + ru + " units, typed-only with Recognize " + tu + "\nfull at limits below:  " + rat + "\ntyped-only at limits below: " + tat);
  });
}
if (failures.length !== 0) throw new Error(failures.join("\n"));
`
