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
	source := fmt.Format(entry.Checked(before).Grammar)
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
// action expression replaced by its contents and each negation of an integer
// literal by the negative literal.
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
