package prim

import "testing"

// TestIsOp checks that IsOp holds for every primitive and for no other name, the name of the Flow
// operation that a primitive stands for included.
func TestIsOp(t *testing.T) {
	for _, name := range OpNames() {
		if !IsOp(name) {
			t.Errorf("IsOp(%q) is false", name)
		}
	}
	for _, name := range []string{"", "flow.matchLit", "runtime.", "runtime.noSuchPrimitive", "action.raise"} {
		if IsOp(name) {
			t.Errorf("IsOp(%q) is true", name)
		}
	}
}
