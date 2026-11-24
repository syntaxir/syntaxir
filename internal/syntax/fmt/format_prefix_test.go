package fmt_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestFormatPrefixOperandOfPostfix checks that a prefix operator or a negative
// literal under a field access, an index or a cast keeps its place: the
// formatted grammar parses back to the same tree, apart from the parentheses
// that formatting adds and the parser keeps, and from a negative literal that
// the parser reads as a negation.
func TestFormatPrefixOperandOfPostfix(t *testing.T) {
	t.Parallel()
	x := func() ast.ActionExpr { return &ast.CaptureRef{Name: "x"} }
	minimum := -int(^uint(0)>>1) - 1
	for _, tc := range []struct {
		actionExpr ast.ActionExpr
		want       string
	}{
		{&ast.FieldAccess{X: &ast.Unary{Op: "-", X: x()}, Name: "Start"}, "(-$x).Start"},
		{&ast.FieldAccess{X: &ast.Unary{Op: "!", X: x()}, Name: "Start"}, "(!$x).Start"},
		{&ast.Binary{Op: "[]", L: &ast.Unary{Op: "-", X: x()}, R: &ast.IntLit{Value: 0}}, "(-$x)[0]"},
		{&ast.Cast{X: &ast.Unary{Op: "-", X: x()}, Type: &ast.TypeRef{Name: "int"}}, "(-$x).(int)"},
		{&ast.FieldAccess{X: &ast.IntLit{Value: -5}, Name: "Start"}, "(-5).Start"},
		{&ast.Binary{Op: "[]", L: &ast.IntLit{Value: -5}, R: &ast.IntLit{Value: 0}}, "(-5)[0]"},
		{&ast.Cast{X: &ast.IntLit{Value: -5}, Type: &ast.TypeRef{Name: "int"}}, "(-5).(int)"},
		{&ast.Cast{X: &ast.IntLit{Value: 5}, Type: &ast.TypeRef{Name: "int"}}, "(5).(int)"},
		{&ast.Cast{X: &ast.IntLit{Value: minimum}, Type: &ast.TypeRef{Name: "int"}}, "(" + strconv.Itoa(minimum) + ").(int)"},
		{&ast.Unary{Op: "-", X: &ast.FieldAccess{X: x(), Name: "Start"}}, "-$x.Start"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := fmt.FormatActionExpr(tc.actionExpr); got != tc.want {
				t.Fatalf("FormatActionExpr: got %q; want %q", got, tc.want)
			}
			g, err := enginetest.ParseGrammar("@start\nrule main = x:\"a\" => 0")
			if err != nil {
				t.Fatal(err)
			}
			g.Rules()[0].Action = tc.actionExpr
			source := fmt.Format(g)
			again, err := enginetest.ParseGrammar(source)
			if err != nil {
				t.Fatalf("formatted source cannot be parsed: %v\n%s", err, source)
			}
			if a, b := normalActions(t, grammarWithoutSources(t, g)), normalActions(t, grammarWithoutSources(t, again)); a != b {
				t.Fatalf("formatting changes the tree:\n%s\n%s\nsource:\n%s", a, b, source)
			}
		})
	}
}

// TestFormatMinimumIntegerVariable checks that the minimum int as the initial
// value of a variable, which must be a literal, stays one after formatting.
func TestFormatMinimumIntegerVariable(t *testing.T) {
	t.Parallel()
	src := "var v int = " + strconv.Itoa(-int(^uint(0)>>1)-1) + "\n\ntype N = { V int }\n\n@start\nrule main N = \"a\" => N{V: v}\n"
	before := enginetest.MustCompile(t, src, check.Options{})
	source := fmt.Format(entry.Grammar(before))
	if source != src {
		t.Errorf("Format: got\n%s\nwant\n%s", source, src)
	}
	after := enginetest.MustCompile(t, source, check.Options{})
	n1, err1 := before.Parse("main", "a")
	n2, err2 := after.Parse("main", "a")
	if err1 != nil || err2 != nil || n1.String() != n2.String() {
		t.Fatalf("formatting changes the result: %v, %v; %v, %v", n1, err1, n2, err2)
	}
}

// normalActions returns the JSON form of a grammar with each parenthesized
// expression, action expression or type replaced by its contents and each
// negation of an integer literal by the negative literal.
func normalActions(t *testing.T, data string) string {
	var v any
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		t.Fatal(err)
	}
	var normal func(any) any
	normal = func(v any) any {
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				v[k] = normal(x)
			}
			switch v["@type"] {
			case "ActionGroup":
				return v["action"]
			case "Group":
				return v["expr"]
			case "TypeGroup":
				return v["type"]
			case "Unary":
				if x, ok := v["x"].(map[string]any); ok && v["op"] == "-" && x["@type"] == "IntLit" {
					x["value"] = -x["value"].(float64)
					return x
				}
			}
			return v
		case []any:
			for i, x := range v {
				v[i] = normal(x)
			}
		}
		return v
	}
	out, err := json.Marshal(normal(v))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestFormatKeepsNodes checks that formatting keeps nodes that the syntax
