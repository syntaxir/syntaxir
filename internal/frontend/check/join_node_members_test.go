package check

import (
	"strings"
	"testing"
)

// A value of a join is told apart by its struct or node type name, or, for an unnamed node
// type, by the kind of its node: a string, a tuple or a list. A written join whose unnamed node
// members share a kind is rejected, and one whose members differ in name or kind is accepted.
func TestJoinNodeMembersTellApart(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"type A = { V int }\ntype J ~ A | node[[]A] | node[(node[string], A)] | node[string]\ntype T = { F *J }\nrule main = \"a\" => T{F: nil}",
		"type N = node[[]node[string]]\ntype J ~ N | node[[]node[(node[string],)]]\ntype T = { F *J }\nrule main = \"a\" => T{F: nil}",
		"type A = { V int }\ntype J ~ A | node[_]\ntype T = { F *J }\nrule main = \"a\" => T{F: nil}",
		"type T = { F *(node[[]node[string]] | node[(node[string],)]) }\nrule main = \"a\" => T{F: nil}",
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if _, err := compileStatic(g, Options{}, nil); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
	for _, tc := range []struct{ src, kind string }{
		{"type A = { V int }\ntype B = { W int }\ntype J ~ node[[]A] | node[[]B]\ntype T = { F *J }\nrule main = \"a\" => T{F: nil}", "list"},
		{"type J ~ node[(node[string],)] | node[(node[string], node[string])]\ntype T = { F *J }\nrule main = \"a\" => T{F: nil}", "tuple"},
		{"type A = { V int }\ntype J ~ node[_] | node[[]A]\ntype T = { F *J }\nrule main = \"a\" => T{F: nil}", "list"},
		{"type T = { F *(node[string] | node[*string]) }\nrule main = \"a\" => T{F: nil}", "string"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		_, err = compileStatic(g, Options{}, nil)
		if err == nil || !strings.Contains(err.Error(), "a "+tc.kind+" node is both") || strings.Count(err.Error(), "is both") != 1 {
			t.Errorf("%q: got %v, want one ambiguous %s join error", tc.src, err, tc.kind)
		}
	}
}
