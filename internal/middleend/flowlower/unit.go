package flowlower

import (
	"fmt"

	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/code/flow"
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/middleend/flowlower/lowerdata"
	"zeroparse.dev/zp/internal/pass"
	"zeroparse.dev/zp/internal/pass/pipe"
)

// The lowering as a step of the pipeline of a compilation.
//
// A lowerdata.Unit is what one lowering takes: the functions of a table, each with the arguments of
// Lower, LowerFrame or LowerOutline and the form that selects among them. Lowering is the lowering
// as the required pass "flowlower" from the Core stage to the Flow stage: it lowers a
// lowerdata.Unit to one module of its functions, and the Flow verifier (Verify), its postcondition,
// checks the result before any Flow pass sees it.

// LowerUnit lowers the functions of u to one module at the Flow stage, in the order of u.Funcs, less
// the functions that u.Refuse refuses, each in the place of the function it replaces. It does not
// verify the module: Verify, the postcondition of the lowering, does.
func LowerUnit(u lowerdata.Unit) (*code.Module, error) {
	var out *code.Module
	for i, f := range u.Funcs {
		m, err := lowerFunc(u.Module, f, u.Opts)
		if err != nil {
			return nil, err
		}
		if f.Form == lowerdata.Flat && u.Refuse != nil && u.Refuse(i, flow.FuncOf(m)) {
			if f.Fallback == nil {
				continue
			}
			if m, err = lowerFunc(u.Module, *f.Fallback, u.Opts); err != nil {
				return nil, err
			}
		}
		if out == nil {
			out = m
			continue
		}
		out.Funcs = append(out.Funcs, m.Funcs...)
	}
	if out == nil {
		out = code.EnterFlow(flow.NewModule("flow")).Module()
	}
	return out, nil
}

// lowerFunc lowers the function f of the Core module m with the options opts to a module of one
// function.
func lowerFunc(m *core.Module, f lowerdata.Func, opts lowerdata.Options) (*code.Module, error) {
	switch f.Form {
	case lowerdata.Flat:
		return lowerFlat(m, f.Rule, f.Name, opts)
	case lowerdata.Frame, lowerdata.Outline:
		return lowerFrameBody(m, f.Rule, f.Body, f.Slots, f.Build, f.Stream, f.Name, opts)
	}
	return nil, fmt.Errorf("rule %s: no lowering of the form %d", f.Rule.Name, f.Form)
}

// Verify checks a module that the lowering made with the Flow verifier. A module it rejects is an
// internal error of the lowering.
func Verify(m *code.Module) error {
	if err := flow.VerifyModule(m); err != nil {
		return fmt.Errorf("internal error: %w", err)
	}
	return nil
}

// verified returns the module of a lowering once Verify accepts it.
func verified(m *code.Module, err error) (*code.Module, error) {
	if err == nil {
		err = Verify(m)
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// Name is the name of the lowering as a step of the pipeline.
const Name = "flowlower"

// Planned is the kind of a lowerdata.Unit: the planned functions of a table and its Core module, which the
// lowering takes.
var Planned = pipe.RegisterIR(&pipe.IR[lowerdata.Unit]{Name: "planned"})

// Lowering is the lowering as the required step Name from a lowerdata.Unit to a module at the Flow stage
// (code.FlowStage), whose verifier, the postcondition of the lowering, checks the result before any
// Flow pass sees it. A failure of the lowering ends the run.
var Lowering = pipe.New(pipe.Def[lowerdata.Unit, code.At[code.Flow]]{
	Decl: pipe.Decl{Name: Name, Required: true, Obligation: "The module holds the functions of the unit, lowered to the Flow dialect."},
	In:   Planned, Out: code.FlowStage,
	Run: func(_ *pipe.Ctx[lowerdata.Unit], u lowerdata.Unit) (code.At[code.Flow], pass.PreservedSet, error) {
		m, err := LowerUnit(u)
		if err != nil {
			return code.At[code.Flow]{}, pass.PreserveNone(), err
		}
		return code.At[code.Flow]{}.With(m), pass.PreserveNone(), nil
	},
})