// could merge: the choice of a grouped sequence, which is one element, an
// attributed expression with attributes of its own, and a meet inside a meet.
// The formatted grammar parses back to the same tree apart from parentheses
// and the `/ _|_` that keeps the choice, and computes the same value.
func TestFormatKeepsNodes(t *testing.T) {
	t.Parallel()
	lit := func(s string) ast.Expr { return &ast.Literal{Value: s} }
	choice := &ast.Choice{Alts: []ast.Expr{&ast.Group{Expr: &ast.Seq{Items: []ast.Expr{lit("a"), lit("b")}}}}}
	for _, tc := range []struct {
		name  string
		rule  *ast.RuleDef
		input string
		want  string
	}{
		{"grouped sequence", &ast.RuleDef{Attrs: []*ast.Attribute{{Name: "start"}}, Name: "main", Expr: &ast.Seq{Items: []ast.Expr{choice, lit("c")}}}, "abc",
			"@start\nrule main = ((\n    \"a\" \"b\"\n) / _|_) \"c\""},
		{"attributes", &ast.RuleDef{Name: "main", Expr: &ast.Attributed{
			Expr:  &ast.Attributed{Expr: lit("a"), Attrs: []*ast.Attribute{{Name: "x"}}},
			Attrs: []*ast.Attribute{{Name: "y"}}}}, "",
			`rule main = ("a" @x) @y`},
		{"meet", &ast.RuleDef{Name: "main", Type: &ast.MeetType{Types: []ast.TypeExpr{
			&ast.TypeRef{Name: "_"}, &ast.MeetType{Types: []ast.TypeExpr{&ast.TypeRef{Name: "_"}, &ast.TypeRef{Name: "_"}}}}},
			Expr: lit("a")}, "",
			`rule main _ & (_ & _) = "a"`},
		{"literal-named references in attribute arguments", &ast.RuleDef{Name: "main", Expr: &ast.Attributed{
			Expr: lit("a"),
			Attrs: []*ast.Attribute{{Name: "x", Args: []any{&ast.Ref{Name: "nil"}, &ast.Seq{Items: []ast.Expr{&ast.Ref{Name: "false"}, lit("b")}}},
				KwArgs: map[string]any{"k": &ast.Optional{Expr: &ast.Ref{Name: "true"}}}}}}}, "",
			`rule main = "a" @x((nil), (false "b"), k=(true?))`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &ast.Grammar{File: ast.File{Decls: []ast.Decl{tc.rule}}}
			source := fmt.Format(g)
			if source != tc.want+"\n" {
				t.Fatalf("Format: got %q; want %q", source, tc.want+"\n")
			}
			again, err := enginetest.ParseGrammar(source)
			if err != nil {
				t.Fatalf("formatted source cannot be parsed: %v\n%s", err, source)
			}
			if a, b := normalActions(t, grammarWithoutSources(t, g)), normalActions(t, grammarWithoutSources(t, again)); a != b && tc.name != "grouped sequence" {
				t.Fatalf("formatting changes the tree:\n%s\n%s", a, b)
			}
			if tc.input == "" {
				return
			}
			before, err := enginetest.Compile(g, "main")
			if err != nil {
				t.Fatal(err)
			}
			after, err := enginetest.Compile(again, "main")
			if err != nil {
				t.Fatal(err)
			}
			n1, err1 := before.Parse(tc.input)
			n2, err2 := after.Parse(tc.input)
			if err1 != nil || err2 != nil || n1.String() != n2.String() {
				t.Fatalf("formatting changes the value: %v, %v; %v, %v", n1, err1, n2, err2)
			}
		})
	}
}

// TestFormatLiteralAttributeArgument checks that an attribute argument written as a case-sensitive
// literal alone, which would read back as a string, is written in parentheses, and that a literal
// that ignores case and a grouped literal keep their spelling.
func TestFormatLiteralAttributeArgument(t *testing.T) {
	t.Parallel()
	one := &ast.Choice{Alts: []ast.Expr{&ast.Literal{Value: "m"}}}
	g := &ast.Grammar{File: ast.File{Decls: []ast.Decl{&ast.RuleDef{Name: "main", Expr: &ast.Attributed{
		Expr: &ast.Literal{Value: "a"},
		Attrs: []*ast.Attribute{{Name: "x", Args: []any{one, &ast.Literal{Value: "i", IgnoreCase: true}},
			KwArgs: map[string]any{"k": &ast.Group{Expr: &ast.Literal{Value: "n"}}}}},
	}}}}}
	source := fmt.Format(g)
	back, err := enginetest.ParseGrammar(source)
	if err != nil {
		t.Fatalf("formatted source cannot be parsed: %v\n%s", err, source)
	}
	a := back.Rules()[0].Expr.(*ast.Attributed).Attrs[0]
	if group, ok := a.Args[0].(*ast.Group); !ok || group.Expr.(*ast.Literal).Value != "m" {
		t.Errorf("one-alternative choice of a literal: got %#v\n%s", a.Args[0], source)
	}
	if lit, ok := a.Args[1].(*ast.Literal); !ok || !lit.IgnoreCase {
		t.Errorf("literal that ignores case: got %#v\n%s", a.Args[1], source)
	}
	if group, ok := a.KwArgs["k"].(*ast.Group); !ok || group.Expr.(*ast.Literal).Value != "n" {
		t.Errorf("grouped literal: got %#v\n%s", a.KwArgs["k"], source)
	}
}
