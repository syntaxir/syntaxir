package print

import (
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/pass"
)

// TestFlowExecPrintsStepAsUnitsCheck checks that the step of a loop's iteration prints as the check of
// the counter of compute units, and counts none: the events of the iteration count them.
func TestFlowExecPrintsStepAsUnitsCheck(t *testing.T) {
	t.Parallel()
	f := framed(t, "steps", nil, &code.Do{Call: &code.Call{Callee: flow.OpStep}}, &code.Do{Call: &code.Call{Callee: flow.OpStep}})
	text, _, _ := testGenerator(pass.Settings{}).printModule(t, f)
	if strings.Count(text, tsUnitsCheck) != 2 || strings.Contains(text, "p.units +") || strings.Contains(text, "p.units++") {
		t.Errorf("two steps do not print as two checks of the counter:\n%s", text)
	}
}

// framed returns a module at the Flow stage of Code IR of one framed function name with the
// variables vars and the body, as the lowering writes it: the body is the block of failure, which ends
// by returning the null value and true, and the function returns the null value and false after it.
func framed(t *testing.T, name string, vars []code.Param, body ...code.Stmt) *code.Module {
	t.Helper()
	value := code.THandle(prim.TValue)
	at := &flow.Attrs{Frame: true, Fail: "fail"}
	for _, v := range vars {
		at.Vars = append(at.Vars, v.Name)
	}
	fail := &code.Block{Label: at.Fail, Stmts: append(body, &code.Return{Values: []code.Expr{code.Null(value), code.BoolC(true)}})}
	m := code.EnterFlow(flow.NewModule("flow " + name)).Module()
	m.Funcs = []*code.Func{{Name: name, Locals: vars, Results: []code.Type{value, code.TBool}, Attrs: at,
		Body: &code.Block{Stmts: []code.Stmt{fail, &code.Return{Values: []code.Expr{code.Null(value), code.BoolC(false)}}}}}}
	if err := flow.VerifyModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}
