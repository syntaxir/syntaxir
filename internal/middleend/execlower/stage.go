package execlower

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The conversion as a step of the pipeline of a compilation.
//
// Lowering is the conversion as the required pass "execlower" from the Flow stage to the Exec stage:
// it converts the module of the functions of a table (plandata.Table), and the verifier of the Exec
// stage, its postcondition, checks the result before any Exec pass sees it. The printers print each function of
// the result as a lowered function (execdata.View), and the passes of a target take each function so
// (execdata.LoweredFuncs).

// Name is the name of the conversion as a step of the pipeline.
const Name = "execlower"

// Lowering is the conversion as the required step Name from a table at the Flow stage to the table at
// the Exec stage (plandata.ExecTable), whose verifier checks its module before any Exec pass sees it.
// It converts the module of the table as lower does, moving its functions to the result, and keeps
// the plan of the table.
var Lowering = pipe.New(pipe.Def[*plandata.Table[code.Flow], *plandata.Table[code.Exec]]{
	Decl: pipe.Decl{Name: Name, Required: true, Obligation: "The functions of the module run the Flow operations they ran, as primitives of the runtime."},
	In:   plandata.FlowTable, Out: plandata.ExecTable,
	Run: func(c *pipe.Ctx[*plandata.Table[code.Flow]], t *plandata.Table[code.Flow]) (*plandata.Table[code.Exec], pass.PreservedSet, error) {
		out, err := lower(t.Module.Module(), cancel.New(c.Context()))
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		x, err := plandata.NewTable(t.Plan, t.IR, code.At[code.Exec]{}.With(out))
		return x, pass.PreserveNone(), err
	},
})
