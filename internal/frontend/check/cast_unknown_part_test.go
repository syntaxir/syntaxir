package check

import (
	"strings"
	"testing"
)

// TestUnknownPartUnderTopNeedsNoCast checks that a value with a part of the type that is not known
// fits a declared Type without a cast where that Type has _ at the part's place, inside a node, a
// tuple, a join or an optional type, and asks for the cast where the place is more specific.
func TestUnknownPartUnderTopNeedsNoCast(t *testing.T) {
	t.Parallel()
	// The value of x:g "!" is node[(g, node[string]), {"x": 0}], where g is of the type that is not
	// known.
	for _, decl := range []string{"node[_]", "node[(_, node[string])]", "node[_] | int", "(_, int)"} {
		body := `x:g "!"`
		if strings.HasPrefix(decl, "(") {
			body = `x:g "!" => ($x, 1)`
		}
		mustCompile(t, unknownList+"rule e "+decl+" = "+body+"\n")
	}
	for _, c := range []struct{ decl, body, cast string }{
		{"node[([][]int, node[string])]", `y:(x:g "!")`, `y:(x:g "!") => $y.(node[([][]int, node[string])])`},
		{"(int, int)", `x:g "!" => ($x, 1)`, `x:g "!" => ($x, 1).((int, int))`},
		// The whole value is not known, so it may be no node.
		{"node[_]", `x:g => $x`, `x:g => $x.(node[_])`},
	} {
		got := compileError(t, unknownList+"rule e "+c.decl+" = "+c.body+"\n")
		if want := "convert it with a cast, as in value.(" + c.decl + ")"; !strings.Contains(got, want) {
			t.Errorf("rule e %s = %s: got %s, want %s", c.decl, c.body, got, want)
		}
		mustCompile(t, unknownList+"rule e "+c.decl+" = "+c.cast+"\n")
	}
}
