package engine_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend/astinspect"
	syntaxfmt "syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestCompiledProgramOwnsItsGrammar checks that a compiled program does not change when the
// caller changes the grammar AST it was compiled from: the programs built later from the same
// grammar, such as the recognizer, and the grammar the program returns are those of the grammar
// at compilation.
func TestCompiledProgramOwnsItsGrammar(t *testing.T) {
	t.Parallel()
	g, err := enginetest.ParseGrammar("rule main = \"a\" item* $$\nrule item = \"b\" #recover(skip=[^;]*) \";\"\n")
	if err != nil {
		t.Fatal(err)
	}
	g = starttest.MarkAll(g)
	p, err := enginetest.Compile(g, "main")
	if err != nil {
		t.Fatal(err)
	}
	written := syntaxfmt.Format(g)
	// The caller rewrites every literal of its AST, in both rules.
	for _, rule := range g.Rules() {
		astinspect.WalkExpr(rule.Expr, func(e ast.Expr) {
			if lit, ok := e.(*ast.Literal); ok {
				lit.Value = "z"
			}
		})
	}
	if got := syntaxfmt.Format(entry.Grammar(p.Program())); got != written {
		t.Errorf("grammar of the program after the caller changed its AST:\n%s\nwant\n%s", got, written)
	}
	for _, input := range []string{"a", "ab;", "ab;b;"} {
		if _, err := p.Parse(input); err != nil {
			t.Errorf("parse %q: %v", input, err)
		}
		if err := p.Recognize(input); err != nil {
			t.Errorf("recognize %q: %v", input, err)
		}
	}
	if err := p.Recognize("z"); err == nil {
		t.Error("the recognizer accepts the changed literal z")
	}
}
