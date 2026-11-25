package grammar_test

import (
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
