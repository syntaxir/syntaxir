package fmt

import (
	"testing"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// A sequence of one item is a tuple of one element and a choice of one alternative is one element
// that keeps the scope of the alternative's cuts, where the bare item and a group splice or have
// the item's own value. The printed text spells the difference with `_` and `_|_`, which have no
// cost, so that it reads back as the same value.
func TestFormatKeepsShapeOfOneItemSequencesAndChoices(t *testing.T) {
	lit := func(s string) ast.Expr { return &ast.Literal{Value: s} }
	one := func(e ast.Expr) ast.Expr { return &ast.Seq{Items: []ast.Expr{e}} }
	two := &ast.Seq{Items: []ast.Expr{lit("b"), lit("c")}}
	for _, tc := range []struct {
		name string
		e    ast.Expr
		want string
	}{
		{"one-item sequence", one(lit("x")), `"x" _`},
		{"one-item sequence in a sequence", &ast.Seq{Items: []ast.Expr{lit("a"), one(lit("x"))}}, `"a" ("x" _)`},
		{"one-item sequence in a repetition", &ast.Repeat{Expr: one(lit("x"))}, `("x" _) #repeat(0)`},
		{"choice of one sequence", &ast.Choice{Alts: []ast.Expr{two}}, `"b" "c" / _|_`},
		{"choice of one sequence in a sequence", &ast.Seq{Items: []ast.Expr{lit("a"), &ast.Choice{Alts: []ast.Expr{two}}}}, `"a" ("b" "c" / _|_)`},
		{"choice of one one-item sequence in a sequence", &ast.Seq{Items: []ast.Expr{lit("a"), &ast.Choice{Alts: []ast.Expr{one(lit("x"))}}}}, `"a" ("x" _ / _|_)`},
		{"choice of one item", &ast.Choice{Alts: []ast.Expr{lit("x")}}, `"x" / _|_`},
		{"choice of one item in a sequence", &ast.Seq{Items: []ast.Expr{lit("a"), &ast.Choice{Alts: []ast.Expr{lit("x")}}}}, `"a" ("x" / _|_)`},
	} {
		if got := FormatExpr(tc.e); got != tc.want {
			t.Errorf("%s: FormatExpr = %s, want %s", tc.name, got, tc.want)
		}
		g := &ast.Grammar{File: ast.File{Decls: []ast.Decl{&ast.RuleDef{Name: "main", Expr: tc.e}}}}
		if got, want := Format(g), "rule main = "+tc.want+"\n"; got != want {
			t.Errorf("%s: Format = %q, want %q", tc.name, got, want)
		}
	}
}
