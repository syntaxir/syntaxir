package check

import (
	"testing"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

func TestUnknownDeclarationAttribute(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{"@foo\nrule a = \"x\"\n", "1:1: unknown declaration attribute @foo"},
		{"rule a = \"x\"\n\n@foo(1) @bar\nrule b = a\n", "3:1: unknown declaration attribute @foo\n3:9: unknown declaration attribute @bar"},
	} {
		if got := compileError(t, tc.src); got != tc.want {
			t.Errorf("%q\n got %s\nwant %s", tc.src, got, tc.want)
		}
	}
	// A declaration attribute is never a # pragma; structural validation rejects one.
	g := mustParse("rule a = \"x\"\n")
	g.Rules()[0].Attrs = []*ast.Attribute{{Name: "atomic", Pragma: true}}
	if _, err := compileStatic(g, Options{}, nil); err == nil || err.Error() != "1:1: $.file.decls[0].attrs[0].pragma: a pragma follows a parser expression; a declaration takes @ attributes" {
		t.Errorf("pragma: %v", err)
	}
}
