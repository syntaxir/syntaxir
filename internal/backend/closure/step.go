package closure

import (
	"fmt"
	"io"

	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/code"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Input is what the last step of a closure target takes: the Core module of the program after the
// Core passes, from which every family of closures is built, and its Action module, which the Action
// segment of the pipeline lowered.
type Input struct {
	Module  *ir.Module
	Actions code.At[code.Action]
}

// InputIR is the kind closure-input, which the join of the table of a closure target gives. It
// prints as its module.
var InputIR = pipe.RegisterIR(&pipe.IR[Input]{
	Name: "closure-input",
	Verify: func(in Input) error {
		if in.Module == nil {
			return fmt.Errorf("the input of closure-core has no module")
		}
		return nil
	},
	Print: func(w io.Writer, in Input) { io.WriteString(w, ir.Print(in.Module, true)) },
})

// CoreName is the name of the last step of a closure target.
const CoreName = "closure-core"

// Core is the step CoreName, the last step of the pipeline of a closure target: it returns the Core
// module of the program and its Action module for the builds of its families (Module), the Core
// module without the cancel control of the compilation, which the module that the engine keeps must
// not poll after the parse that built it.
// It changes nothing in its input, which may be the kept result of a shared Core part: its result is
// a shallow copy of the module header that shares the rules, which the builds of the families only
// read (Build).
var Core = pipe.New(pipe.Def[Input, result.Output]{
	Decl: pipe.Decl{Name: CoreName, Required: true, Obligation: "The module is the optimized Core module of the program, without a cancel control, with its Action module."},
	In:   InputIR, Out: result.IR,
	Run: func(_ *pipe.Ctx[Input], in Input) (result.Output, pass.PreservedSet, error) {
		if in.Actions.Module() == nil {
			return nil, pass.PreserveNone(), fmt.Errorf("internal error: the input of closure-core has no Action module")
		}
		m := *in.Module
		m.Control = nil
		return &Module{Core: &m, Actions: in.Actions}, pass.PreserveNone(), nil
	},
})

// Module is the output of a closure target, which every family of its closures is built from
// (Build): the optimized Core module of the program and its Action module, whose functions are the
// programs of its action sites.
type Module struct {
	Core    *ir.Module
	Actions code.At[code.Action]
}

// Backend returns result.Closure.
func (*Module) Backend() result.Backend { return result.Closure }
