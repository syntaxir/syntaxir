package parser_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func TestRepetitionPragmaBounds(t *testing.T) {
	for _, tc := range []struct {
		source   string
		args     []any
		min, max int
		input    string
	}{
		{`"a" #repeat(2)`, []any{2}, 2, 2, "aa"},
		{`"a" #repeat(2, 3)`, []any{2, 3}, 2, 3, "aaa"},
		{`"a" #repeat(, 3)`, []any{nil, 3}, 0, 3, "aa"},
		{`"a" #repeat(2, )`, []any{2, nil}, 2, -1, "aaaa"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			g, err := parser.Parse("@start\nrule main = " + tc.source)
			if err != nil {
				t.Fatal(err)
			}
			assertBounds := func(g *ast.Grammar) {
				t.Helper()
				rep := g.Rules()[0].Expr.(*ast.Repeat)
				if rep.Min != tc.min || rep.Max != tc.max {
					t.Fatalf("bounds %d,%d", rep.Min, rep.Max)
				}
			}
			assertBounds(g)
			before, err := ast.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := ast.UnmarshalJSON(before)
			if err != nil {
				t.Fatal(err)
			}
			assertBounds(decoded)
			formatted := fmt.Format(decoded)
			reparsed, err := parser.Parse(formatted)
			if err != nil {
				t.Fatal(err)
			}
			assertBounds(reparsed)
			p, err := entry.Compile(g, entry.Options{})
			if err != nil {
				t.Fatal(err)
			}
			rep := entry.Checked(p).Grammar.Rules()[0].Expr.(*ast.Repeat)
			if rep.Min != tc.min || rep.Max != tc.max {
				t.Fatalf("normalized bounds %d,%d", rep.Min, rep.Max)
			}
			after, _ := ast.MarshalJSON(g)
			if !bytes.Equal(before, after) {
				t.Fatal("compilation mutated source AST")
			}
			data, err := p.MarshalBinary("main")
			if err != nil {
				t.Fatal(err)
			}
			loaded, start, err := engine.LoadProgram(data)
			if err != nil {
				t.Fatal(err)
			}
			var expected string
			for _, program := range []*engine.Program{p, loaded} {
				for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
					n, err := program.ParseWith(start, tc.input, engine.ParseOptions{Backend: backend})
					if program == loaded && backend == engine.Closure {
						// A compiled grammar runs on the bytecode backends only.
						if err == nil {
							t.Fatal("closure backend on a compiled grammar")
						}
						continue
					}
					if err != nil {
						t.Fatal(err)
					}
					if expected == "" {
						expected = n.String()
					} else if n.String() != expected {
						t.Fatalf("CST differs: %s != %s", n.String(), expected)
					}
				}
			}
		})
	}
}

func TestAttributeArgumentBinding(t *testing.T) {
	for _, src := range []string{
		`rule main = "a" @error("expected a")`,
		`rule main = "a" @error(message="expected a")`,
		`rule main = "a" #recover(skip="b")`,
		`rule main = "a" #recover(skip="b")`,
		`rule main = [a-z] #repeat(2) #atomic`,
		`rule main = ("a" #atomic) #repeat(2)`,
		`rule main = "a" #repeat(2) #repeat(1)`,
	} {
		g, err := parser.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Compile(starttest.MarkAll(g), entry.Options{}); err != nil {
			t.Fatalf("%s: %v", src, err)
		}
	}
	for _, src := range []string{
		`rule main = "a" #repeat(min=2)`,
		`rule main = "a" #repeat(,)`,
		`rule main = "a" #repeat(-1)`,
		`rule main = "a" #repeat(2,-1)`,
		`rule main = "a" #repeat(,-2)`,
		`rule main = "a" #repeat(2,1)`,
		`rule main = "a" #repeat("two")`,
		`rule main = "a" @error("x",message="y")`,
		`rule main = "a" #atomic(1)`,
	} {
		g, err := parser.Parse(src)
		if err != nil {
			continue
		}
		if _, err := entry.Compile(starttest.MarkAll(g), entry.Options{}); err == nil {
			t.Fatalf("accepted %s", src)
		}
	}
	for _, src := range []string{
		`rule main = "a" @error(message="x", "y")`,
		`rule main = "a" @error(message="x",message="y")`,
	} {
		if _, err := parser.Parse(src); err == nil {
			t.Fatalf("accepted %s", src)
		}
	}
}

