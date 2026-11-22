package parser_test

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/internal/driver/entry"
	"syntaxir.dev/sxir/internal/frontend/parser"
	"syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/internal/testing/starttest"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func TestExpressionSourceRoundTrip(t *testing.T) {
	for _, src := range []string{
		`rule main = [0-9]+ {x := 1} {x < 10} => ...`,
		`rule main = [^a-zA-Z\)\#]`,
		`package demo; rule main = "a"; type A = { X A; Y A }; rule next = "b"`,
		"rule main = (\n    \"a\"\n    / \"b\"\n)\nrule next = \"c\"",
		`rule main = #operators { skip " "*; operand num; level { infix left "+" => ...; postfix "!" }; level power { infix right "^" } }`,
		"rule main = #operators {\n    operand \"a\"\n} => ...",
		"rule main = #operators {\n    operand \"a\"\n} => ...",
		`rule main = (a => A{}) / (b => B{}) => C{}`,
		`rule main = (a => A{}) => C{}`,
		`rule main = (a => ...)`,
		`rule main = a:a => foldl($a, 0, (acc, item) => acc + item)`,
	} {
		t.Run(src, func(t *testing.T) {
			g, err := parser.Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			formatted := fmt.Format(g)
			g2, err := parser.Parse(formatted)
			if err != nil {
				t.Fatalf("formatted source invalid: %v\n%s", err, formatted)
			}
			if again := fmt.Format(g2); again != formatted {
				t.Fatalf("format is not stable:\n%s\n%s", formatted, again)
			}
			data, err := ast.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := ast.UnmarshalJSON(data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parser.Parse(fmt.Format(decoded)); err != nil {
				t.Fatalf("JSON format invalid: %v", err)
			}
		})
	}
}

func TestExpressionSourceBoundaries(t *testing.T) {
	for _, src := range []string{
		`rule a = "a" rule b = "b"`,
		"rule a = \"a\"\n/ \"b\"",
		"rule a = \"a\"\n\u00a0\"b\"",
		"rule a = \"a\"\n/*comment*/ \"b\"",
		"rule a = \"(\"\n\"x\"",
		"rule a = \"{\"\n\"x\"",
		"rule a = (\"(\" )\n\"x\"",
		`type A = { X A Y A }`,
		"rule a = #operators { operand \"a\" }\n=> ...",
		"rule a = #operators { operand \"a\" } =>\n...",
		`rule a = a => A{} => B{}`,
		`rule a = a => (B => C)`,
		`#atomic rule a = "a"`,
	} {
		t.Run(src, func(t *testing.T) {
			if _, err := parser.Parse(src); err == nil {
				t.Fatal("accepted invalid source")
			}
		})
	}
}

func TestExpressionSourceInlineActionsCompile(t *testing.T) {
	for _, src := range []string{
		`rule main = ("a" => ...)`,
		`rule main = x:("a" => "x")`,
		`rule main = ("a" => "x")*`,
		`rule main = "a" #recover(skip=("b" => ...))`,
	} {
		g, err := parser.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Compile(starttest.MarkAll(g), entry.Options{}); err != nil {
			t.Fatalf("inline action in %s: %v", src, err)
		}
	}
}

func TestExpressionSourceHeaderComments(t *testing.T) {
	for _, src := range []string{
		`rule /*header*/ main = "a"`,
		`type /*header*/ A = { Text string }; rule main = "a"`,
		`package /*header*/ demo; rule main = "a"`,
		`var /*header*/ x int; rule main = "a"`,
		`var x /*header*/ int; rule main = "a"`,
		`rule main = #operators { operand "a"; level /*header*/ { infix left "+" } }`,
		`rule main = #operators { operand "a"; level { infix /*header*/ left "+" } }`,
	} {
		g, err := parser.Parse(src)
		if err != nil {
			t.Fatalf("parsing should preserve header comments: %v", err)
		}
		if len(g.File.HeaderComments) != 1 {
			t.Fatalf("header comments: %d", len(g.File.HeaderComments))
		}
		formatted := fmt.Format(g)
		again, parseErr := parser.Parse(formatted)
		if parseErr != nil {
			t.Fatalf("header format no longer parses: %v\n%s", parseErr, formatted)
		}
		if len(again.File.HeaderComments) == 0 {
			t.Fatalf("formatter removed header placement error:\n%s", formatted)
		}
		if _, err := entry.Compile(starttest.MarkAll(again), entry.Options{}); err == nil {
			t.Fatal("formatted header became compilable")
		}
		_, err = entry.Compile(starttest.MarkAll(g), entry.Options{})
		if err == nil || !strings.Contains(err.Error(), "comments are not allowed inside statement headers") {
			t.Fatalf("expected positioned semantic rejection, got %v", err)
		}
	}
	valid := `type N = { Value int }
var x int
/*before body*/
/*inside*/
/*predicate*/
/*after body*/
/*before action*/
/*argument*/
rule main = v1:"a" "b" {x := 1} => N{Value: len($v1)} /*after action*/`
	g, err := parser.Parse(valid)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.File.HeaderComments) != 0 {
		t.Fatal("expression comments marked as headers")
	}
	if _, err := entry.Compile(starttest.MarkAll(g), entry.Options{}); err != nil {
		t.Fatal(err)
	}
}

// TestVariableHeaderCommentRejected checks that a comment in the header of a
// variable declaration after its name, before its type or its "=", is a header
// comment on each frontend: the compiler rejects it at its own position with
// the header diagnostic.
func TestVariableHeaderCommentRejected(t *testing.T) {
	for _, tt := range []struct {
		src       string
		line, col int
	}{
		{"var x /* c */ int\nrule main = \"a\"\n", 1, 7},
		{"var x /* c */ = 1\nrule main = \"a\"\n", 1, 7},
		{"var x int /* c */ = 1\nrule main = \"a\"\n", 1, 11},
		{"var x /* c */ int = 1\nrule main = \"a\"\n", 1, 7},
		{"var x /* c */ (int)\nrule main = \"a\"\n", 1, 7},
	} {
		for _, frontend := range []struct {
			name  string
			parse func(string) (*ast.Grammar, error)
		}{{"generated", parser.Parse}, {"reference", parser.ParseReference}} {
			g, err := frontend.parse(tt.src)
			if err != nil {
				t.Errorf("%s %q: %v", frontend.name, tt.src, err)
				continue
			}
			if n := len(g.File.HeaderComments); n != 1 {
				t.Errorf("%s %q: %d header comments", frontend.name, tt.src, n)
			}
			_, err = entry.Compile(starttest.MarkAll(g), entry.Options{})
			want := (&diagnostic.Error{Pos: ast.Pos{Line: tt.line, Col: tt.col}, Msg: "comments are not allowed inside statement headers"}).Error()
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s %q: want %q, got %v", frontend.name, tt.src, want, err)
			}
		}
	}
}

func TestExpressionSourceDefaultCST(t *testing.T) {
	var trees []string
	for _, src := range []string{`rule main = "a" "b"`, `rule main = "a" "b" => ...`} {
		g, err := parser.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		p, err := entry.Compile(starttest.MarkAll(g), entry.Options{})
		if err != nil {
			t.Fatal(err)
		}
		n, err := p.Parse("main", "ab")
		if err != nil {
			t.Fatal(err)
		}
		trees = append(trees, n.String())
	}
	if trees[0] != trees[1] || trees[0] != `("a" "b")` {
		t.Fatalf("default CST differs: %v", trees)
	}
}
