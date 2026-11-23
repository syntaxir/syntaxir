package print_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// TestMissingTokensTypeOnlyGrammar checks that a grammar that only names the type missing parses,
// keeps the reference in a compiled grammar, and is refused by the generators.
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
	if _, err := codegentest.GenerateCode(t, entry.Checked(prog).Grammar, common.GenOptions{Package: "example", Start: "main"}); err == nil {
		t.Fatal("generated unsupported Missing type")
	}
}

// TestMissingTokensInsertionOnlyLeftRecursionRejected checks that a grammar whose left recursion
// exists only through an insertion is rejected like any grammar with #missing.
func TestMissingTokensInsertionOnlyLeftRecursionRejected(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testMissingTokensInsertionOnlyLeftRecursionRejected)
}

func testMissingTokensInsertionOnlyLeftRecursionRejected(t *testing.T) {
	prog := mustCompile(t, `rule main = a $$
rule a = "(" #missing a "x" / "y"`)
	if _, err := codegentest.GenerateCode(t, entry.Checked(prog).Grammar, common.GenOptions{Package: "example", Start: "main"}); err == nil {
		t.Fatal("generated a parser for the grammar")
	}
}
