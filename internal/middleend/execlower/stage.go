package execlower

import (
	"fmt"
	"io"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The conversion as a step of the pipeline of a compilation.
//
// Lowering is the conversion as the required pass "execlower" from the Flow stage to the Exec stage:
// it converts the module of the functions of a table (plandata.Table), and the verifier of the Exec
// stage, its postcondition, checks the result before any Exec pass sees it. The printers print each function of
// the result as a Lowered (View), and the passes of a target take each function so (LoweredFuncs).

// View returns the function i of the Exec module m, which the conversion made, as a lowered function:
// with m and the variables of the function (prim.Attrs.Vars), which the conversion keeps.
func View(m *code.Module, i int) *Lowered {
	f := m.Funcs[i]
	return &Lowered{Module: m, Func: f, Vars: prim.AttrsOf(f).Vars}
}

// LoweredIR is the kind of a lowered function, which the passes of a target take and return: its
// module verifies and holds the function with its variables (Lowered.Keeps), and it is dumped as the
// listing of the function.
var LoweredIR = pipe.RegisterIR(&pipe.IR[*Lowered]{
	Name: "lowered@exec",
	Verify: func(l *Lowered) error {
		if err := Verify(l.Module); err != nil {
			return err
		}
		return l.Keeps()
	},
	Print: func(w io.Writer, l *Lowered) { fmt.Fprintf(w, "%s\n", code.FuncStringIn(l.Module, l.Func)) },
	Copy:  (*Lowered).Clone,
})

// LoweredFuncs views a module at the Exec stage as its functions, each a lowered function of the
// unit of that function (View), for the passes of a target. A pass that moved the function in its
// module, removed it or removed one of its variables fails to put it back.
var LoweredFuncs = pipe.RegisterFuncs(code.ExecStage, LoweredIR, &pipe.Funcs[code.At[code.Exec], *Lowered]{
	Name: "lowered-funcs@exec",
	Len:  func(u code.At[code.Exec]) int { return len(u.Module().Funcs) },
	Func: func(u code.At[code.Exec], i int) string { return u.Module().Funcs[i].Name },
	Get:  func(u code.At[code.Exec], i int) *Lowered { return View(u.Module().FuncUnit(i), 0) },
	Put: func(u code.At[code.Exec], i int, l *Lowered) (code.At[code.Exec], error) {
		if err := l.Keeps(); err != nil {
			return u, err
		}
		if l.Module.Funcs[0] != l.Func {
			return u, fmt.Errorf("an Exec pass moved the function %s in its module", l.Func.Name)
		}
		return u, u.Module().PutFuncUnit(i, l.Module)
	},
})

// Name is the name of the conversion as a step of the pipeline.
const Name = "execlower"

// Lowering is the conversion as the required step Name from a table at the Flow stage to the table at
// the Exec stage (plandata.ExecTable), whose verifier checks its module before any Exec pass sees it.
// It converts the module of the table as lower does, moving its functions to the result, and keeps
// the plan of the table.
var Lowering = pipe.New(pipe.Def[*plandata.Table[code.Flow], *plandata.Table[code.Exec]]{
	Decl: pipe.Decl{Name: Name, Required: true, Obligation: "The functions of the module run the Flow operations they ran, as primitives of the runtime."},
	In:   plandata.FlowTable, Out: plandata.ExecTable,
	Run: func(_ *pipe.Ctx[*plandata.Table[code.Flow]], t *plandata.Table[code.Flow]) (*plandata.Table[code.Exec], pass.PreservedSet, error) {
		out, err := lower(t.Module.Module())
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		x, err := plandata.NewTable(t.Plan, t.IR, code.At[code.Exec]{}.With(out))
		return x, pass.PreserveNone(), err
	},
})

// Func returns the function of the use k in the module of the table t, which the conversion made, as
// a lowered function (View), or false if the table has none.
func Func(t *plandata.Table[code.Exec], k plandata.Key) (*Lowered, bool) {
	i, ok := t.Index(k)
	if !ok {
		return nil, false
	}
	return View(t.Module.Module(), i), true
}
