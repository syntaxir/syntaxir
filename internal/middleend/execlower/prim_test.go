package execlower

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
)

// TestEveryFlowOperationHasItsPrimitive is the specification test of the primitives (design record
// 073): every Flow operation has the constant of its primitive in package prim, whose immediate is of
// the type of the operation's, and every primitive stands for a Flow operation.
func TestEveryFlowOperationHasItsPrimitive(t *testing.T) {
	ops := map[string]bool{}
	for _, o := range flow.Ops() {
		ops[o.Name] = true
		name := Prim(o.Name)
		imm, ok := prim.ImmType(name)
		if !ok {
			t.Errorf("the Flow operation %s has no primitive %s", o.Name, name)
			continue
		}
		if imm != o.ImmType {
			t.Errorf("the immediate of %s is a %v, of the primitive %s a %v", o.Name, o.ImmType, name, imm)
		}
		if FlowOp(name) != o.Name {
			t.Errorf("FlowOp(%s) = %s, want %s", name, FlowOp(name), o.Name)
		}
	}
	for _, name := range prim.OpNames() {
		if !ops[FlowOp(name)] {
			t.Errorf("the primitive %s stands for no Flow operation", name)
		}
	}
	if len(prim.OpNames()) != len(ops) {
		t.Errorf("%d primitives for %d Flow operations", len(prim.OpNames()), len(ops))
	}
}

// TestPrimOpChecksThePrimitive is the negative control of the mapping: a Flow operation without a
// primitive, and one whose immediate is not of the type of its primitive's, do not convert.
func TestPrimOpChecksThePrimitive(t *testing.T) {
	unknown := &code.Op{Name: flow.Dialect + ".undeclared", Dialect: flow.Dialect}
	if _, err := PrimOp(unknown); err == nil || !strings.Contains(err.Error(), "has no primitive") {
		t.Errorf("an operation without a primitive: %v", err)
	}
	o := flow.Ops()[slices.IndexFunc(flow.Ops(), func(o *code.Op) bool { return o.Name == flow.OpMatchLit })]
	o.ImmType = reflect.TypeFor[*core.Class]()
	if _, err := PrimOp(o); err == nil || !strings.Contains(err.Error(), "the immediate of the Flow operation flow.matchLit") {
		t.Errorf("an immediate of another type: %v", err)
	}
	m := flowModule()
	m.AddOp(unknown)
	if _, err := Lower(m); err == nil || !strings.Contains(err.Error(), "has no primitive") {
		t.Errorf("Lower of a module that declares an operation without a primitive: %v", err)
	}
}

// TestLowerConvertsTheAttributes checks that the conversion gives each function the Exec-stage
// attributes with the value of every Flow attribute of the same name, and that the Exec-stage
// attributes have every Flow attribute but the forms: a field added to one and not the other fails.
func TestLowerConvertsTheAttributes(t *testing.T) {
	ft, pt := reflect.TypeFor[flow.Attrs](), reflect.TypeFor[prim.Attrs]()
	for i := range ft.NumField() {
		f := ft.Field(i)
		if f.Name == "Forms" {
			if _, ok := pt.FieldByName(f.Name); ok {
				t.Error("the Exec-stage attributes keep the forms")
			}
			continue
		}
		if p, ok := pt.FieldByName(f.Name); !ok || p.Type != f.Type {
			t.Errorf("the Flow attribute %s %v is not an Exec-stage attribute of its type", f.Name, f.Type)
		}
	}
	if pt.NumField() != ft.NumField()-1 {
		t.Errorf("%d Exec-stage attributes for %d Flow attributes and the forms", pt.NumField(), ft.NumField())
	}

	// Every field holds a value other than its zero value, so a field the conversion drops fails.
	in := &flow.Attrs{Rule: 1, Vars: []string{"ok"}, Start: "s", Result: "v", Ctx: "c",
		Fail: "fail", Cut: "k", Binds: true, Frame: true, Node: true, Document: true, Traced: true}
	iv := reflect.ValueOf(in).Elem()
	for i := range iv.NumField() {
		if f := ft.Field(i); f.Name != "Forms" && iv.Field(i).IsZero() {
			t.Fatalf("the input leaves the Flow attribute %s zero", f.Name)
		}
	}
	m := flowModule()
	m.Funcs[0].Attrs = in
	x, err := Lower(m)
	if err != nil {
		t.Fatal(err)
	}
	out := prim.AttrsOf(x.Funcs[0])
	if out == nil {
		t.Fatalf("the function has attributes of the type %T", x.Funcs[0].Attrs)
	}
	ov := reflect.ValueOf(out).Elem()
	for i := range ov.NumField() {
		name := pt.Field(i).Name
		if !reflect.DeepEqual(ov.Field(i).Interface(), iv.FieldByName(name).Interface()) {
			t.Errorf("the attribute %s is %v, want %v", name, ov.Field(i), iv.FieldByName(name))
		}
	}
	if l := execdata.View(x, 0); !slices.Equal(l.Vars, in.Vars) {
		t.Errorf("the lowered function has the variables %v, want %v", l.Vars, in.Vars)
	}
}

// TestLowerRejectsTheForms checks that a function whose forms the Flow passes have not removed does
// not convert: the forms are consumed at the Flow stage.
func TestLowerRejectsTheForms(t *testing.T) {
	m := flowModule()
	m.Funcs[0].Attrs = &flow.Attrs{Forms: &flow.Forms{}}
	if _, err := Lower(m); err == nil || !strings.Contains(err.Error(), "f: the forms of the lowering are left") {
		t.Errorf("a function with forms: %v", err)
	}
}

// TestExecVerifierRejectsFlowAttributes is the negative control of the Exec stage's check: a function
// at the Exec stage that keeps attributes of another type, such as the Flow attributes, fails the
// verifier of the stage, the conversion's postcondition and the kind of a lowered function.
func TestExecVerifierRejectsFlowAttributes(t *testing.T) {
	l := lowered(t)
	if err := code.ExecStage.Verify(code.At[code.Exec]{}.With(l.Module)); err != nil {
		t.Fatalf("the converted module: %v", err)
	}
	l.Func.Attrs = &flow.Attrs{Vars: l.Vars}
	const want = "f: attributes of the type *flow.Attrs at the Exec stage, not *prim.Attrs"
	if err := code.ExecStage.Verify(code.At[code.Exec]{}.With(l.Module)); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("the kind of the Exec stage: %v", err)
	}
	if err := execdata.Verify(l.Module); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("the postcondition of the conversion: %v", err)
	}
	if err := execdata.LoweredIR.Verify(l); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("the kind of a lowered function: %v", err)
	}
}
