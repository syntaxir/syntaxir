package parser

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/syntax/fmt"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func TestCaptureAccessSyntax(t *testing.T) {
	t.Parallel()
	g, err := Parse(`rule main = xs:(y:"a")+ => map($xs, (i) => i.$y.Start)`)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Format(g); !strings.Contains(got, "i.$y.Start") {
		t.Fatalf("formatted:\n%s", got)
	}
	body := g.Rules()[0].Action.(*ast.Call).Args[1].(*ast.Lambda).Body.(*ast.FieldAccess)
	if inner, ok := body.X.(*ast.FieldAccess); !ok || inner.Name != "$y" || body.Name != "Start" {
		t.Fatalf("access %#v", body)
	}
}
