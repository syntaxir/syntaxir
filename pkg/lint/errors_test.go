package lint_test

import (
	"context"
	"testing"

	"syntaxir.dev/sxir/pkg/grammar"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/lint"
)

// TestCheckErrors checks that a grammar that does not check gives the grammar.CheckErrors that
// package compiler also returns, from every entry point of the linter.
func TestCheckErrors(t *testing.T) {
	g, err := grammar.Load(grammar.FromString("g.sxir", "@start\nrule main = undefined\n"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"Run":      func() error { _, err := lint.Config{}.Run(ctx, g, ""); return err },
		"Rewrites": func() error { _, err := lint.Rewrites(ctx, g, "", nil); return err },
		"Classify": func() error { _, err := lint.Classify(ctx, g, ""); return err },
	} {
		err := call()
		errs, ok := err.(grammar.CheckErrors)
		if !ok || len(errs) != 1 || errs[0].Pos != (ast.Pos{Line: 2, Col: 13}) || errs[0].Msg != "undefined rule undefined" {
			t.Errorf("%s: %T %v, want grammar.CheckErrors", name, err, err)
		}
	}
}
