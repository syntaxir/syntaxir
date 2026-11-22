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

// TestVerifyMarksChecksTheMarks checks the marks of the whole parser state in the check of the Exec
// stage: a reset, a trail drop and a position read of a mark set before them verify, and each of them
// fails on a mark that is not a local or that no markState set before it, and a trail drop on a mark
// that does not save the trail.
func TestVerifyMarksChecksTheMarks(t *testing.T) {
	m := &code.Local{Name: "m"}
	mark := func(st StateMark) code.Stmt {
		return &code.Set{Dst: []code.Place{{Name: "m"}}, Values: []code.Expr{&code.Call{Callee: OpMarkState, Imm: st}}}
	}
	use := func(op string, arg code.Expr) code.Stmt {
		return &code.Set{Dst: []code.Place{{}}, Values: []code.Expr{&code.Call{Callee: op, Args: []code.Expr{arg}}}}
	}
	fn := func(stmts ...code.Stmt) *code.Func {
		return &code.Func{Name: "f", Body: &code.Block{Stmts: stmts}, Attrs: &Attrs{}}
	}
	ok := fn(mark(StateMark{Trail: true}), use(OpMarkPos, m), use(OpReset, m), use(OpDropTrail, m))
	if err := verifyMarks(ok); err != nil {
		t.Errorf("marks set before their uses: %v", err)
	}
	for _, c := range []struct {
		f    *code.Func
		want string
	}{
		{fn(use(OpReset, m)), "runtime.reset of the mark m before a markState sets it"},
		{fn(use(OpMarkPos, m), mark(StateMark{})), "runtime.markPos of the mark m before a markState sets it"},
		{fn(mark(StateMark{}), use(OpReset, code.BoolC(false))), "runtime.reset of a mark that is not a local"},
		{fn(mark(StateMark{Env: true}), use(OpDropTrail, m)), "runtime.dropTrail of the mark m, which does not save the trail"},
	} {
		if err := verifyMarks(c.f); err == nil || err.Error() != c.want {
			t.Errorf("verifyMarks = %v, want %q", err, c.want)
		}
	}
}
