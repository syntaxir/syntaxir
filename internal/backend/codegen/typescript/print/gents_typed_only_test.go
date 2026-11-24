package print_test

import (
	"encoding/json"
	"fmt"
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
// generator corpus (the long tier runs the larger typed corpus). Every rule is a start rule, so the typed values of
// parseRuleValue are compared for each rule too. The typed-only parser has no parse, parseRule or parseWithOptions,
// and it compiles under the strict options of the typed consumers.
func TestGeneratedTSTypedOnlyMatchesFullBuild(t *testing.T) {
	t.Parallel()
	ordinary := codegentest.Case{Name: "ordinary-typed", Src: codegentest.OrdinaryOnlyTyped, Inputs: codegentest.OrdinaryOnlyTypedInputs}
	// The cases are few, so the default tier runs all of them: codegentest.Representative would sample them by hash.
	checkTypedOnly(t, append([]codegentest.Case{ordinary}, codegentest.TypedJoinMemberCases()...), 0)
}

// TestGeneratedTSTypedOnlyInsertsMissingTokens checks that a typed-only TypeScript parser of a grammar with #missing
// inserts missing tokens as the full build does: the same typed values, recognition and compute units, with and
// without a missing-token limit, on the typed insertion cases.
func TestGeneratedTSTypedOnlyInsertsMissingTokens(t *testing.T) {
	t.Parallel()
	checkTypedOnly(t, codegentest.TypedInsertionCases(), 2)
}

// checkTypedOnly runs checkTypedOnlyCase on each case, as subtests. A positive missing also compares the parses
// with that missing-token limit.
func checkTypedOnly(t *testing.T, cases []codegentest.Case, missing int) {
	t.Helper()
	if testing.Short() {
		t.Skip("runs generated TypeScript consumers")
	}
	node := tooltest.RequireNode(t)
	tsc := tooltest.RequireTSC(t)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			checkTypedOnlyCase(t, node, tsc, c, missing)
		})
	}
}

