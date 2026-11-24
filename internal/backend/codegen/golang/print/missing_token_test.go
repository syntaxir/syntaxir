package print_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// TestGeneratedParsersInsertMissingTokens checks that the generated parsers with limit controls insert
// missing tokens as the engine does, on the insertion cases and the shared insertion corpus: the trees, the
// errors, the recognitions and the compute units of the parses with a limit of missing tokens, which
// run the tables of the insertion program; and with no limit, those of the parses of the same
// grammars, which run the parser's tables and insert nothing.
func TestGeneratedParsersInsertMissingTokens(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	testGeneratedParsersInsertMissingTokens(t)
}

// TestGeneratedParsersInsertMissingTokensOrdinary is TestGeneratedParsersInsertMissingTokens for the
// parsers without limit controls, whose functions count no units.
func TestGeneratedParsersInsertMissingTokensOrdinary(t *testing.T) {
	t.Parallel()
	codegentest.PinOrdinary(t)
	testGeneratedParsersInsertMissingTokens(t)
}

func testGeneratedParsersInsertMissingTokens(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := append(codegentest.InsertionCases(), codegentest.InsertionCorpus()...)
	for _, limit := range []int{0, 2, 8} {
		for _, m := range codegentest.GeneratedParsersInsertionMismatches(t, goBin, cases, common.GenOptions{}, limit, nil) {
			t.Errorf("limit %d: %s", limit, m)
		}
	}
	parseRuleInsertsMissingTokens(t, goBin)
}

// TestGeneratedTypedParsersInsertMissingTokens checks that the typed generated parsers insert missing
// tokens as the engine does: on the typed insertion cases, the insertion cases and the shared insertion corpus, the
// typed values, which hold the Missing values of the insertions, equal those of the reference route
// and the engine's values, and the trees, errors and compute units of the typed parsers equal those of
// the node parsers, with the limits of missing tokens that insert nothing, some and all.
func TestGeneratedTypedParsersInsertMissingTokens(t *testing.T) {
	t.Parallel()
	codegentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := append(append(codegentest.TypedInsertionCases(), codegentest.InsertionCases()...), codegentest.InsertionCorpus()...)
	for _, limit := range []int{0, 2, 8} {
		for _, m := range codegentest.GeneratedTypesInsertionMismatches(t, goBin, cases, common.GenOptions{ConvertTypes: true}, limit, len(cases)) {
			t.Errorf("limit %d: %s", limit, m)
		}
		for _, m := range codegentest.GeneratedParsersInsertionMismatches(t, goBin, cases, common.GenOptions{Types: true}, limit, nil) {
			t.Errorf("units, limit %d: %s", limit, m)
		}
	}
	parseRuleValueInsertsMissingTokens(t, goBin)
}

