package print_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// TestGeneratedTSRuleValueMatchesGo checks that the TypeScript parseRuleValue returns the values, errors and compute
// units of the Go ParseRuleValue, for every rule of the typed grammars of the default corpus.
func TestGeneratedTSRuleValueMatchesGo(t *testing.T) {
	t.Parallel()
	checkRuleValue(t, []codegentest.Case{{Name: "ordinary-typed", Src: codegentest.OrdinaryOnlyTyped, Inputs: codegentest.OrdinaryOnlyTypedInputs}})
}

// checkRuleValue runs checkRuleValueCase on each case, as subtests.
func checkRuleValue(t *testing.T, cases []codegentest.Case) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs Go and TypeScript parsers")
	}
	node := tooltest.RequireNode(t)
	tsc := tooltest.RequireTSC(t)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			checkRuleValueCase(t, node, tsc, c)
		})
	}
}

// checkRuleValueCase compares, for every rule of the case (each one a start rule), the names "" and "nope", every
// input as bytes and both position units, the Go and the TypeScript results of ParseRuleValue: the value, the error
// and the compute units. The Go transcript runs in a module of its own; the TypeScript consumer runs under strict tsc.
func checkRuleValueCase(t *testing.T, node, tsc string, c codegentest.Case) {
	src := startAllRules(c.Src)
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	names := append(grammarRuleNames(src), "", "nope")
	dir := t.TempDir()
	goDir := filepath.Join(dir, "go")
	if err := os.MkdirAll(filepath.Join(goDir, "rulevalue"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "go.mod"), []byte(codegentest.GoMod("ordtest")), 0o644); err != nil {
		t.Fatal(err)
	}
	goFiles, err := codegentest.Generate(t, g, common.GenOptions{Package: "rulevalue", Module: "ordtest/rulevalue", Start: "main", Lib: common.LibImport, Types: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range goFiles {
		path := filepath.Join(goDir, "rulevalue", filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	program := strings.NewReplacer("INPUTS", goStringList(c.Inputs), "NAMES", goStringList(names), "PKG", "rulevalue").Replace(goRuleValueTranscript)
	if err := os.WriteFile(filepath.Join(goDir, "rulevalue", "transcript_test.go"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	goLines := transcriptLines(runGoTranscript(t, goDir, "rulevalue"))

	tsFiles, err := codegentest.GenerateTS(g, common.GenOptions{Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	tsDir := filepath.Join(dir, "ts")
	writeTSFiles(t, tsDir, tsFiles)
	hexInputs := make([]string, 0, len(c.Inputs))
	for _, in := range c.Inputs {
		hexInputs = append(hexInputs, strconv.Quote(fmt.Sprintf("%x", in)))
	}
	consumer := strings.NewReplacer("INPUTS", "["+strings.Join(hexInputs, ",")+"]", "NAMES", tsStringList(names)).Replace(tsRuleValueConsumer)
	if err := os.WriteFile(filepath.Join(tsDir, "consumer.ts"), []byte(consumer), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append(append([]string{}, strictTSArgs...), "parser.ts", "consumer.ts")
	cmd := exec.Command(tsc, args...)
	cmd.Dir = tsDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("strict TypeScript consumer: %v\n%s", err, out)
	}
	cmd = exec.Command(node, codegentest.NodeTSArgs("consumer.ts")...)
	cmd.Dir = tsDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("TypeScript Node consumer: %v\n%s", err, out)
	}
	tsLines := transcriptLines(string(out))

	if len(goLines) != len(tsLines) || len(goLines) == 0 {
		t.Fatalf("the transcripts have %d Go and %d TypeScript lines", len(goLines), len(tsLines))
	}
	values, units := 0, 0
	for i := range goLines {
		goLine, tsLine := normalizeRuleValueLine(t, goLines[i]), normalizeRuleValueLine(t, tsLines[i])
		if !reflect.DeepEqual(goLine, tsLine) {
			t.Errorf("line %d differs:\nGo         %v\nTypeScript %v", i, goLines[i], tsLines[i])
		}
		if goLine["value"] != nil {
			values++
		}
		if u, _ := goLine["units"].(float64); u > 0 {
			units++
		}
	}
	// The comparison must see values and charges, or it passes for nothing.
	if values == 0 || units == 0 {
		t.Fatalf("the transcripts have %d values and %d charged parses: the comparison cannot fail", values, units)
	}
	t.Logf("%d lines compared: %d with a value, %d charged", len(goLines), values, units)
}

// startAllRules marks every rule of the grammar src as a start rule, so that each one can be named. The rule main,
// which the tests configure as the start, is the default start rule, since a grammar with several start rules needs one.
func startAllRules(src string) string {
	return ruleDeclName.ReplaceAllStringFunc(strings.ReplaceAll(src, "@start\n", ""), func(decl string) string {
		if decl == "rule main" {
			return "@start(default)\n" + decl
		}
		return "@start\n" + decl
	})
}

var ruleDeclName = regexp.MustCompile(`(?m)^rule [A-Za-z][A-Za-z0-9_]*`)

// grammarRuleNames returns the names of the rules of src, in declaration order.
func grammarRuleNames(src string) []string {
	var names []string
	for _, decl := range ruleDeclName.FindAllString(src, -1) {
		names = append(names, strings.TrimPrefix(decl, "rule "))
	}
	return names
}

// goStringList is the Go literal of a slice of strings, for the transcript programs.
func goStringList(xs []string) string {
	quoted := make([]string, len(xs))
	for i, x := range xs {
		quoted[i] = strconv.Quote(x)
	}
	return "[]string{" + strings.Join(quoted, ", ") + "}"
}

// tsStringList is the TypeScript literal of a slice of strings.
func tsStringList(xs []string) string {
	quoted := make([]string, len(xs))
	for i, x := range xs {
		quoted[i] = strconv.Quote(x)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// runGoTranscript runs the transcript test of the module dir with the build flags of the generated programs, which
// turn on their feature witnesses, and returns the file that it writes for the package pkg.
func runGoTranscript(t *testing.T, dir, pkg string) string {
	t.Helper()
	cmd := exec.Command("go", "test", "-count=1", "-run", "TestTranscript", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local", "TRANSCRIPT_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(dir, pkg+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// transcriptLines splits a transcript into its nonempty lines.
func transcriptLines(s string) []string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// normalizeRuleValueLine decodes one transcript line into the form that both languages' values share. The Go and
// TypeScript representations differ in the following ways, which the comparison maps to one form:
//   - the TypeScript values carry a "type" discriminator, which a Go struct does not, so "type" is dropped;
//   - Go names the fields as in Go, with capital initials, where TypeScript keeps the grammar's names, so keys are
//     compared lowercased;
//   - a Go struct of the elements E0, E1, ... is a tuple, which TypeScript writes as an array;
//   - Go writes a node as its Start, End and Value, which TypeScript writes as start, end and value.
//
// The transcript writes null for the value of a failed parse whose Go value is the zero value of its type, since
// TypeScript returns null there (see the Go transcript).
func normalizeRuleValueLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		t.Fatalf("transcript line %q: %v", line, err)
	}
	raw["value"] = normalizeValue(raw["value"])
	return raw
}

func normalizeValue(v any) any {
	switch v := v.(type) {
	case map[string]any:
		if elems, ok := tupleElems(v); ok {
			return normalizeValue(elems)
		}
		out := map[string]any{}
		for k, x := range v {
			if k == "type" {
				continue
			}
			out[strings.ToLower(k)] = normalizeValue(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = normalizeValue(x)
		}
		return out
	}
	return v
}

// tupleElems returns the elements of a Go tuple, a struct whose fields are E0, E1, ... in order, and whether m is one.
func tupleElems(m map[string]any) ([]any, bool) {
	if len(m) == 0 {
		return nil, false
	}
	elems := make([]any, len(m))
	for i := range elems {
		x, ok := m["E"+strconv.Itoa(i)]
		if !ok {
			return nil, false
		}
		elems[i] = x
	}
	return elems, true
}

// strictTSArgs are the compiler options of the typed consumers.
var strictTSArgs = []string{"--target", "es2020", "--lib", "es2020", "--strict", "--noUnusedLocals", "--noUnusedParameters", "--noImplicitReturns", "--noFallthroughCasesInSwitch", "--noImplicitOverride", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess", "--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly", "--verbatimModuleSyntax", "--allowImportingTsExtensions", "--noEmit"}

// goRuleValueTranscript prints one JSON line per input (as hex bytes), position unit and name: the value, the error
// and the compute units of ParseRuleValue.
const goRuleValueTranscript = `package PKG

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

func TestTranscript(t *testing.T) {
	var b strings.Builder
	for _, in := range INPUTS {
		for _, unit := range []struct {
			label string
			unit  PositionUnit
		}{{"CodePoints", CodePoints}, {"Bytes", Bytes}} {
			for _, name := range NAMES {
				var units uint64
				v, err := ParseRuleValue(name, in, WithUnit(unit.unit), sxirruntime.WithComputeReport(&units))
				// A failed parse has no value in TypeScript, where it is null; Go returns the zero value of the type.
				rv := reflect.ValueOf(v)
				if err != nil && (!rv.IsValid() || rv.IsZero()) {
					v = nil
				}
				line := map[string]any{"rule": name, "unit": unit.label, "input": hex.EncodeToString([]byte(in)), "value": v, "units": units, "error": nil}
				if err != nil {
					line["error"] = err.Error()
				}
				j, err := json.Marshal(line)
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintln(&b, string(j))
			}
		}
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("TRANSCRIPT_DIR"), "PKG.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
`

// tsRuleValueConsumer prints the same lines as goRuleValueTranscript, from the TypeScript parseRuleValue. The
// options call must agree with the plain call, and its computeReport gives the units.
const tsRuleValueConsumer = `import * as p from "./parser.ts";
declare const console: { log(line: string): void };
const names: string[] = NAMES;
const inputs: string[] = INPUTS;
const bytes = (hex: string): Uint8Array => Uint8Array.from((hex.match(/../g) ?? []).map((h) => parseInt(h, 16)));
const replacer = (_key: string, v: unknown): unknown => typeof v === "bigint" ? Number(v) : v;
const message = (e: Error | null): string | null => e === null ? null : e.message;
const units: [string, p.Unit][] = [["CodePoints", p.CodePoints], ["Bytes", p.Bytes]];
for (const hex of inputs) {
  const input = bytes(hex);
  for (const [label, unit] of units) {
    for (const name of names) {
      const r = p.parseRuleValue(name, input, unit);
      const report = { units: 0 };
      const o = p.parseRuleValueWithOptions(name, input, { unit, computeReport: report });
      if (JSON.stringify(r.value, replacer) !== JSON.stringify(o.value, replacer) || message(r.error) !== message(o.error)) {
        throw new Error("parseRuleValue and parseRuleValueWithOptions differ for rule " + name);
      }
      console.log(JSON.stringify({ rule: name, unit: label, input: hex, value: r.value, units: report.units, error: message(r.error) }, replacer));
    }
  }
}
`
