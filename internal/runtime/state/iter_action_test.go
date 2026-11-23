package state_test

import (
	"testing"

	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/actionlower"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

// TestEngineValueIterTable runs the iteration table (conform.IterCases) on the engine's compositions itemAt and
// filterKeep (actionItemAt and actionFilterKeep, printed from Exec IR), as action expressions compiled and run by the engine, which run the loops of the lowered built-ins.
func TestEngineValueIterTable(t *testing.T) {
	for _, controlled := range []bool{false, true} {
		prog := &state.SharedProgram{}
		cp := compiletest.Program(t, "@start\nrule main = \"a\"\n", check.Options{})
		state.InitSharedProgram(prog, cp.Runtime())
		p := &state.Parser{Prog: prog, Input: state.NewInput(conform.BuiltinInput, input.Bytes)}
		if controlled {
			p.Control = state.NewBudgetControl(nil, runtime.Limits{Compute: 1 << 62})
		}
		c := &state.EvalCtx{P: p}
		one, _ := state.Element(c, "list", 1)
		two, _ := state.Element(c, "list", 2)
		vals := map[string]any{
			conform.MemberNil:     nil,
			conform.MemberInt:     1,
			conform.BuiltinString: "ab",
			conform.BuiltinBool:   true,
			conform.MemberList:    c.ListNode(nil),
			conform.BuiltinPair:   c.ListNode([]*value.Node{one, two}),
		}
		// The budget-controls reference build charges nothing, so its units are not checked.
		err := conform.RunIters("engine", false, func(call, v string) (string, int) {
			got, err := iterate(c, cp, call, vals[v])
			if err != nil {
				return "error: " + err.Error(), 0
			}
			return state.EngineMemberCanon(got), 0
		})
		if err != nil {
			t.Errorf("controlled=%v: %v", controlled, err)
		}
	}
}

// iterate runs the call of the iteration table over x as an action expression: the built-in over the value x, with the function
// of the table, which returns the element it is passed (for filter, whether it is the int 1). The action expression is
// compiled and run by the engine, so the loop that runs is the production loop. The action expression's own charges (its
// subexpressions) are part of what the parser spends, so the table's steps are not checked here; the charge of each
// element is held by the action expression charge goldens.
func iterate(c *state.EvalCtx, cp *frontend.Result, call string, x any) (any, error) {
	op := func(name string) *ir.Local { return &ir.Local{Kind: ir.LocalOp, Name: name} }
	lambda := func(params ...string) (*ir.Lambda, *ir.Local) {
		l := &ir.Lambda{Params: params}
		e := &ir.Local{Kind: ir.LocalParam, Name: params[len(params)-1], Lambda: l}
		l.Body = e
		return l, e
	}
	var actionExpr ir.ActionExpr
	switch call {
	case conform.CallFoldl, conform.CallFoldr:
		f, _ := lambda("acc", "e")
		actionExpr = &ir.Builtin{Func: call, Args: []ir.ActionExpr{&ir.Const{Value: nil}, op("x"), f}}
	case conform.CallMap:
		f, _ := lambda("e")
		actionExpr = &ir.Builtin{Func: call, Args: []ir.ActionExpr{op("x"), f}}
	case conform.CallFilter:
		f, e := lambda("e")
		f.Body = &ir.Binary{Op: "==", L: e, R: &ir.Const{Value: 1}}
		actionExpr = &ir.Builtin{Func: call, Args: []ir.ActionExpr{op("x"), f}}
	default: // conform.CallFilterSelf: the function returns the element, which is not a bool
		f, _ := lambda("e")
		actionExpr = &ir.Builtin{Func: conform.CallFilter, Args: []ir.ActionExpr{op("x"), f}}
	}
	l, err := actionlower.Lower(actionExpr, actionlower.Options{Visits: true})
	if err != nil {
		panic(err)
	}
	prog, err := vmlower.ActionProgram(l.Module, l.Func, vmlower.ActionEnv{Checker: cp.Checker(), Vars: cp.Vars, Scope: &program.Scope{}})
	if err != nil {
		panic(err)
	}
	return prog.Eval(&state.EvalCtx{P: c.P, Locals: &state.Local{Name: "x", Val: x}})
}