// parseRuleValueInsertsMissingTokens checks that ParseRuleValue inserts missing tokens as ParseValue
// does: for each start rule, the typed value and error of ParseRuleValue equal those of ParseValue
// of a parser whose default start rule it is, at the limits of missing tokens that insert nothing,
// some and all, and they count the same compute units. (ParseValue is compared with the engine by
// TestGeneratedTypedParsersInsertMissingTokens.)
func parseRuleValueInsertsMissingTokens(t *testing.T, goBin string) {
	t.Helper()
	const rules = "rule a Tok = o:\"(\" #missing b \")\" #missing => $o\nrule b = [a-z]* / \"[\" a \"]\" #missing\ntype Tok ~ node[string] | missing\n"
	srcs := map[string]string{
		"pmain": "@start(default)\nrule main = a \"x\" / a \"y\" $$\n@start\n" + rules,
		"pa":    "@start\nrule main = a \"x\" / a \"y\" $$\n@start(default)\n" + rules,
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(codegentest.GoMod("codegentest")), 0o644); err != nil {
		t.Fatal(err)
	}
	for pkg, start := range map[string]string{"pmain": "main", "pa": "a"} {
		g, err := parseTest(srcs[pkg])
		if err != nil {
			t.Fatal(err)
		}
		codegentest.GeneratePackage(t, dir, pkg, g, common.GenOptions{Start: start, Types: true})
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	"encoding/json"
	"fmt"

	"codegentest/pa"
	"codegentest/pmain"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

func show(v any, err error) string {
	b, _ := json.Marshal(v)
	return fmt.Sprintf("%s %v", b, err)
}

func main() {
	for _, in := range []string{"(b)x", "(b", "([(b)]y", "([(b]", "([(b", "(", "", "b)"} {
		for _, limit := range []int{0, 2, 8} {
			o := sxirruntime.WithMissingTokens(limit)
			for _, c := range []struct {
				name      string
				got, want string
			}{
				{"main", show(pmain.ParseRuleValue("main", in, o)), show(pmain.ParseValue(in, o))},
				{"a", show(pmain.ParseRuleValue("a", in, o)), show(pa.ParseValue(in, o))},
			} {
				if c.got != c.want {
					fmt.Printf("ParseRuleValue(%q, %q) at limit %d:\n got  %s\n want %s\n", c.name, in, limit, c.got, c.want)
				}
			}
			var got, want uint64
			pmain.ParseRuleValue("main", in, o, sxirruntime.WithComputeReport(&got))
			pmain.ParseValue(in, o, sxirruntime.WithComputeReport(&want))
			var gotA, wantA uint64
			pmain.ParseRuleValue("a", in, o, sxirruntime.WithComputeReport(&gotA))
			pa.ParseValue(in, o, sxirruntime.WithComputeReport(&wantA))
			if got != want || gotA != wantA {
				fmt.Printf("compute units of ParseRuleValue and ParseValue of %q at limit %d: main %d and %d, a %d and %d\n", in, limit, got, want, gotA, wantA)
			}
		}
	}
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Errorf("go run: %v\n%s", err, out)
	}
}

// parseRuleInsertsMissingTokens checks that ParseRule inserts missing tokens as the engine's parse of the
// same start rule does: the default start rule, and another start rule that the insertions reach memoized
// and through recursion, with the limits of missing tokens that insert nothing, some and all.
func parseRuleInsertsMissingTokens(t *testing.T, goBin string) {
	t.Helper()
	const src = "@start(default)\nrule main = a \"x\" / a \"y\" $$\n@start\nrule a = \"(\" b \")\" #missing\nrule b = [a-z]* / \"[\" a \"]\" #missing"
	inputs := []string{"(b)", "(b", "([(b)]", "([(b]", "([(b", "(", "", "b)"}
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	// The generated code projects, as the engine's program without projections shows (check.Options).
	prog := compileBare(t, src, check.Options{NoProjections: true})
	code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: "g", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	var want, rows strings.Builder
	for _, name := range []string{"main", "a"} {
		for _, in := range inputs {
			for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
				for _, limit := range []int{0, 2, 8} {
					n, err := prog.ParseWith(name, in, engine.ParseOptions{Expectations: true, Unit: unit, MissingTokens: limit})
					fmt.Fprintf(&want, "%s %q %d %d %s\n", name, in, unit, limit, codegentest.ResultJSON(n, err))
					fmt.Fprintf(&rows, "\t{%q, %q, %d, %d},\n", name, in, unit, limit)
				}
			}
		}
	}
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", codegentest.GoMod("parserule"))
	write("g/parser.go", string(code))
	write("main.go", `package main

import (
	"encoding/json"
	"fmt"

	"parserule/g"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
)

var rows = []struct {
	name, input  string
	unit, limit int
}{
`+rows.String()+`}

func main() {
	for _, r := range rows {
		n, err := g.ParseRule(r.name, r.input, g.WithUnit(g.PositionUnit(r.unit)), sxirruntime.WithMissingTokens(r.limit))
		out := map[string]any{"node": n}
		if err != nil {
			out["err"] = err.Error()
		}
		b, _ := json.Marshal(out)
		fmt.Printf("%s %q %d %d %s\n", r.name, r.input, r.unit, r.limit, b)
	}
}
`)
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), codegentest.GoFlagsEnv, "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	got, exp := strings.Split(string(out), "\n"), strings.Split(want.String(), "\n")
	if len(got) != len(exp) {
		t.Fatalf("ParseRule printed %d lines, want %d:\n%s", len(got), len(exp), out)
	}
	for i := range got {
		if got[i] != exp[i] {
			t.Errorf("ParseRule differs from the engine:\n generated %s\n engine    %s", got[i], exp[i])
		}
	}
}

