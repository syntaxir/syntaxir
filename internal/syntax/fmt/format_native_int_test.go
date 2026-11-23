package fmt_test

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	syntaxfmt "syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
	"syntaxir.dev/sxir/pkg/runtime/input"
)

// TestFormatNativeMinimumRoundTrip checks that the formatted minimum native integer, alone and in
// arithmetic, parses back to the same text and computes the same value on every backend, from the
// grammar, its JSON AST and its formatted source, compiled and loaded.
func TestFormatNativeMinimumRoundTrip(t *testing.T) {
	max := int(^uint(0) >> 1)
	minimum := &ast.IntLit{Value: -max - 1}
	for _, tc := range []struct {
		name       string
		actionExpr ast.ActionExpr
		want       int
	}{
		{"literal", minimum, minimum.Value},
		{"negated", &ast.Unary{Op: "-", X: minimum}, minimum.Value},
		{"left addition", &ast.Binary{Op: "+", L: minimum, R: &ast.IntLit{Value: 1}}, minimum.Value + 1},
		{"right subtraction", &ast.Binary{Op: "-", L: &ast.IntLit{Value: 1}, R: minimum}, minimum.Value + 1},
		{"left division", &ast.Binary{Op: "/", L: minimum, R: &ast.IntLit{Value: 2}}, minimum.Value / 2},
		{"right multiplication", &ast.Binary{Op: "*", L: &ast.IntLit{Value: 2}, R: minimum}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := enginetest.ParseGrammar("type N = { V int }\n@start\nrule main = \"é\" $$ => N{V: 0}")
			if err != nil {
				t.Fatal(err)
			}
			g.Rules()[0].Action.(*ast.Constructor).Fields[0].Value = tc.actionExpr
			data, err := ast.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			jsonAST, err := ast.UnmarshalJSON(data)
			if err != nil {
				t.Fatal(err)
			}
			source := syntaxfmt.Format(jsonAST)
			sourceAST, err := enginetest.ParseGrammar(source)
			if err != nil {
				t.Fatalf("formatted source cannot be parsed: %v\n%s", err, source)
			}
			if again := syntaxfmt.Format(sourceAST); again != source {
				t.Fatalf("format is not idempotent:\n%s\n%s", source, again)
			}
			for _, tree := range []*ast.Grammar{g, jsonAST, sourceAST} {
				p, err := enginetest.Compile(tree, "main")
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := p.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := engine.LoadParser(encoded)
				if err != nil {
					t.Fatal(err)
				}
				for _, parser := range []*engine.Parser{p, loaded} {
					for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
						for _, unit := range []input.PositionUnit{input.CodePoints, input.Bytes} {
							n, err := parser.Parse("é", engine.WithBackend(backend), engine.WithUnit(unit))
							if parser == loaded && backend == engine.Closure {
								// A compiled grammar runs on the bytecode backends only.
								if err == nil {
									t.Fatalf("closure backend on a compiled grammar: %v", n)
								}
								continue
							}
							want := fmt.Sprintf("N{V: %d}", tc.want)
							if err != nil || n.String() != want {
								t.Fatalf("backend %v, unit %v: %v, %v; want %s", backend, unit, n, err, want)
							}
						}
					}
				}
			}
		})
	}
}
