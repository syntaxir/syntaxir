package grammar_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/pkg/grammar"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// syntaxErrorsOf loads src as grammar syntax and returns its errors, failing unless Load returns
// a SyntaxErrors itself.
func syntaxErrorsOf(t *testing.T, src string) grammar.SyntaxErrors {
	t.Helper()
	_, err := grammar.Load(grammar.FromString("g.sxir", src))
	errs, ok := err.(grammar.SyntaxErrors)
	if !ok || len(errs) == 0 {
		t.Fatalf("%q: %T %v, want a non-empty grammar.SyntaxErrors", src, err, err)
	}
	var viaAs grammar.SyntaxErrors
	if !errors.As(err, &viaAs) || len(viaAs) != len(errs) {
		t.Fatalf("%q: errors.As did not give the list", src)
	}
	return errs
}

func TestLoadSyntaxErrors(t *testing.T) {
	type want struct {
		pos, end ast.Pos
		msg      string
	}
	for _, c := range []struct {
		src  string
		want []want
	}{
		// An error at a point has no end.
		{"rule main = \"a\" /\n", []want{{ast.Pos{Line: 2, Col: 1}, ast.Pos{}, "expected an expression, found end of file"}}},
		// An error about a text ends after it; columns count characters.
		{"rule main = \"é\\q\"\n", []want{{ast.Pos{Line: 1, Col: 15}, ast.Pos{Line: 1, Col: 17}, `unknown escape sequence \q`}}},
		// The lexical errors come first, then the others in the order of the source.
		{"rule a = (\nrule b = \"x\\q\"\n", []want{
			{ast.Pos{Line: 2, Col: 12}, ast.Pos{Line: 2, Col: 14}, `unknown escape sequence \q`},
			{ast.Pos{Line: 2, Col: 8}, ast.Pos{}, "expected closing parenthesis for rule group, found '='"},
		}},
	} {
		errs := syntaxErrorsOf(t, c.src)
		var got []want
		for _, e := range errs {
			got = append(got, want{e.Pos, e.End, e.Msg})
		}
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%q:\n got %v\nwant %v", c.src, got, c.want)
		}
	}
}

func TestLoadSyntaxErrorsCap(t *testing.T) {
	errs := syntaxErrorsOf(t, strings.Repeat("rule a = (\n", 150))
	if len(errs) != 101 {
		t.Fatalf("%d errors, want 100 and the last", len(errs))
	}
	if last := errs[100]; last.Msg != "too many errors" || last.Pos != (ast.Pos{Line: 102, Col: 8}) {
		t.Errorf("last error %+v, want too many errors where the 101st error is", last)
	}
}

// TestLoadSyntaxErrorsText checks that the text of the errors is the text of the parser's errors,
// which the command line prints.
func TestLoadSyntaxErrorsText(t *testing.T) {
	for _, src := range []string{"rule main = (\n", "rule a = (\nrule b = \"x\\q\"\n", "rule main = 'a' $\n"} {
		_, want := parser.Parse(src)
		errs := syntaxErrorsOf(t, src)
		if errs.Error() != want.Error() {
			t.Errorf("%q: %q, want %q", src, errs.Error(), want.Error())
		}
		if errs[0].Error() != strings.Split(want.Error(), "\n")[0] {
			t.Errorf("%q: first error %q", src, errs[0].Error())
		}
	}
}

// TestLoadSyntaxErrorsIsTheList checks that the list does not unwrap to its elements, and that the
// other errors of Load are not syntax errors.
func TestLoadSyntaxErrorsIsTheList(t *testing.T) {
	_, err := grammar.Load(grammar.FromString("g.sxir", "rule main = (\n"))
	var one *grammar.SyntaxError
	if errors.As(err, &one) {
		t.Error("errors.As gave one error of the list")
	}
	var errs grammar.SyntaxErrors
	for _, s := range []grammar.Source{
		grammar.FromString("g.json", "{"),
		grammar.FromBytes("g.sxirc", []byte("SXC\x00rest")),
	} {
		if _, err := grammar.Load(s); err == nil || errors.As(err, &errs) {
			t.Errorf("%s: %v", s.Name(), err)
		}
	}
}

func ExampleSyntaxErrors() {
	src := grammar.FromString("g.sxir", "rule main = \"a\" /\nrule b = \"\\q\"\n")
	_, err := grammar.Load(src)
	var errs grammar.SyntaxErrors
	if errors.As(err, &errs) {
		for _, e := range errs {
			fmt.Printf("%s:%d:%d: %s\n", src.Name(), e.Pos.Line, e.Pos.Col, e.Msg)
		}
	}
	// Output:
	// g.sxir:2:11: unknown escape sequence \q
	// g.sxir:2:1: expected an expression, found 'rule'
}