// checkTypedOnlyCase checks the typed-only parser of one case against the full build.
func checkTypedOnlyCase(t *testing.T, node, tsc string, c codegentest.Case, missing int) {
	// Every rule is a start rule, so that parseRuleValue can name each of them in both builds.
	src := startAllRules(c.Src)
	names := append(grammarRuleNames(src), "", "nope")
	g, err := parseTest(src)
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
	// The typed entry points, which the full build has too, are the ones a typed-only parser must have.
	for _, built := range []struct {
		name  string
		files []common.GeneratedFile
	}{{"typed-only", only}, {"typed-only with Recognize", onlyRec}} {
		var text strings.Builder
		for _, f := range built.files {
			text.Write(f.Content)
		}
		for _, entry := range []string{"export function parseValue(", "export function parseValueWithOptions(", "export function parseRuleValue(", "export function parseRuleValueWithOptions("} {
			if !strings.Contains(text.String(), entry) {
				t.Fatalf("%s parser has no %s", built.name, entry)
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
	consumer := strings.NewReplacer("INPUTS", "["+strings.Join(inputs, ",")+"]", "NAMES", tsStringList(names), "MISSING", fmt.Sprint(missing)).Replace(tsTypedOnlyConsumer)
	if err := os.WriteFile(filepath.Join(dir, "consumer.ts"), []byte(consumer), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"--target", "es2020", "--lib", "es2020", "--strict", "--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--noFallthroughCasesInSwitch", "--noImplicitOverride", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess", "--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly", "--verbatimModuleSyntax", "--allowImportingTsExtensions", "--noEmit", "full/parser.ts", "only/parser.ts", "onlyrec/parser.ts", "consumer.ts"}
	cmd := exec.Command(tsc, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("strict typed-only consumer: %v\n%s", err, out)
	}
	if out, err := tooltest.NodeCombinedOutput(t, node, dir, codegentest.NodeTSArgs("consumer.ts")...); err != nil {
		t.Fatalf("typed-only Node consumer: %v\n%s", err, out)
	}
}

// tsTypedOnlyConsumer compares, for each input and unit, the typed result of parseValue of the full build and of the
// typed-only parser, the recognition of both recognizers, and the compute units that parseValueWithOptions and
// recognizeWithOptions need under each: the smallest limit under which they succeed, and the used units of the
// interruption below it. With a positive missing-token limit, the parses with that limit are compared as well.
const tsTypedOnlyConsumer = `import * as full from "./full/parser.ts";
import * as only from "./only/parser.ts";
import * as onlyRec from "./onlyrec/parser.ts";
const inputs: string[] = INPUTS;
const names: string[] = NAMES;
const limits: number[] = MISSING > 0 ? [0, MISSING] : [0];
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
for (const missingTokens of limits) for (const unit of [full.CodePoints, full.Bytes]) {
  inputs.forEach((input, i) => {
    const a = typed(full.parseValueWithOptions(input, { unit, missingTokens }));
    const b = typed(only.parseValueWithOptions(input, { unit, missingTokens }));
    if (a !== b) failures.push("limit " + missingTokens + " unit " + unit + " input " + i + ": parseValue " + a + ", typed-only " + b);
    const [fu, fat] = units(l => typed(full.parseValueWithOptions(input, { unit, missingTokens, limits: { compute: l } })));
    const [ou, oat] = units(l => typed(only.parseValueWithOptions(input, { unit, missingTokens, limits: { compute: l } })));
    if (fu !== ou || fat !== oat) failures.push("limit " + missingTokens + " unit " + unit + " input " + i + ": full parse charges " + fu + " units, typed-only " + ou + "\nfull at limits below:  " + fat + "\ntyped-only at limits below: " + oat);
    for (const name of names) {
      const vf = typed(full.parseRuleValueWithOptions(name, input, { unit, missingTokens }));
      const vo = typed(only.parseRuleValueWithOptions(name, input, { unit, missingTokens }));
      if (vf !== vo) failures.push("limit " + missingTokens + " unit " + unit + " input " + i + ": parseRuleValue " + name + " " + vf + ", typed-only " + vo);
      const cf = { units: 0 };
      const co = { units: 0 };
      full.parseRuleValueWithOptions(name, input, { unit, missingTokens, computeReport: cf });
      only.parseRuleValueWithOptions(name, input, { unit, missingTokens, computeReport: co });
      if (cf.units !== co.units) failures.push("limit " + missingTokens + " unit " + unit + " input " + i + ": parseRuleValueWithOptions " + name + " charges " + cf.units + " units, typed-only " + co.units);
    }
    const ra = report(full.recognizeWithOptions(input, { unit, missingTokens }));
    const rb = report(onlyRec.recognizeWithOptions(input, { unit, missingTokens }));
    if (ra !== rb) failures.push("limit " + missingTokens + " unit " + unit + " input " + i + ": recognize " + ra + ", typed-only with Recognize " + rb);
    const [ru, rat] = units(l => report(full.recognizeWithOptions(input, { unit, missingTokens, limits: { compute: l } })));
    const [tu, tat] = units(l => report(onlyRec.recognizeWithOptions(input, { unit, missingTokens, limits: { compute: l } })));
    if (ru !== tu || rat !== tat) failures.push("limit " + missingTokens + " unit " + unit + " input " + i + ": full recognize charges " + ru + " units, typed-only with Recognize " + tu + "\nfull at limits below:  " + rat + "\ntyped-only at limits below: " + tat);
  });
}
if (failures.length !== 0) throw new Error(failures.join("\n"));
`

// typedOnlyTreeEntries are the entry points that build an untyped tree. A typed-only parser has none of them,
// whatever else it is generated with.
var typedOnlyTreeEntries = []string{
	"export function parse(", "export function parseRule(", "export function parseWithOptions(", "export function parseRuleWithOptions(",
	"export function createDocument(", "export function createStream(",
}

// typedOnlyCombinations are the options that a typed-only parser takes besides its typed output: each one must
// generate a parser that compiles under the strict options and gives the typed values, the limit errors and,
// with Recognize, the recognition of the full build with the same options.
var typedOnlyCombinations = []struct {
	name string
	opts common.GenOptions
}{
	{"plain", common.GenOptions{}},
	{"recognize", common.GenOptions{Recognize: true}},
	{"no-limits", common.GenOptions{NoLimitControls: true}},
	{"no-limits-recognize", common.GenOptions{NoLimitControls: true, Recognize: true}},
	{"max-depth", common.GenOptions{MaxDepth: 64}},
	{"observability", common.GenOptions{Observability: true}},
	{"source-info", common.GenOptions{SourceInfo: true}},
	{"optimize-size", common.GenOptions{Optimize: "size"}},
	{"optimize-speed-recognize", common.GenOptions{Optimize: "speed", Recognize: true}},
	{"everything", common.GenOptions{Recognize: true, Observability: true, MaxDepth: 64, SourceInfo: true, Optimize: "size"}},
}

// TestGeneratedTSTypedOnlyCombinations checks -typed-only against the TypeScript options it combines with: the
// parseValue, parseValueWithOptions, parseRuleValue and parseRuleValueWithOptions entry points are present, no
// entry point of the tree, Document or stream is, the parser compiles under the strict options and runs, and its
// typed values, its errors under the limits of the options (compute, input and depth) and its recognition are those
// of the same parser generated without -typed-only.
func TestGeneratedTSTypedOnlyCombinations(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript consumers")
	}
	node := tooltest.RequireNode(t)
	tsc := tooltest.RequireTSC(t)
	src := startAllRules(codegentest.OrdinaryOnlyTyped)
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	args := []string{"--target", "es2020", "--lib", "es2020", "--strict", "--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--noFallthroughCasesInSwitch", "--noImplicitOverride", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess", "--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly", "--verbatimModuleSyntax", "--allowImportingTsExtensions", "--noEmit"}
	inputs := make([]string, 0, len(codegentest.OrdinaryOnlyTypedInputs))
	for _, in := range codegentest.OrdinaryOnlyTypedInputs {
		quoted, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, string(quoted))
	}
	for _, c := range typedOnlyCombinations {
		opts := c.opts
		opts.Start, opts.Types = "main", true
		full, err := codegentest.GenerateTS(g, opts)
		if err != nil {
			t.Fatalf("%s: full build: %v", c.name, err)
		}
		opts.TypedOnly = true
		only, err := codegentest.GenerateTS(g, opts)
		if err != nil {
			t.Fatalf("%s: typed-only build: %v", c.name, err)
		}
		var text strings.Builder
		for _, f := range only {
			text.Write(f.Content)
		}
		for _, entry := range typedOnlyTreeEntries {
			if strings.Contains(text.String(), entry) {
				t.Errorf("%s: typed-only parser has %s", c.name, entry)
			}
		}
		for _, entry := range []string{"export function parseValue(", "export function parseValueWithOptions(", "export function parseRuleValue(", "export function parseRuleValueWithOptions("} {
			if !strings.Contains(text.String(), entry) {
				t.Errorf("%s: typed-only parser has no %s", c.name, entry)
			}
		}
		if has := strings.Contains(text.String(), "export function recognize("); has != c.opts.Recognize {
			t.Errorf("%s: typed-only parser has recognize = %t, want %t", c.name, has, c.opts.Recognize)
		}
		writeTSFiles(t, filepath.Join(dir, c.name, "full"), full)
		writeTSFiles(t, filepath.Join(dir, c.name, "only"), only)
		consumer := strings.NewReplacer("INPUTS", "["+strings.Join(inputs, ",")+"]", "LIMITS", fmt.Sprint(!c.opts.NoLimitControls), "RECOGNIZE", fmt.Sprint(c.opts.Recognize)).Replace(tsTypedOnlyCombinationConsumer)
		if err := os.WriteFile(filepath.Join(dir, c.name, "consumer.ts"), []byte(consumer), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(tsc, append(append([]string(nil), args...), c.name+"/consumer.ts")...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: strict typed-only consumer: %v\n%s", c.name, err, out)
		}
		if out, err := tooltest.NodeCombinedOutput(t, node, dir, codegentest.NodeTSArgs(c.name+"/consumer.ts")...); err != nil {
			t.Errorf("%s: typed-only Node consumer: %v\n%s", c.name, err, out)
		}
	}
}

// tsTypedOnlyCombinationConsumer compares the typed-only parser of one combination with the full build of the same
// options, on each input, unit and limit. Without limit controls the compute and signal options do not exist, so only
// the input and depth limits are tried.
const tsTypedOnlyCombinationConsumer = `import * as full from "./full/parser.ts";
import * as only from "./only/parser.ts";
const inputs: string[] = INPUTS;
const limited: boolean = LIMITS;
const recognizing: boolean = RECOGNIZE;
const stable = (x: unknown): string => JSON.stringify(x, (_key, v: unknown) => typeof v === "bigint" ? "int:" + v : v);
const report = (e: unknown): string => e === null ? "ok" : String((e as Error).name) + ":" + String((e as Error).message) + stable(e);
const typed = (r: { value: unknown; error: unknown }): string => stable(r.value) + "|" + report(r.error);
const failures: string[] = [];
const same = (what: string, a: string, b: string): void => { if (a !== b) failures.push(what + ": full " + a + ", typed-only " + b); };
for (const unit of [full.CodePoints, full.Bytes]) {
  inputs.forEach((input, i) => {
    const at = "unit " + unit + " input " + i + " ";
    same(at + "parseValue", typed(full.parseValue(input, unit)), typed(only.parseValue(input, unit)));
    same(at + "parseRuleValue", typed(full.parseRuleValue("x", input, unit)), typed(only.parseRuleValue("x", input, unit)));
    same(at + "parseRuleValue unknown rule", typed(full.parseRuleValue("nope", input, unit)), typed(only.parseRuleValue("nope", input, unit)));
    for (const limits of limited ? [{ compute: 1 }, { compute: 3 }, { input: 2 }, { depth: 1 }] : [{ input: 2 }, { depth: 1 }]) {
      same(at + "parseValueWithOptions " + stable(limits), typed(full.parseValueWithOptions(input, { unit, limits })), typed(only.parseValueWithOptions(input, { unit, limits })));
      same(at + "parseRuleValueWithOptions " + stable(limits), typed(full.parseRuleValueWithOptions("a", input, { unit, limits })), typed(only.parseRuleValueWithOptions("a", input, { unit, limits })));
    }
    if (recognizing) {
      const f = full as unknown as { recognize(i: string, u: number): Error | null };
      const o = only as unknown as { recognize(i: string, u: number): Error | null };
      same(at + "recognize", report(f.recognize(input, unit)), report(o.recognize(input, unit)));
    }
  });
}
if (failures.length !== 0) throw new Error(failures.join("\n"));
`

// TestGeneratedTSTypedOnlyRefusals checks that -typed-only is refused at generation, with a message that names the
// reason and without output, for what it cannot be combined with: no typed output, a Document, a stream and
// recognize-only output, which all need or are the untyped tree. A grammar with #missing is no refusal: see
// TestGeneratedTSTypedOnlyInsertsMissingTokens.
func TestGeneratedTSTypedOnlyRefusals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		src  string
		opts common.GenOptions
		want string
	}{
		{"without types", codegentest.OrdinaryOnlyTyped, common.GenOptions{TypedOnly: true}, "typed-only output needs typed output (-types)"},
		{"with a document", codegentest.OrdinaryOnlyTyped, common.GenOptions{Types: true, TypedOnly: true, Document: true}, "typed-only output cannot have a Document or a stream"},
		{"with a stream", codegentest.OrdinaryOnlyTyped, common.GenOptions{Types: true, TypedOnly: true, Stream: true}, "typed-only output cannot have a Document or a stream"},
		{"with a document and a stream", codegentest.OrdinaryOnlyTyped, common.GenOptions{Types: true, TypedOnly: true, Document: true, Stream: true, Recognize: true}, "typed-only output cannot have a Document or a stream"},
		{"with recognize-only", codegentest.OrdinaryOnlyTyped, common.GenOptions{Types: true, TypedOnly: true, RecognizeOnly: true}, "recognize-only output generates only Recognize"},
		{"recognize-only alone with types", codegentest.OrdinaryOnlyTyped, common.GenOptions{Types: true, RecognizeOnly: true}, "recognize-only output generates only Recognize"},
	} {
		t.Run(c.name, func(t *testing.T) {
			g, err := parseTest(startAllRules(c.src))
			if err != nil {
				t.Fatal(err)
			}
			c.opts.Start = "main"
			files, err := codegentest.GenerateTSRaw(g, c.opts)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want one containing %q", err, c.want)
			}
			if len(files) != 0 {
				t.Errorf("a refused generation returned %d files", len(files))
			}
		})
	}
}
