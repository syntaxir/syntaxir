package stores

import (
	"slices"
	"sort"
	"testing"

	. "syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
)

// storesFunc returns a function of the test module with the locals names, all ints, and the body.
func storesFunc(t *testing.T, names []string, body ...Stmt) *Func {
	t.Helper()
	m := module()
	f := &Func{Name: "f", Body: &Block{Label: "fail", Stmts: body}}
	for _, n := range names {
		f.Locals = append(f.Locals, P(n, TInt))
	}
	m.Funcs = append(m.Funcs, f)
	if err := Verify(m); err != nil {
		t.Fatal(err)
	}
	return f
}

// pureLocals treats every expression without a call as pure.
func pureLocals(x Expr) bool {
	found := false
	Walk(&Block{Stmts: []Stmt{&Assert{Cond: x}}}, nil, func(e Expr) {
		if _, ok := e.(*Call); ok {
			found = true
		}
	})
	return !found
}

func keys(m map[string]bool) []string {
	var out []string
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func TestDeadStores(t *testing.T) {
	note := func(x Expr) Stmt { return &Do{Call: C("log.note", x)} }
	flag := C("log.flag")
	cases := []struct {
		name          string
		locals        []string
		body          []Stmt
		dead, removed []string // removed lists the places of the removed stores
	}{
		{name: "a chain of unread stores goes", locals: []string{"a", "b", "c"},
			body: []Stmt{Assign("a", Int(1)), Assign("b", L("a")), Assign("c", L("b")), note(L("a"))},
			dead: []string{"b", "c"}, removed: []string{"b", "c"}},
		{name: "a store with an effect stays with a dead place", locals: []string{"ok"},
			body: []Stmt{&Set{Dst: []Place{{Name: "ok"}}, Values: []Expr{flag}}},
			dead: []string{"ok"}},
		{name: "an increment keeps nothing", locals: []string{"n"},
			body: []Stmt{Assign("n", Int(0)), Assign("n", Add(L("n"), Int(1)))},
			dead: []string{"n"}, removed: []string{"n", "n"}},
		{name: "the Init of an If keeps its place", locals: []string{"a"},
			body: []Stmt{&If{Init: Assign("a", Int(1)), Cond: BoolC(true), Then: &Block{}}}},
		{name: "a pure store of several places goes", locals: []string{"a", "b"},
			body: []Stmt{&Set{Dst: []Place{{Name: "a"}, {Name: "b"}}, Values: []Expr{Int(1), Int(2)}}},
			dead: []string{"a", "b"}, removed: []string{"a"}},
		{name: "reads after an exit do not count", locals: []string{"a"},
			body: []Stmt{Assign("a", Int(1)), &Exit{Label: "fail"}, note(L("a"))},
			dead: []string{"a"}, removed: []string{"a"}},
		{name: "a register place keeps a store", locals: []string{"a"},
			body: []Stmt{Assign("a", Int(1)), &Set{Dst: []Place{{Name: "x", Reg: true}}, Values: []Expr{L("a")}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := module()
			m.AddRegister("x", TInt, "A register.")
			f := &Func{Name: "f", Body: &Block{Label: "fail", Stmts: c.body}}
			for _, n := range c.locals {
				typ := TInt
				if n == "ok" {
					typ = TBool
				}
				f.Locals = append(f.Locals, P(n, typ))
			}
			m.Funcs = append(m.Funcs, f)
			if err := Verify(m); err != nil {
				t.Fatal(err)
			}
			st := Dead(f, c.locals, pureLocals)
			if got := keys(st.Dead); !slices.Equal(got, c.dead) {
				t.Errorf("dead %v, want %v", got, c.dead)
			}
			var removed []string
			Walk(f.Body, func(s Stmt) {
				if set, ok := s.(*Set); ok && st.Removed[set] {
					removed = append(removed, set.Dst[0].Name)
				}
			}, nil)
			if !slices.Equal(removed, c.removed) {
				t.Errorf("removed %v, want %v", removed, c.removed)
			}
		})
	}
}

func TestDeadStoresKeepCandidatesOnly(t *testing.T) {
	f := storesFunc(t, []string{"a", "entry"}, Assign("a", Int(1)), Assign("entry", Int(2)))
	st := Dead(f, []string{"a"}, pureLocals)
	if got := keys(st.Dead); !slices.Equal(got, []string{"a"}) {
		t.Errorf("dead %v, want [a]: a local that is not a candidate stays", got)
	}
}

func TestDeadStoresKeepNullOppositeSeveralPlaces(t *testing.T) {
	m := module()
	h := m.AddHandle("H", "log", "A handle.")
	f := &Func{Name: "f", Locals: []Param{P("a", TInt), P("b", h)}, Body: &Block{Stmts: []Stmt{
		&Set{Dst: []Place{{Name: "a"}, {Name: "b"}}, Values: []Expr{Int(1), Null(h)}},
	}}}
	m.Funcs = append(m.Funcs, f)
	if err := Verify(m); err != nil {
		t.Fatal(err)
	}
	st := Dead(f, []string{"a", "b"}, pureLocals)
	if got := keys(st.Dead); !slices.Equal(got, []string{"a"}) {
		t.Errorf("dead %v, want [a]: a place opposite a null stays", got)
	}
	if len(st.Removed) != 0 {
		t.Error("the store with a place that stays is removed")
	}
}

// TestRefusesFlat checks the check of a flat function: a reachable read of the entry position or
// the action context refuses it, an unreachable one does not, and a variable refuses it only if it
// holds a value.
func TestRefusesFlat(t *testing.T) {
	read := func(l string) Stmt { return &Set{Dst: []Place{{Name: "x"}}, Values: []Expr{L(l)}} }
	exit := &Exit{Label: "fail"}
	ints := []Param{P("x", TInt)}
	for _, c := range []struct {
		name string
		body []Stmt
		vars []Param
		want bool
	}{
		{"no read", []Stmt{read("x")}, ints, false},
		{"reads the start", []Stmt{read("s")}, ints, true},
		{"reads the context", []Stmt{read("c")}, ints, true},
		{"reads the start after an exit", []Stmt{exit, read("s")}, ints, false},
		{"a variable of a value", []Stmt{read("x")}, []Param{P("v", THandle(prim.TValue))}, true},
		{"a variable of a mark", []Stmt{read("x")}, []Param{P("m", THandle(prim.TMark))}, false},
	} {
		f := storesFunc(t, []string{"x", "s", "c"}, c.body...)
		if got := RefusesFlat(f, "s", "c", c.vars); got != c.want {
			t.Errorf("%s: RefusesFlat = %v, want %v", c.name, got, c.want)
		}
	}
	if f := storesFunc(t, []string{"x", "s"}, read("s")); RefusesFlat(f, "", "", nil) {
		t.Error("a function without entry variables is refused for a read of a local")
	}
}

// module declares a log component whose operations record calls, which the functions of the tests
// call.
func module() *Module {
	m := NewModule("test", "budget", "typed")
	m.AddComponent("log", "The calls made.")
	m.AddOp(&Op{Name: "log.note", Component: "log", Params: []Param{P("x", TInt)}, Writes: []string{"log"}, Contract: "Records x."})
	m.AddOp(&Op{Name: "log.charge", Component: "log", Features: []string{"budget"}, Writes: []string{"log"}, Contract: "Records a charge."})
	m.AddOp(&Op{Name: "log.flag", Component: "log", Results: []Type{TBool}, Writes: []string{"log"}, Contract: "Records a call and returns true."})
	return m
}
