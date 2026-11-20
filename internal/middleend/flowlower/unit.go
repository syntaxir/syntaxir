package flowlower

import (
	"fmt"

	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/code/flow"
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/middleend/corepass"
	"zeroparse.dev/zp/internal/pass"
)

// The lowering as a step of the pipeline of a compilation.
//
// A Unit is what one lowering takes: the functions of a table, each with the arguments of Lower,
// LowerFrame or LowerOutline and the form that selects among them. Lowering is the lowering as the
// required pass "flowlower" from the Core stage to the Flow stage: it lowers a Unit to one module of
// its functions, and the Flow verifier (Verify), its postcondition, checks the result before any Flow
// pass sees it.

// Form selects the function a Unit is lowered to.
type Form int

const (
	// Flat is a local function of a rule (Lower): Node, Slots and Build are unused.
	Flat Form = iota
	// Frame is a framed function (LowerFrame).
	Frame
	// Outline is a function that a CallOutline of a framed function runs (LowerOutline).
	Outline
)

// Func is one function to lower, as the planner of a table (flowplan.FuncSpec) lists it: the rule
// Rule in the form Form, named Name. A framed function or an outline lowers the node Body with the
// capture scope Slots, building its value if Build; a flat function lowers the body of Rule, and
// Body, Slots and Build are unused.
type Func struct {
	Form  Form
	Rule  *core.Rule
	Name  string
	Body  core.Node
	Slots []core.Slot
	Build bool
	// Stream lowers the #stream repetition (core.Stream) of a framed Node function as the repetition that
	// delivers its elements in a stream parse: it hands each committed element to the callback instead of
	// gathering it, when the invocation streams (StreamOn), and gathers it otherwise. The function is the
	// body of the start rule that ParseStream runs.
	Stream bool
	// Fallback is, for a flat function, the function that the table runs in its place if the
	// function is refused (Unit.Refuse), or nil if the rule has none.
	Fallback *Func
}

// Unit is what one run of the lowering takes: the Core module Module of a table, on which Analyze
// must have run, the options Opts of its functions, and the functions Funcs in order. The lowering
// only reads Module, so the units of the tables of a program may share it.
type Unit struct {
	Module *core.Module
	Opts   Options
	Funcs  []Func
	// Refuse, if not nil, decides on each flat function right after it is lowered, before any pass:
	// it is called with the index of the function in Funcs and the function as the lowering wrote
	// it, and a function that it refuses is replaced in the module by its lowered Fallback, or left
	// out if it has none. The planner of the table decides (flowplan.Refused).
	Refuse func(i int, f *code.Func) bool
}

// LowerUnit lowers the functions of u to one module at the Flow stage, in the order of u.Funcs, less
// the functions that u.Refuse refuses, each in the place of the function it replaces. It does not
// verify the module: Verify, the postcondition of the lowering, does.
func LowerUnit(u Unit) (*code.Module, error) {
	var out *code.Module
	for i, f := range u.Funcs {
		m, err := lowerFunc(u.Module, f, u.Opts)
		if err != nil {
			return nil, err
		}
		if f.Form == Flat && u.Refuse != nil && u.Refuse(i, flow.FuncOf(m)) {
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
		out = flow.NewModule("flow")
	}
	return out, nil
}

// lowerFunc lowers the function f of the Core module m with the options opts to a module of one
// function.
func lowerFunc(m *core.Module, f Func, opts Options) (*code.Module, error) {
	switch f.Form {
	case Flat:
		return lowerFlat(m, f.Rule, f.Name, opts)
	case Frame, Outline:
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

// IR describes a module of Flow functions to the manager of the pipeline: it is verified by Verify
// and dumped as the listing of each of its functions, labeled with its table.
var IR = pass.IR[*code.Module]{Stage: flow.Stage, Verify: Verify, Print: code.PrintFuncs, Copy: (*code.Module).Clone}

// Name is the name of the lowering as a step of the pipeline.
const Name = "flowlower"

// Lowering is the lowering as the required step Name from the Core stage to the Flow stage.
var Lowering = pass.Lower(Name, corepass.Stage, IR, LowerUnit)
