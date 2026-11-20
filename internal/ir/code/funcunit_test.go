package code

import (
	"strings"
	"testing"
)

// TestFuncUnit checks that the unit of one function of a module shares the module's declarations, that
// an operation added to it reaches the module only when it is put back, with the function it holds, and
// that a unit of another number of functions is refused.
func TestFuncUnit(t *testing.T) {
	m := NewModule("m")
	m.AddOp(&Op{Name: "a"})
	f, g := &Func{Name: "f", Body: &Block{}}, &Func{Name: "g", Body: &Block{}}
	m.Funcs = []*Func{f, g}
	u, v := m.FuncUnit(0), m.FuncUnit(1)
	if len(u.Funcs) != 1 || u.Funcs[0] != f || u.Op("a") == nil {
		t.Fatalf("unit of f: %+v", u)
	}
	u.AddOp(&Op{Name: "b"})
	v.AddOp(&Op{Name: "c"})
	if m.Op("b") != nil || u.Op("c") != nil || v.Op("b") != nil {
		t.Fatal("an operation added to one unit reached the module or another unit")
	}
	h := &Func{Name: "h", Body: &Block{}}
	u.Funcs[0] = h
	if err := m.PutFuncUnit(0, u); err != nil {
		t.Fatal(err)
	}
	if err := m.PutFuncUnit(1, v); err != nil {
		t.Fatal(err)
	}
	if m.Funcs[0] != h || m.Funcs[1] != g || m.Op("b") == nil || m.Op("c") == nil || len(m.Ops) != 3 {
		t.Errorf("after the puts: funcs %v, %d ops", m.Funcs, len(m.Ops))
	}
	u.Funcs = nil
	if err := m.PutFuncUnit(0, u); err == nil {
		t.Error("a unit without its function was put back")
	}
}

// TestPutFuncUnitConflicts checks that a unit whose passes changed a declaration other than the
// operations, or declared an operation that the module declares otherwise, is not put back, and that the
// module is left as it was; an operation declared alike by two units is declared once.
func TestPutFuncUnitConflicts(t *testing.T) {
	newModule := func() *Module {
		m := NewModule("m")
		m.Funcs = []*Func{{Name: "f", Body: &Block{}}, {Name: "g", Body: &Block{}}}
		return m
	}
	m := newModule()
	u, v := m.FuncUnit(0), m.FuncUnit(1)
	u.AddOp(&Op{Name: "x", Component: "a"})
	v.AddOp(&Op{Name: "x", Component: "b"})
	if err := m.PutFuncUnit(0, u); err != nil {
		t.Fatal(err)
	}
	g := m.Funcs[1]
	if err := m.PutFuncUnit(1, v); err == nil || !strings.Contains(err.Error(), "operation x") {
		t.Errorf("an operation declared otherwise: %v", err)
	}
	if m.Funcs[1] != g || len(m.Ops) != 1 || m.Op("x").Component != "a" {
		t.Error("a refused unit changed the module")
	}
	w := m.FuncUnit(1)
	w.AddOp(&Op{Name: "x", Component: "a"})
	if err := m.PutFuncUnit(1, w); err != nil || len(m.Ops) != 1 {
		t.Errorf("an operation declared alike: %v, %d ops", err, len(m.Ops))
	}
	for name, change := range map[string]func(u *Module){
		"dialect":  func(u *Module) { u.Dialects = append(u.Dialects, "d") },
		"feature":  func(u *Module) { u.Features = append(u.Features, "f") },
		"handle":   func(u *Module) { u.Handles = append(u.Handles, &HandleType{}) },
		"register": func(u *Module) { u.Registers = append(u.Registers, &Register{}) },
	} {
		m := newModule()
		u := m.FuncUnit(0)
		change(u)
		if err := m.PutFuncUnit(0, u); err == nil || !strings.Contains(err.Error(), "declaration other than") {
			t.Errorf("%s added: %v", name, err)
		}
	}
}
