package flowlower

import (
	"fmt"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/flow"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowlower/lowerdata"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The lowering as a step of the pipeline of a compilation.
//
// A lowerdata.Unit is what one lowering takes: the functions of a table, each with the arguments of
// Lower, LowerFrame or LowerOutline and the form that selects among them. Lowering is the lowering
// as the required pass "flowlower" from the Core stage to the Flow stage: it lowers a
// planned table (plandata.Planned) to the table of one module of its functions, and the Flow
// verifier (Verify), its postcondition, checks the module before any Flow pass sees it.

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

// Lowering is the lowering as the required step Name from a planned table to the table at the Flow
// stage (plandata.FlowTable), whose verifier, the postcondition of the lowering, checks its module
// before any Flow pass sees it. A failure of the lowering ends the run.
var Lowering = pipe.New(pipe.Def[plandata.Planned, *plandata.Table[code.Flow]]{
	Decl: pipe.Decl{Name: Name, Required: true, Obligation: "The module holds the functions of the plan, lowered to the Flow dialect."},
	In:   plandata.PlannedIR, Out: plandata.FlowTable,
	Run: func(_ *pipe.Ctx[plandata.Planned], p plandata.Planned) (*plandata.Table[code.Flow], pass.PreservedSet, error) {
		t, err := LowerTable(p)
		return t, pass.PreserveNone(), err
	},
})

// LowerTable lowers the functions of the plan of p to one module at the Flow stage (LowerUnit), with
// the Core IR and the options of the table, and returns the table of the module and of the plan with
// the decisions of the lowering taken: a flat function that p.Refuse refuses is replaced by its
// fallback, or left out if its rule has none (plandata.Plan.Refusing). The plan of p is unchanged. It
// does not verify the module: Verify, the postcondition of the lowering, does.
func LowerTable(p plandata.Planned) (*plandata.Table[code.Flow], error) {
	specs := p.Plan.Funcs
	u := lowerdata.Unit{Module: p.IR.M, Opts: p.IR.Opts, Funcs: make([]lowerdata.Func, len(specs))}
	for i, s := range specs {
		u.Funcs[i] = s.Func
		if s.Fallback != nil {
			fb := s.Fallback.Func
			u.Funcs[i].Fallback = &fb
		}
	}
	var refused []*plandata.FuncSpec
	if p.Refuse != nil {
		u.Refuse = func(i int, f *code.Func) bool {
			if !p.Refuse(specs[i].Key, f) {
				return false
			}
			refused = append(refused, specs[i])
			return true
		}
	}
	m, err := LowerUnit(u)
	if err != nil {
		return nil, err
	}
	return plandata.NewTable(p.Plan.Refusing(refused), p.IR, code.At[code.Flow]{}.With(m))
}
