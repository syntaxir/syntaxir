package prim

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
)

// TestCloneCopiesTheAttributes checks that a copy of a function at the Exec stage has attributes of its
// own: changing the variables of the copy leaves those of the original.
func TestCloneCopiesTheAttributes(t *testing.T) {
	m := code.NewModule("m")
	m.Funcs = []*code.Func{{Name: "f", Body: &code.Block{}, Attrs: &Attrs{Vars: []string{"a"}, Frame: true}}}
	c := m.Clone()
	a := AttrsOf(c.Funcs[0])
	if a == nil || a == AttrsOf(m.Funcs[0]) || !a.Frame {
		t.Fatalf("the copy has the attributes %+v", c.Funcs[0].Attrs)
	}
	a.Vars[0] = "b"
	if got := AttrsOf(m.Funcs[0]).Vars[0]; got != "a" {
		t.Errorf("changing the copy changed the original's variable to %s", got)
	}
}

// TestVerifyModuleChecksTheAttributes checks the check of the Exec stage: a function with the Exec-stage
// attributes or none verifies, one with attributes of another type does not.
func TestVerifyModuleChecksTheAttributes(t *testing.T) {
	m := code.NewModule("m")
	m.Funcs = []*code.Func{{Name: "f", Body: &code.Block{}, Attrs: &Attrs{}}, {Name: "g", Body: &code.Block{}}}
	if err := VerifyModule(m); err != nil {
		t.Fatal(err)
	}
	type other struct{}
	m.Funcs[1].Attrs = &other{}
	if err := VerifyModule(m); err == nil || !strings.Contains(err.Error(), "g: attributes of the type *prim.other at the Exec stage") {
		t.Errorf("attributes of another type: %v", err)
	}
}
