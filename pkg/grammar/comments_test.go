package grammar_test

import (
	"errors"
	"strings"
	"testing"

	"syntaxir.dev/sxir/pkg/grammar"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// TestFormatCommentsParseBack formats the comments of a grammar decoded from
// JSON, which has no layout, so Format writes them from their strings, and
// checks that the text is the source again.
func TestFormatCommentsParseBack(t *testing.T) {
	for _, src := range []string{
		"/* a /* b */ c */\nrule r = \"a\"\n",
		"/* a /* b /* c */ */ d */\nrule r = \"a\"\n",
		"/* a\n/b */\nrule r = \"a\"\n",
		"rule r = \"a\" /* a /* b */ c */\n",
	} {
		g, err := grammar.Load(grammar.FromString("g.sxir", src))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		data, err := ast.MarshalJSON(g)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := ast.UnmarshalJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		if got := grammar.Format(decoded); got != src {
			t.Errorf("%q: formatted from JSON as\n%q", src, got)
		}
	}
}

// TestFormatHandWrittenCommentsParseBack formats comment strings set by hand
// that hold block comment delimiters, and checks that the text parses and reads
// back the string with its unpaired delimiters broken apart.
func TestFormatHandWrittenCommentsParseBack(t *testing.T) {
	for _, tc := range []struct{ text, back string }{
		{" a /* b */ c ", " a /* b */ c "},
		{" a /* b ", " a / * b "},
		{" a */ b ", " a * / b "},
		{" /* /* b */ ", " / * /* b */ "},
		{"*/*", "* / * "},
		{"/*/", "/ * / "},
		{" a\n/b ", " a\n/b "},
		{" a\n */ b ", " a\n * / b "},
	} {
		r := &ast.RuleDef{Name: "r", Expr: &ast.Literal{Value: "a"}}
		r.LeadingComments = tc.text
		text := grammar.Format(&ast.Grammar{File: ast.File{Decls: []ast.Decl{r}}})
		back, err := grammar.Load(grammar.FromString("g.sxir", text))
		if err != nil {
			t.Errorf("%q: the formatted grammar does not parse: %v\n%s", tc.text, err, text)
			continue
		}
		rules := back.Rules()
		if len(rules) != 1 || rules[0].LeadingComments != tc.back {
			t.Errorf("%q: formatted as %q, which reads back %q, want %q", tc.text, text, leading(rules), tc.back)
		}
	}
}

func leading(rules []*ast.RuleDef) string {
	if len(rules) == 0 {
		return ""
	}
	return rules[0].LeadingComments
}

// TestValidateHeaderComments checks the header comments of each declaration and of a struct field:
// one that holds a line break is invalid, as its header stands on one line and the parser reads
// no line break there; any other, the empty one included, is written so that it reads back.
func TestValidateHeaderComments(t *testing.T) {
	rule := func() *ast.RuleDef { return &ast.RuleDef{Name: "m", Expr: &ast.Literal{Value: "a"}} }
	field := func(h []string) *ast.TypeDef {
		return &ast.TypeDef{Name: "T", Type: &ast.StructType{Fields: []*ast.Field{{Name: "A", Type: &ast.TypeRef{Name: "int"}, HeaderComments: h}}}}
	}
	for _, c := range []struct {
		name, path string
		g          func(h []string) *ast.Grammar
		// header returns the header comments of the grammar read back.
		header func(g *ast.Grammar) []string
	}{
		{"package", "$.file.pkg.headerComments[0]",
			func(h []string) *ast.Grammar {
				return &ast.Grammar{File: ast.File{Pkg: ast.Pkg{Name: "p", HeaderComments: h}, Decls: []ast.Decl{rule()}}}
			},
			func(g *ast.Grammar) []string { return g.File.Pkg.HeaderComments }},
		{"rule", "$.file.decls[0].headerComments[0]",
			func(h []string) *ast.Grammar {
				r := rule()
				r.HeaderComments = h
				return &ast.Grammar{File: ast.File{Decls: []ast.Decl{r}}}
			},
			func(g *ast.Grammar) []string { return g.File.Decls[0].(*ast.RuleDef).HeaderComments }},
		{"type", "$.file.decls[0].headerComments[0]",
			func(h []string) *ast.Grammar {
				d := field(nil)
				d.HeaderComments = h
				return &ast.Grammar{File: ast.File{Decls: []ast.Decl{d}}}
			},
			func(g *ast.Grammar) []string { return g.File.Decls[0].(*ast.TypeDef).HeaderComments }},
		{"alias", "$.file.decls[0].headerComments[0]",
			func(h []string) *ast.Grammar {
				return &ast.Grammar{File: ast.File{Decls: []ast.Decl{&ast.TypeAlias{Name: "T", Type: &ast.TypeRef{Name: "int"}, HeaderComments: h}}}}
			},
			func(g *ast.Grammar) []string { return g.File.Decls[0].(*ast.TypeAlias).HeaderComments }},
		{"var", "$.file.decls[0].headerComments[0]",
			func(h []string) *ast.Grammar {
				return &ast.Grammar{File: ast.File{Decls: []ast.Decl{&ast.VarDef{Name: "v", Type: &ast.TypeRef{Name: "int"}, HeaderComments: h}}}}
			},
			func(g *ast.Grammar) []string { return g.File.Decls[0].(*ast.VarDef).HeaderComments }},
		{"field", "$.file.decls[0].type.fields[0].headerComments[0]",
			func(h []string) *ast.Grammar { return &ast.Grammar{File: ast.File{Decls: []ast.Decl{field(h)}}} },
			func(g *ast.Grammar) []string {
				return g.File.Decls[0].(*ast.TypeDef).Type.(*ast.StructType).Fields[0].HeaderComments
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, text := range []string{" a\n", " a\n b ", "\nx", "\n", " a\n b\n"} {
				err := grammar.Validate(c.g([]string{text}))
				var verr *ast.ValidationError
				if !errors.As(err, &verr) || verr.Path != c.path || !strings.Contains(verr.Msg, "line break") {
					t.Errorf("%q: validation gives %v, want a line break at %s", text, err, c.path)
				}
			}
			for _, text := range []string{" a ", "", "a", " /* b */ "} {
				g := c.g([]string{text})
				if err := grammar.Validate(g); err != nil {
					t.Errorf("%q: %v", text, err)
					continue
				}
				src := grammar.Format(g)
				back, err := grammar.Load(grammar.FromString("g.sxir", src))
				if err != nil {
					t.Errorf("%q: reading %q: %v", text, src, err)
					continue
				}
				if got := c.header(back); len(got) != 1 || got[0] != text && got[0] != text+" " {
					t.Errorf("%q: written %q, which reads back the header comments %q", text, src, got)
				}
			}
		})
	}
}
