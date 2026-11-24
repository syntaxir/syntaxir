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

// insertionCases are grammars with #missing and inputs on which a parse inserts missing tokens: in
// sequences, repetitions, memoized and left-recursive rules, Operators Rules, lookaheads, @error labels
// and predicates over the missing value, and inputs that need more insertions than a small limit.
var insertionCases = []codegentest.Case{
	{Name: "token insertion", Src: "rule main = \"(\" [a-z]* \")\" #missing \";\" #missing $$",
		Inputs: []string{"(a);", "(a;", "(a)", "(a", "(", "a", ""}},
	{Name: "token insertion in left recursion", Src: "rule main = e $$\nrule e = e \"+\" #missing n / n\nrule n = [0-9]",
		Inputs: []string{"1+2", "12", "123", "1+23", "1 2", "1"}},
	{Name: "insertion in a repetition", Src: "rule main = item* $$\nrule item = word sep\nrule word = \"a\"+ #atomic\nrule sep = \";\" #missing",
		Inputs: []string{"a;a;", "aa", "a;a", "a a", "a;;", ""}},
	{Name: "insertion past the limit", Src: "rule main = (x \";\" #missing)* $$\nrule x = [a-z]",
		Inputs: []string{"a;b;c;", "abc", "ab", "abcdef"}},
	{Name: "memoized rules that insert", Src: "rule main = a \"x\" / a \"y\" / a \"z\" $$\nrule a = \"(\" b \")\" #missing\nrule b = [a-z]* / \"[\" a \"]\" #missing",
		Inputs: []string{"(b)x", "(bz", "([(b)]y", "([(b]z", "(y", "x"}},
	{Name: "lookaheads with token insertion", Src: "rule main = !(\"x\" #missing) \"y\" #missing &(\"z\" #missing) $$",
		Inputs: []string{"", "y", "z", "yz", "x"}},
	{Name: "insertion in an @error label", Src: "rule main = (\"a\" #missing \"b\") @error(\"m1\") (\"c\" #missing) $$",
		Inputs: []string{"abc", "b", "bc", "ab", ""}},
	{Name: "the message of an inserted token",
		Src:    "rule main = [a\\né]* x:(\";\" #missing) { $x.(missing).message == \"2:2: syntax error\" } \"b\" $$",
		Inputs: []string{"\nab", "a\nab", "ab", "\néb", "\n;b"}},
	{Name: "insertion in an Operators Rule", Src: "rule main = e $$\nrule e = #operators {\n operand [0-9]+ #atomic\n operand \"(\" e \")\" #missing\n" +
		" level add { infix left \"+\" }\n level post { postfix \"[\" e \"]\" #missing }\n}",
		Inputs: []string{"1+2", "(1+2", "1[2", "1[2+(3", "(", "1+"}},
	{Name: "insertion in a recovery", Src: "rule main = stmt* $$\nrule stmt = (\"let\" \" \" [a-z]+ \";\" #missing) #recover(skip=[^;]* \";\")",
		Inputs: []string{"let a;", "let a", "let alet b;", "lex a;let b", ";;"}},
	{Name: "left recursion only through an insertion", Src: "rule main = a $$\nrule a = \"(\" #missing a \"x\" / \"y\"",
		Inputs: []string{"y", "(yx", "yx", "yxx", "x", ""}},
}

// TestGeneratedParsersInsertMissingTokens checks that the generated parsers with limit controls insert
// missing tokens as the engine does, on insertionCases and the shared insertion corpus: the trees, the
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
	cases := append(append([]codegentest.Case(nil), insertionCases...), codegentest.InsertionCorpus()...)
	for _, limit := range []int{0, 2, 8} {
		for _, m := range codegentest.GeneratedParsersInsertionMismatches(t, goBin, cases, common.GenOptions{}, limit, nil) {
			t.Errorf("limit %d: %s", limit, m)
		}
	}
	parseRuleInsertsMissingTokens(t, goBin)
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
	if _, err := codegentest.GenerateCode(t, entry.Checked(prog).Grammar, common.GenOptions{Package: "example", Start: "main"}); err != nil {
		t.Fatal(err)
	}
}

// TestMissingTokensGenerationRefused checks that the Go generator refuses a grammar with #missing for
// the parsers that cannot insert: typed values.
func TestMissingTokensGenerationRefused(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testMissingTokensGenerationRefused)
}

func testMissingTokensGenerationRefused(t *testing.T) {
	prog := mustCompile(t, `rule main = (item ";" #missing)* #stream $$
rule item = [a-z]+`)
	for _, c := range []struct {
		name string
		opts common.GenOptions
	}{
		{"typed values", common.GenOptions{Types: true}},
	} {
		c.opts.Package, c.opts.Start = "example", "main"
		_, err := codegentest.GenerateCode(t, entry.Checked(prog).Grammar, c.opts)
		if err == nil || !strings.Contains(err.Error(), "missing-token recovery is not supported") {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