func TestAttributeScalarJSON(t *testing.T) {
	g, err := parser.Parse(`rule main = "a" @custom(1, "text", nil, flag=true, missing=nil, expr=(a / b))`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := ast.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"kwArgs"`) {
		t.Fatal("keyword arguments absent")
	}
	decoded, err := ast.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	formatted := fmt.Format(decoded)
	g2, err := parser.Parse(formatted)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.FormatExpr(decoded.Rules()[0].Expr) != fmt.FormatExpr(g2.Rules()[0].Expr) {
		t.Fatalf("argument structure changed after formatting: %s", formatted)
	}
}

func TestAttributeOmittedSlots(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   []any
	}{{"@custom(2)", []any{2}}, {"@custom(2, )", []any{2}}, {"@custom(nil, 2)", []any{nil, 2}},
		{"@custom(2, nil)", []any{2, nil}}, {"@custom(2, nil,)", []any{2, nil}}, {"@custom(nil)", []any{nil}}} {
		g, err := parser.Parse("rule main = \"a\" " + tc.source)
		if err != nil {
			t.Fatal(err)
		}
		a := g.Rules()[0].Expr.(*ast.Attributed).Attrs[0]
		if !reflect.DeepEqual(a.Args, tc.want) {
			t.Fatalf("%#v", a.Args)
		}
		data, err := ast.MarshalJSON(g)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := ast.DecodeJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		again, err := parser.Parse(fmt.Format(decoded))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(again.Rules()[0].Expr.(*ast.Attributed).Attrs[0].Args, tc.want) {
			t.Fatal("lost slots")
		}
	}
}

func TestAttributeEmptySlotsAreErrors(t *testing.T) {
	for _, source := range []string{"@custom(, 2)", "@custom(,)", "@custom(2,,)", "@custom(2, , 3)", "@custom(k=1, 2)"} {
		if _, err := parser.Parse("rule main = \"a\" " + source); err == nil {
			t.Fatalf("%s parsed", source)
		}
	}
}

func TestAttributeNativeIntegerRoundTrip(t *testing.T) {
	maximum := int(^uint(0) >> 1)
	minimum := -maximum - 1
	g := &ast.Grammar{File: ast.File{Decls: []ast.Decl{&ast.RuleDef{
		Name: "main", Expr: &ast.Attributed{Expr: &ast.Literal{Value: "a"},
			Attrs: []*ast.Attribute{{Name: "custom", Args: []any{minimum, maximum, -1}, KwArgs: map[string]any{"minimum": minimum}}}},
	}}}}
	source := fmt.Format(g)
	parsed, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("formatted native integers: %v\n%s", err, source)
	}
	got := parsed.Rules()[0].Expr.(*ast.Attributed).Attrs[0]
	if !reflect.DeepEqual(got.Args, []any{minimum, maximum, -1}) || got.KwArgs["minimum"] != minimum {
		t.Fatalf("integer arguments changed: %+v", got)
	}
}

func TestAttributeEqualsLiteralIsAnExpressionArgument(t *testing.T) {
	g, err := parser.Parse(`rule main = "a" @custom(x "=")`)
	if err != nil {
		t.Fatal(err)
	}
	a := g.Rules()[0].Expr.(*ast.Attributed).Attrs[0]
	if len(a.Args) != 1 || len(a.KwArgs) != 0 {
		t.Fatalf("arguments: %+v", a)
	}
	seq, ok := a.Args[0].(*ast.Seq)
	if !ok || len(seq.Items) != 2 {
		t.Fatalf("argument: %#v", a.Args[0])
	}
	if ref, ok := seq.Items[0].(*ast.Ref); !ok || ref.Name != "x" {
		t.Fatalf("reference: %#v", seq.Items[0])
	}
	if lit, ok := seq.Items[1].(*ast.Literal); !ok || lit.Value != "=" {
		t.Fatalf("literal: %#v", seq.Items[1])
	}
}

// TestIgnoreCaseAttributeArgument checks that a string literal with the
// suffix i is an expression argument, a literal that ignores case, in both
// parsers, and that it survives formatting; a literal without the suffix is a
// string.
func TestIgnoreCaseAttributeArgument(t *testing.T) {
	const src = "@start\nrule main = \"a\" @x(\"q\", \"p\"i, k=\"r\"i)\n"
	for _, p := range []struct {
		name  string
		parse func(string) (*ast.Grammar, error)
	}{{"generated", parser.Parse}, {"reference", parser.ParseReference}} {
		t.Run(p.name, func(t *testing.T) {
			check := func(g *ast.Grammar) {
				t.Helper()
				a := g.Rules()[0].Expr.(*ast.Attributed).Attrs[0]
				if a.Args[0] != "q" {
					t.Fatalf("plain literal: got %#v; want the string", a.Args[0])
				}
				for _, v := range []any{a.Args[1], a.KwArgs["k"]} {
					if lit, ok := v.(*ast.Literal); !ok || !lit.IgnoreCase {
						t.Fatalf("got %#v; want a literal that ignores case", v)
					}
				}
			}
			g, err := p.parse(src)
			if err != nil {
				t.Fatal(err)
			}
			check(g)
			formatted := fmt.Format(g)
			if formatted != src {
				t.Fatalf("Format: got %q; want %q", formatted, src)
			}
			again, err := p.parse(formatted)
			if err != nil {
				t.Fatal(err)
			}
			check(again)
		})
	}
}
