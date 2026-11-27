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
// one that holds a line terminator (LF, CR or CRLF) is invalid, as its header stands on one line
// and the parser reads no line terminator there; any other, the empty one included, is written so that it reads back.
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
			for _, text := range []string{" a\n", " a\n b ", "\nx", "\n", " a\n b\n", "\r", "a\rb", " a\r\n", "\r\n", " a\r b "} {
				err := grammar.Validate(c.g([]string{text}))
				var verr *ast.ValidationError
				if !errors.As(err, &verr) || verr.Path != c.path || !strings.Contains(verr.Msg, "line terminator") {
					t.Errorf("%q: validation gives %v, want a line terminator at %s", text, err, c.path)
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

// TestCommentLineTerminators checks every comment string of an AST with each line terminator of
// the lexical specification, LF, CR and CRLF: a string in a header, which stands on one line, is
// invalid when it holds one, and any other string validates, and Format writes it so that it reads
// back as the same string. Format ends every line that it writes with LF, so the string reads back
// with LF where it had CR or CRLF. The slots are those of TestValidateHeaderComments and every
// owned comment string that Format writes, with the comments of a literal.
func TestCommentLineTerminators(t *testing.T) {
	lit := func() *ast.Literal { return &ast.Literal{Value: "a"} }
	gr := func(d ast.Decl) *ast.Grammar { return &ast.Grammar{File: ast.File{Decls: []ast.Decl{d}}} }
	rule := func(g *ast.Grammar) *ast.RuleDef { return g.File.Decls[0].(*ast.RuleDef) }
	typeDef := func(g *ast.Grammar) *ast.TypeDef { return g.File.Decls[0].(*ast.TypeDef) }
	fieldOf := func(g *ast.Grammar) *ast.Field { return typeDef(g).Type.(*ast.StructType).Fields[0] }
	one := func(s string) []string { return []string{s} }
	attrs := func() []*ast.Attribute { return []*ast.Attribute{{Name: "a"}} }
	withPkg := func(set func(p *ast.Pkg)) *ast.Grammar {
		g := gr(&ast.RuleDef{Name: "m", Expr: lit()})
		g.File.Pkg.Name = "p"
		g.File.Pkg.Attrs = attrs()
		set(&g.File.Pkg)
		return g
	}
	for _, c := range []struct {
		name string
		g    func(s string) *ast.Grammar
		// path is the path of the error of a string with a line terminator in a header, or ""
		// where the string is valid; got returns the strings of the slot in the grammar read back,
		// and is nil for the comments of an expression, which have no place in the formatted output
		// without layout, so that output must only read back.
		path string
		got  func(g *ast.Grammar) []string
	}{
		{"rule leading", func(s string) *ast.Grammar {
			r := &ast.RuleDef{Name: "m", Expr: lit()}
			r.LeadingComments = s
			return gr(r)
		},
			"", func(g *ast.Grammar) []string { return one(rule(g).LeadingComments) }},
		{"rule detached", func(s string) *ast.Grammar {
			r := &ast.RuleDef{Name: "m", Expr: lit()}
			r.LeadingDetachedComments = one(s)
			return gr(r)
		}, "", func(g *ast.Grammar) []string { return rule(g).LeadingDetachedComments }},
		{"rule trailing", func(s string) *ast.Grammar {
			r := &ast.RuleDef{Name: "m", Expr: lit()}
			r.TrailingComments = one(s)
			return gr(r)
		},
			"", func(g *ast.Grammar) []string { return rule(g).TrailingComments }},
		{"rule header", func(s string) *ast.Grammar { return gr(&ast.RuleDef{Name: "m", Expr: lit(), HeaderComments: one(s)}) },
			"$.file.decls[0].headerComments[0]", nil},
		{"rule second header", func(s string) *ast.Grammar {
			return gr(&ast.RuleDef{Name: "m", Expr: lit(), HeaderComments: []string{"x", s}})
		}, "$.file.decls[0].headerComments[1]", nil},
		{"rule keyword", func(s string) *ast.Grammar {
			return gr(&ast.RuleDef{Name: "m", Expr: lit(), Attrs: attrs(), KeywordComments: s})
		}, "", func(g *ast.Grammar) []string { return one(rule(g).KeywordComments) }},
		{"rule keyword detached", func(s string) *ast.Grammar {
			return gr(&ast.RuleDef{Name: "m", Expr: lit(), Attrs: attrs(), KeywordDetachedComments: one(s)})
		}, "", func(g *ast.Grammar) []string { return rule(g).KeywordDetachedComments }},
		{"package keyword", func(s string) *ast.Grammar { return withPkg(func(p *ast.Pkg) { p.KeywordComments = s }) },
			"", func(g *ast.Grammar) []string { return one(g.File.Pkg.KeywordComments) }},
		{"package keyword detached", func(s string) *ast.Grammar {
			return withPkg(func(p *ast.Pkg) { p.KeywordDetachedComments = one(s) })
		}, "", func(g *ast.Grammar) []string { return g.File.Pkg.KeywordDetachedComments }},
		{"package header", func(s string) *ast.Grammar { return withPkg(func(p *ast.Pkg) { p.HeaderComments = one(s) }) },
			"$.file.pkg.headerComments[0]", nil},
		{"alias header", func(s string) *ast.Grammar {
			return gr(&ast.TypeAlias{Name: "T", Type: &ast.TypeRef{Name: "string"}, HeaderComments: one(s)})
		}, "$.file.decls[0].headerComments[0]", nil},
		{"type header", func(s string) *ast.Grammar {
			return gr(&ast.TypeDef{Name: "T", Type: &ast.StructType{Fields: []*ast.Field{{Name: "A", Type: &ast.TypeRef{Name: "string"}}}}, HeaderComments: one(s)})
		}, "$.file.decls[0].headerComments[0]", nil},
		{"var header", func(s string) *ast.Grammar {
			return gr(&ast.VarDef{Name: "v", Type: &ast.TypeRef{Name: "int"}, HeaderComments: one(s)})
		}, "$.file.decls[0].headerComments[0]", nil},
		{"field header", func(s string) *ast.Grammar {
			return gr(&ast.TypeDef{Name: "T", Type: &ast.StructType{Fields: []*ast.Field{{Name: "A", Type: &ast.TypeRef{Name: "string"}, HeaderComments: one(s)}}}})
		}, "$.file.decls[0].type.fields[0].headerComments[0]", nil},
		{"field leading", func(s string) *ast.Grammar {
			f := &ast.Field{Name: "A", Type: &ast.TypeRef{Name: "string"}}
			f.LeadingComments = s
			return gr(&ast.TypeDef{Name: "T", Type: &ast.StructType{Fields: []*ast.Field{f}}})
		}, "", func(g *ast.Grammar) []string { return one(fieldOf(g).LeadingComments) }},
		{"field trailing", func(s string) *ast.Grammar {
			f := &ast.Field{Name: "A", Type: &ast.TypeRef{Name: "string"}}
			f.TrailingComments = one(s)
			return gr(&ast.TypeDef{Name: "T", Type: &ast.StructType{Fields: []*ast.Field{f, {Name: "B", Type: &ast.TypeRef{Name: "int"}}}}})
		}, "", func(g *ast.Grammar) []string { return fieldOf(g).TrailingComments }},
		{"literal leading", func(s string) *ast.Grammar {
			l := lit()
			l.LeadingComments = s
			return gr(&ast.RuleDef{Name: "m", Expr: &ast.Seq{Items: []ast.Expr{&ast.Literal{Value: "b"}, l}}})
		}, "", nil},
		{"literal trailing", func(s string) *ast.Grammar {
			l := lit()
			l.TrailingComments = one(s)
			return gr(&ast.RuleDef{Name: "m", Expr: &ast.Seq{Items: []ast.Expr{l, &ast.Literal{Value: "b"}}}})
		}, "", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, term := range []string{"\n", "\r", "\r\n"} {
				for _, s := range []string{
					term,                              // an empty line comment
					" a" + term,                       // a line comment
					" a" + term + " b" + term,         // line comments
					" a" + term + " b ",               // a block comment over lines
					" a" + term + term + " b ",        // a block comment with an empty line
					"/" + term + "*" + term,           // line comments that could join into delimiters
					" a" + term + "/ b" + term + "* ", // a block comment whose lines could join into delimiters
				} {
					g := c.g(s)
					err := grammar.Validate(g)
					if c.path != "" {
						var verr *ast.ValidationError
						if !errors.As(err, &verr) || verr.Path != c.path || !strings.Contains(verr.Msg, "line terminator") {
							t.Errorf("%q: validation gives %v, want a line terminator at %s", s, err, c.path)
						}
						continue
					}
					if err != nil {
						t.Errorf("%q: %v", s, err)
						continue
					}
					src := grammar.Format(g)
					back, err := grammar.Load(grammar.FromString("g.sxir", src))
					if err != nil {
						t.Errorf("%q: reading %q: %v", s, src, err)
						continue
					}
					if c.got == nil {
						continue
					}
					lf := strings.NewReplacer("\r\n", "\n", "\r", "\n")
					if got := c.got(back); len(got) != 1 || lf.Replace(got[0]) != lf.Replace(s) {
						t.Errorf("%q: written %q, which reads back the comments %q", s, src, got)
					}
				}
			}
		})
	}
}

// TestFormatTrailingCommentAfterClosingDelimiter formats a comment that ends the line after the
// closing delimiter of a node inside an expression, which the parser gives to that node, and checks
// that the text parses, keeps the comment and formats again as the same text. Such a comment ends
// its line, so the text after the node, such as the ")" of #recover, goes on the next line.
func TestFormatTrailingCommentAfterClosingDelimiter(t *testing.T) {
	var sources []string
	for _, context := range []string{
		`"a" #recover(skip=G)`, `"a" #recover(skip=G) "b"`, `"a" #recover(skip="b" / G)`, `"a" #recover(skip=G*)`,
		`"a" #recover(skip=!G "b")`, `"a" #recover(skip=c:G)`, `!G`, `G*`, `G #atomic`, `(G / "b")`, `"a" @y(x=G)`,
	} {
		for _, group := range []string{`("x")`, `($ => 1)`, `("x" / "y")`, `(("x"))`} {
			for _, comment := range []string{"//t", "/*a */ //t", "/*a\n*/ //t"} {
				sources = append(sources, "rule main = "+strings.ReplaceAll(context, "G", group)+" "+comment+"\nrule b = \"b\"\n")
			}
		}
	}
	for _, src := range sources {
		g, err := grammar.Load(grammar.FromString("g.sxir", src))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		text := grammar.Format(g)
		back, err := grammar.Load(grammar.FromString("g.sxir", text))
		if err != nil {
			t.Errorf("%q: the formatted grammar does not parse: %v\n%s", src, err, text)
			continue
		}
		if !strings.Contains(text, "//t") || len(back.AllComments()) != len(g.AllComments()) {
			t.Errorf("%q: formatted as %q, which loses a comment", src, text)
		}
		if again := grammar.Format(back); again != text {
			t.Errorf("%q: formatted as %q, and again as %q", src, text, again)
		}
	}
}

// TestFormatTrailingCommentStrings formats a declaration whose trailing comment strings end in a
// line terminator before the last: only the last can be a line comment, which ends the line, so
// the ones before it are written as block comments.
func TestFormatTrailingCommentStrings(t *testing.T) {
	for _, comments := range [][]string{
		{"t\n", "u"}, {"t\n\n", "u\n"}, {"t", "u\n"}, {"t\nv\n", "u"}, {"t\n", "", "u\n"}, {"t\n", "u\n", ""},
	} {
		r := &ast.RuleDef{Name: "r", Expr: &ast.Literal{Value: "a"}}
		r.TrailingComments = comments
		text := grammar.Format(&ast.Grammar{File: ast.File{Decls: []ast.Decl{r, &ast.RuleDef{Name: "b", Expr: &ast.Literal{Value: "b"}}}}})
		back, err := grammar.Load(grammar.FromString("g.sxir", text))
		if err != nil {
			t.Errorf("%q: the formatted grammar does not parse: %v\n%s", comments, err, text)
			continue
		}
		if rules := back.Rules(); len(rules) != 2 || !strings.Contains(strings.Join(rules[0].TrailingComments, ""), "t") || !strings.Contains(strings.Join(rules[0].TrailingComments, ""), "u") {
			t.Errorf("%q: formatted as %q, which reads back the rules %v", comments, text, rules)
		}
	}
}
