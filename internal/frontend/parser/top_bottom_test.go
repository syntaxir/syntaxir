package parser

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func TestTopBottomSyntax(t *testing.T) {
	t.Parallel()
	src := "type A ~ []B & *C | _ & (D | B) | _|_\ntype E ~ [](B & C) & *(B | C)\ntype B = { Text string }\ntype C = { Text string }\ntype D = { Text string }\nrule main = x:\"a\" => $x.(_ & node[_])"
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	formatted := fmt.Format(g)
	for _, want := range []string{"type A ~ []B & *C | _ & (D | B) | _|_", "type E ~ [](B & C) & *(B | C)", "$x.(_ & node[_])"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted lacks %q:\n%s", want, formatted)
		}
	}
	alias := g.Types()[0].(*ast.TypeAlias).Type.(*ast.JoinType)
	if len(alias.Types) != 3 {
		t.Fatalf("union %#v", alias)
	}
	if m, ok := alias.Types[0].(*ast.MeetType); !ok || len(m.Types) != 2 {
		t.Fatalf("meet %#v", alias.Types[0])
	}
	if b, ok := alias.Types[2].(*ast.TypeRef); !ok || b.Name != "_|_" {
		t.Fatalf("bottom %#v", alias.Types[2])
	}
	// The JSON AST keeps meets and the top and bottom types.
	data, err := ast.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ast.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	// JSON keeps the tree but not the layout, so compare a second encoding.
	again, err := ast.MarshalJSON(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) || !strings.Contains(string(data), "MeetType") {
		t.Fatalf("JSON round trip:\n%s\nwant\n%s", again, data)
	}
}
