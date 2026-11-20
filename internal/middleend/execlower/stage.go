package execlower

import (
	"io"

	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/code/flow"
	"zeroparse.dev/zp/internal/pass"
)

// The conversion as a step of the pipeline of a compilation.
//
// Lowering is the conversion as the required pass "execlower" from the Flow stage to the Exec stage:
// it converts a module of the functions of a table, and Verify, its postcondition, checks the result
// before any Exec pass sees it. The printers print each function of the result as a Lowered (View).

// View returns the function i of the Exec module m, which the conversion made, as a lowered function:
// with m and the variables of the function (flow.Attrs.Vars), which the conversion keeps.
func View(m *code.Module, i int) *Lowered {
	f := m.Funcs[i]
	return &Lowered{Module: m, Func: f, Vars: flow.AttrsOf(f).Vars}
}

// ModuleIR describes the Exec module of a table to the manager of the pipeline: it is verified by
// Verify and dumped as the listing of each of its functions, labeled with its table.
var ModuleIR = pass.IR[*code.Module]{Stage: Stage, Verify: Verify, Print: code.PrintFuncs, Copy: (*code.Module).Clone}

// IR describes a lowered function to the manager of the pipeline: its module is verified by Verify
// and must hold the function with its variables (Lowered.Keeps), and it is dumped as the listing of
// each function of its module, labeled with its table.
var IR = pass.IR[*Lowered]{
	Stage: Stage,
	// The module verifies, and holds the function with its variables (Lowered.Keeps).
	Verify: func(l *Lowered) error {
		if err := Verify(l.Module); err != nil {
			return err
		}
		return l.Keeps()
	},
	Print: func(w io.Writer, l *Lowered) { code.PrintFuncs(w, l.Module) },
}

// Name is the name of the conversion as a step of the pipeline.
const Name = "execlower"

// Lowering is the conversion as the required step Name from the Flow stage to the Exec stage. It
// converts the module as lower does, moving its functions to the result.
var Lowering = pass.Lower(Name, flow.Stage, ModuleIR, lower)
