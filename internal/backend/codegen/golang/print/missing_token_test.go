package print_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
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