// TestMissingTokensTypeOnlyGrammar checks a grammar that names the missing type without a #missing
// site: the engine parses it, a compiled grammar keeps it, and the Go generator generates its parser.
func TestMissingTokensTypeOnlyGrammar(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testMissingTokensTypeOnlyGrammar)
}

func testMissingTokensTypeOnlyGrammar(t *testing.T) {
	prog := mustCompile(t, `type Item ~ missing
rule main = "a"`)
	if n, err := prog.Parse("main", "a"); n == nil || err != nil {
		t.Fatal(n, err)
	}
	if !prog.HasMissing {
		t.Fatal("reserved Missing reference was not recorded")
	}
	// A compiled grammar keeps the reference: the loaded program parses as the source does.
	data, err := prog.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := engine.LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.HasMissing {
		t.Fatal("the loaded program lost the reference to missing")
	}
	if n, err := loaded.ParseWith("main", "a", engine.ParseOptions{Backend: engine.Bytecode, MissingTokens: 1}); n == nil || err != nil {
		t.Fatal("loaded", n, err)
	}
	if _, err := codegentest.GenerateCode(t, entry.Grammar(prog), common.GenOptions{Package: "example", Start: "main"}); err != nil {
		t.Fatal(err)
	}
}

// TestMissingTokensTypedDeclarations checks the types package of a typed parser: a grammar with
// #missing declares Missing and the sealed ErrorValue, the Go type of error, and reserves the name
// ErrorValue; a grammar without it keeps *Error for error and leaves the name to the grammar.
func TestMissingTokensTypedDeclarations(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testMissingTokensTypedDeclarations)
}

func testMissingTokensTypedDeclarations(t *testing.T) {
	// recovered declares S, whose field E holds a recovered value, and its rule; inserted adds #missing.
	const recovered = "type S = { E error }\n@start\nrule main = s:(\"a\" #recover(skip=[^;]*)) \";\" $$ => S{E: $s.(error)}\n"
	inserted := strings.Replace(recovered, `";"`, `";" #missing`, 1)
	types := func(src string) (string, error) {
		files, err := codegentest.Generate(t, entry.Grammar(mustCompile(t, src)), common.GenOptions{Package: "example", Module: "example.com/example", Start: "main", Types: true})
		if err != nil {
			return "", err
		}
		for _, f := range files {
			if f.Path == "types/types.go" {
				return string(f.Content), nil
			}
		}
		t.Fatalf("no types package in %d files", len(files))
		return "", nil
	}
	code, err := types(inserted)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\ntype Missing struct {", "\ntype ErrorValue interface{ isErrorValue() }", "\tE ErrorValue\n"} {
		if !strings.Contains(code, want) {
			t.Errorf("the types of a grammar with #missing lack %q", want)
		}
	}
	code, err = types(recovered)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(code, "type Missing") || strings.Contains(code, "type ErrorValue") || strings.Contains(code, "isErrorValue") || !strings.Contains(code, "\tE *Error\n") {
		t.Errorf("the types of a grammar without #missing:\n%s", code)
	}
	// Missing is a reserved type name of the grammar language; ErrorValue is reserved by the types
	// package of a grammar with #missing only.
	const decl = "type ErrorValue = { F node[string] }\n"
	if _, err := types(decl + inserted); err == nil || !strings.Contains(err.Error(), "the type ErrorValue has the name of a type that the generated types package declares") {
		t.Errorf("ErrorValue in a grammar with #missing: %v", err)
	}
	if _, err := types(decl + recovered); err != nil {
		t.Errorf("ErrorValue in a grammar without #missing: %v", err)
	}
}
