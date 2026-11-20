package code

import "testing"

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
