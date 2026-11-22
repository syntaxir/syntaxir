package print

import (
	"context"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	tspass "syntaxir.dev/sxir/internal/backend/codegen/typescript/pass"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/middleend/execlower"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// testGenerator returns a generator of tables lowered with the settings opt.
func testGenerator(opt pass.Settings) *Generator {
	return &Generator{Generator: common.Generator{Settings: opt}}
}

// printModule prints the one function of the module f at the Flow stage as printTSFlowExec prints a
// function of the table: converted to the Exec stage with the Exec passes and the TypeScript passes
// under the settings of g, as the lowering of a table converts it. f is consumed.
func (g *Generator) printModule(t *testing.T, f *code.Module) (string, []string, []tsDecl) {
	t.Helper()
	// The module is at the Flow stage and no plan holds its functions.
	in, err := plandata.NewTable(nil, nil, code.At[code.Flow]{}.With(f))
	if err != nil {
		t.Fatal(err)
	}
	exec := pipe.Then(pipe.Of(execlower.Lowering), pipe.Within(plandata.ExecModule,
		pipe.WithConst(flowpass.ObserveFeature, false, pipe.Then(codepass.Pipeline(), pipe.PerFunc(execlower.LoweredFuncs, tspass.Pipeline)))))
	x, err := pipe.Execute(context.Background(), &pipe.Run{Settings: g.Opt()}, exec, in)
	if err != nil {
		t.Fatal(err)
	}
	return g.printTSFlowExec(execlower.View(x.Module.Module(), 0))
}

// markInIfFunc returns a framed function whose mark of the parser state is set in the body of an If
// and reset there (resetInIf) or by a later If, with the position and a capture changed in between.
// The lowering of a rule makes the mark at the level of the scope that backtracks, never inside an If
// today; the printer must still print such a mark if the lowering ever does.
func markInIfFunc(t *testing.T, resetInIf bool) *code.Module {
	t.Helper()
	do := func(name string, imm any, args ...code.Expr) code.Stmt {
		return &code.Do{Call: &code.Call{Callee: name, Imm: imm, Args: args}}
	}
	cond := func() code.Expr { return code.Bin(">=", code.R(prim.Pos), code.Int(3)) }
	reset := func() code.Stmt { return do(flow.OpReset, nil, code.L("x1")) }
	then := []code.Stmt{
		code.Assign("x1", &code.Call{Callee: flow.OpMarkState, Imm: prim.StateMark{Trail: true, Env: true}}),
		&code.Set{Dst: []code.Place{{Name: prim.Pos, Reg: true}}, Values: []code.Expr{code.Int(7)}},
		do(flow.OpSetCapture, prim.Slot(0), code.Null(code.THandle(prim.TValue))),
	}
	var body []code.Stmt
	if resetInIf {
		then = append(then, reset())
		body = append(body, &code.If{Cond: cond(), Then: &code.Block{Stmts: then}})
	} else {
		body = append(body, &code.If{Cond: cond(), Then: &code.Block{Stmts: then}},
			&code.If{Cond: cond(), Then: &code.Block{Stmts: []code.Stmt{reset()}}})
	}
	return framed(t, "markInIf", []code.Param{code.P("x1", code.THandle(prim.TMark))}, body...)
}

// TestFlowExecPrintsMarkInIfBody checks that the mark of a framed function that is set in the body of
// an If prints its parts that a Reset reads, and the Reset, in generated TypeScript.
func TestFlowExecPrintsMarkInIfBody(t *testing.T) {
	t.Parallel()
	for name, resetInIf := range map[string]bool{"reset in the If": true, "reset after the If": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := testGenerator(pass.Settings{})
			text, decls, _ := g.printModule(t, markInIfFunc(t, resetInIf))
			for _, want := range []string{"x1Pos = p.pos", "x1Env = p.env", "x1Trail = p.trail.length", "p.reset(x1Pos, x1Env, x1Trail,"} {
				if !strings.Contains(text, want) {
					t.Errorf("the printed body has no %q:\n%s", want, text)
				}
			}
			for _, want := range []string{"let x1Pos", "let x1Env", "let x1Trail"} {
				if !strings.Contains(strings.Join(decls, ""), want) {
					t.Errorf("the declarations have no %q: %q", want, decls)
				}
			}
		})
	}
}

// TestFlowExecOmitsUnreadMarkInIfBody is the negative control: a mark set in the body of an If that no
// Reset reads prints nothing, so the assertions above do not pass on any text.
func TestFlowExecOmitsUnreadMarkInIfBody(t *testing.T) {
	t.Parallel()
	f := markInIfFunc(t, true)
	// The body of the function is its block of failure; the If is its first statement.
	then := flow.FuncOf(f).Body.Stmts[0].(*code.Block).Stmts[0].(*code.If).Then
	then.Stmts = then.Stmts[:len(then.Stmts)-1] // without the Reset
	text, decls, _ := testGenerator(pass.Settings{}).printModule(t, f)
	if strings.Contains(text, "x1") || strings.Contains(strings.Join(decls, ""), "x1") {
		t.Errorf("an unread mark prints:\n%s%q", text, decls)
	}
}
