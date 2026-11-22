package check_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/internal/driver/checked"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func TestMalformedASTEntryPointsAndPosition(t *testing.T) {
	t.Parallel()
	codegentest.EachMode(t, testMalformedASTEntryPointsAndPosition)
}

func testMalformedASTEntryPointsAndPosition(t *testing.T) {
	e := &ast.Optional{SourceInfo: ast.SourceInfo{Pos: ast.Pos{Line: 3, Col: 4}}}
	e.Expr = e
	g := &ast.Grammar{File: ast.File{Decls: []ast.Decl{&ast.RuleDef{Name: "p", Expr: e}}}}
	for name, run := range map[string]func() error{
		"compile":           func() error { _, err := entry.Compile(g, entry.Options{}); return err },
		"saved AST rebuild": func() error { _, err := checked.Check(g, checked.Options{}, []check.RuleFlags{{}}, nil); return err },
		"Go generator": func() error {
			_, err := codegentest.GenerateCode(t, g, common.GenOptions{Start: "p", Package: "p"})
			return err
		},
		"typed Go generator": func() error {
			_, err := codegentest.GenerateCode(t, g, common.GenOptions{Start: "p", Package: "p", Types: true})
			return err
		},
		"TS generator": func() error { _, err := codegentest.GenerateTSBundle(g, common.GenOptions{Start: "p"}); return err },
	} {
		t.Run(name, func(t *testing.T) {
			err := run()
			if errs, ok := err.(diagnostic.ErrorList); !ok || len(errs) != 1 || errs[0].Pos != e.Pos || !strings.Contains(err.Error(), "$.file.decls[0].expr.expr") {
				t.Fatalf("got %v; want positioned pointer-cycle error", err)
			}
		})
	}
}
