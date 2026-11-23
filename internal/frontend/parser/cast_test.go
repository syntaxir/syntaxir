package parser

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func TestCastSyntax(t *testing.T) {
	t.Parallel()
	g, err := Parse("type Foo = { A node[string] }\ntype Word = { Text string }\nrule main = x:\"a\" => $x.( []Foo | *Word ).Start\nrule one = \"a\" => (1).(int)")
	if err != nil {
		t.Fatal(err)
	}
	formatted := fmt.Format(g)
	for _, want := range []string{"$x.([]Foo | *Word).Start", "(1).(int)"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted lacks %q:\n%s", want, formatted)
		}
	}
	if _, err := Parse(formatted); err != nil {
		t.Fatalf("formatted grammar does not parse: %v", err)
	}
	member := g.Rules()[0].Action.(*ast.FieldAccess)
	if cast, ok := member.X.(*ast.Cast); !ok || member.Name != "Start" {
		t.Fatalf("cast %#v", member)
	} else if _, ok := cast.Type.(*ast.JoinType); !ok {
		t.Fatalf("cast type %#v", cast.Type)
	}
	if data, err := ast.MarshalJSON(g); err != nil || !strings.Contains(string(data), "Cast") {
		t.Fatalf("JSON AST: %v %s", err, data)
	}
}
