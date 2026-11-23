package typescript

import (
	"context"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/typescript/tsprim"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// grammarFunc returns a function of grammar code with the attributes a, the variables vars and a body
// of the statements stmts.
func grammarFunc(a *prim.Attrs, vars []code.Param, stmts ...code.Stmt) *code.Func {
	for _, v := range vars {
		a.Vars = append(a.Vars, v.Name)
	}
	return &code.Func{Name: "f", Attrs: a, Locals: vars, Body: &code.Block{Stmts: stmts}}
}

// do is the statement that calls the primitive name with the immediate imm and the arguments args.
func do(name string, imm any, args ...code.Expr) code.Stmt {
	return &code.Set{Dst: []code.Place{{}}, Values: []code.Expr{&code.Call{Callee: name, Imm: imm, Args: args}}}
}

func local(name string, t prim.Type) code.Param {
	return code.Param{Name: name, Type: prim.CodeType(t)}
}

// A function with every property of printability is admitted: printed primitives, among them an
// operation of the TypeScript passes, a predicate and a bind of the frame, and variables of declared
// types, among them a mark.
func TestPrintableAdmitsPrintedFunction(t *testing.T) {
	f := grammarFunc(&prim.Attrs{Frame: true, Start: "s"},
		[]code.Param{local("n", prim.TypeInt), local("m", prim.TypeMark), local("v", prim.TypeValue)},
		do(prim.OpStep, nil),
		do(tsprim.MergeOp, nil),
		do(prim.OpPred, prim.Predicate{Capture: prim.Capture{Frame: true}}),
		do(prim.OpBind, prim.Binding{Capture: prim.Capture{Frame: true}}),
		do(prim.OpAdvance, nil, &code.Local{Name: "n"}))
	if what := unprintable(f); what != "" {
		t.Errorf("unprintable = %q, want none", what)
	}
}

// The negative controls of the properties: each function lacks one, and the analysis names it.
func TestPrintableRejectsEachProperty(t *testing.T) {
	cases := []struct {
		name string
		f    *code.Func
		want string
	}{
		{"an operation the printer does not print", grammarFunc(&prim.Attrs{}, nil, do(prim.OpEval, prim.Action{})),
			"the operation " + prim.OpEval},
		{"an operation in a condition", grammarFunc(&prim.Attrs{}, nil,
			&code.If{Cond: &code.Call{Callee: prim.OpUseCtx}, Then: &code.Block{}}),
			"the operation " + prim.OpUseCtx},
		{"captures attached from variables", grammarFunc(&prim.Attrs{}, nil, do(prim.OpAttachCaptures, prim.Attach{})),
			"the operation " + prim.OpAttachCaptures},
		{"a predicate that reads variables", grammarFunc(&prim.Attrs{}, nil,
			do(prim.OpPred, prim.Predicate{Capture: prim.Capture{Names: []string{"a"}}})),
			"a predicate that reads variables"},
		{"a bind that reads variables", grammarFunc(&prim.Attrs{}, nil,
			do(prim.OpBind, prim.Binding{Capture: prim.Capture{Names: []string{"a"}}})),
			"a bind that reads variables"},
		{"a read of the start", grammarFunc(&prim.Attrs{Start: "s"}, []code.Param{local("n", prim.TypeInt)},
			do(prim.OpAdvance, nil, &code.Local{Name: "s"})),
			"a read of the start of the rule"},
		{"a read of the context", grammarFunc(&prim.Attrs{Ctx: "c"}, nil,
			do(prim.OpAppendError, nil, &code.Local{Name: "c"})),
			"a read of the context of the rule's action"},
		{"a variable of an undeclared type", grammarFunc(&prim.Attrs{}, []code.Param{local("e", prim.TypeEnv)}),
			"the variable e of the type env"},
		{"a variable of the context's type", grammarFunc(&prim.Attrs{}, []code.Param{local("x", prim.TypeCtx)}),
			"the variable x of the type ctx"},
	}
	for _, c := range cases {
		if got := unprintable(c.f); got != c.want {
			t.Errorf("%s: unprintable = %q, want %q", c.name, got, c.want)
		}
	}
}

// The step names the function and the table of the first construct that the printer does not print,
// and leaves out the functions that are not grammar code.
func TestPrintableNamesFunctionAndTable(t *testing.T) {
	m := &code.Module{Name: "m"}
	lib := &code.Func{Name: "lib", Body: &code.Block{Stmts: []code.Stmt{do(prim.OpEval, nil)}}}
	bad := grammarFunc(&prim.Attrs{}, nil, do(prim.OpUseCtx, nil))
	bad.Name = "r1"
	m.Funcs = []*code.Func{lib, grammarFunc(&prim.Attrs{}, nil, do(prim.OpStep, nil)), bad}
	tables := &execdata.Tables{Names: []string{"rules", "recRules"}, Tables: []*plandata.Table[code.Exec]{
		{Module: code.EnterExec(&code.Module{Name: "empty"})},
		{Module: code.EnterExec(m)},
	}}
	_, err := pipe.Execute(context.Background(), &pipe.Run{}, pipe.Of(Printability), tables)
	want := "ts-printable: the TypeScript printer does not print the operation " + prim.OpUseCtx + " in the function r1 of the table recRules"
	if err == nil || err.Error() != want {
		t.Errorf("ts-printable = %v, want %q", err, want)
	}
	m.Funcs = m.Funcs[:2]
	if _, err := pipe.Execute(context.Background(), &pipe.Run{}, pipe.Of(Printability), tables); err != nil {
		t.Errorf("ts-printable without the unprinted function = %v, want nil", err)
	}
	if Printability.Name() != "ts-printable" {
		t.Errorf("the step is %s, want ts-printable", Printability.Name())
	}
}
