package state

import (
	"testing"

	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// TestEngineValueItemAtOpensBoxes holds the primitive the loops of the iterating built-ins read their elements
// with: the elements of a list travel in boxes, and the function of the built-in is passed the ints themselves,
// from either end. A compiled action expression opens a box where it reads a parameter, so the action expressions of the table above
// cannot show a primitive that passes the box.
func TestEngineValueItemAtOpensBoxes(t *testing.T) {
	prog := &SharedProgram{}
	InitSharedProgram(prog, compiletest.Program(t, "@start\nrule main = \"a\"\n", check.Options{}).Runtime())
	p := &Parser{Prog: prog, Input: NewInput(conform.BuiltinInput, input.Bytes)}
	c := &EvalCtx{P: p}
	one, _ := element(c, "list", 1)
	two, _ := element(c, "list", 2)
	items := []*value.Node{one, two}
	for _, tc := range []struct {
		i     int
		right bool
		want  any
	}{{0, false, 1}, {1, false, 2}, {0, true, 2}, {1, true, 1}} {
		if got := p.actionItemAt(items, tc.i, tc.right); got != tc.want {
			t.Errorf("itemAt(%d, right=%v) = %#v, want %#v", tc.i, tc.right, got, tc.want)
		}
	}
}
