package engine

import (
	"fmt"
	"strings"
	"testing"

	checkpkg "syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/testing/codegentest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestRawInvalidASTLiterals requires compilation to reject a grammar AST whose literal or string
// constant is not valid UTF-8, which only an AST built in Go can hold, with the validation error
// at the path of that string, and without changing the AST. The literal of its replacement characters
// compiles; it matches each invalid input byte (TestInvalidUTF8LiteralMatching).
func TestRawInvalidASTLiterals(t *testing.T) {
	t.Parallel()
	for _, raw := range codegentest.RawLiteralValues() {
		for _, shape := range []string{`t:"�"`, `t:"�" .`, `t:("�" / "other")`, `t:("�" #repeat(2)) #atomic`} {
			t.Run(fmt.Sprintf("%x/%s", raw, shape), func(t *testing.T) {
				src := "type N = { T node[string]; S string }\nrule main N = " + shape + ` $$ => N{T:$t, S:"constant"}`
				rejected := func(g *ast.Grammar, path string, value func() string) {
					t.Helper()
					_, err := Compile(g, checkpkg.Options{})
					if err == nil || !strings.Contains(err.Error(), " "+path) || !strings.Contains(err.Error(), "not valid UTF-8") {
						t.Fatalf("got %v; want the invalid UTF-8 error of a string under %s", err, path)
					}
					if value() != raw {
						t.Fatal("Compile changed the AST")
					}
				}
				g := mustParse(src)
				lit := codegentest.ReplaceFirstLiteral(g.Rules()[0].Expr, raw)
				if lit == nil {
					t.Fatal("missing placeholder")
				}
				rejected(g, "$.file.decls[1].expr", func() string { return lit.Value })

				g = mustParse(src)
				constant := g.Rules()[0].Action.(*ast.Constructor).Fields[1].Value.(*ast.StringLit)
				constant.Value = raw
				rejected(g, "$.file.decls[1].action", func() string { return constant.Value })

				canonical := mustParse(src)
				codegentest.ReplaceFirstLiteral(canonical.Rules()[0].Expr, string([]rune(raw)))
				if _, err := Compile(canonical, checkpkg.Options{}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
