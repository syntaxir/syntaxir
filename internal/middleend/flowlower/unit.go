package flowlower

import (
	"fmt"
	"slices"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/prim"
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
	return lowerUnit(u, core.ActionSitesOf(u.Module), newTables(u.Module))
}

// lowerUnit is LowerUnit with the action sites and the tables tb of u.Module.
func lowerUnit(u lowerdata.Unit, sites *core.ActionSites, tb *tables) (*code.Module, error) {
	var out *code.Module
	for i, f := range u.Funcs {
		m, err := lowerFunc(u.Module, sites, tb, f, u.Opts)
		if err != nil {
			return nil, err
		}
		if f.Form == lowerdata.Flat && u.Refuse != nil && u.Refuse(i, flow.FuncOf(m)) {
			if f.Fallback == nil {
				continue
			}
			if m, err = lowerFunc(u.Module, sites, tb, *f.Fallback, u.Opts); err != nil {
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
		out.Attrs = tb.t
	}
	return out, nil
}

// lowerFunc lowers the function f of the Core module m, whose action sites are sites and tables tb,
// with the options opts to a module of one function.
func lowerFunc(m *core.Module, sites *core.ActionSites, tb *tables, f lowerdata.Func, opts lowerdata.Options) (*code.Module, error) {
	switch f.Form {
	case lowerdata.Flat:
		return lowerFlat(m, sites, tb, f.Rule, f.Name, opts)
	case lowerdata.Frame, lowerdata.Outline:
		return lowerFrameBody(m, sites, tb, f.Rule, f.Body, f.Slots, f.Build, f.Stream, f.Name, opts)
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
	Run: func(c *pipe.Ctx[plandata.Planned], p plandata.Planned) (*plandata.Table[code.Flow], pass.PreservedSet, error) {
		t, err := lowerTable(p, cancel.New(c.Context()))
		return t, pass.PreserveNone(), err
	},
})

// lowerTable lowers the functions of the plan of p to one module at the Flow stage (LowerUnit), with
// the Core IR and the options of the table, and returns the table of the module and of the plan with
// the decisions of the lowering taken: a flat function that p.Refuse refuses is replaced by its
// fallback, or left out if its rule has none (plandata.Plan.Refusing). The plan of p is unchanged. It
// does not verify the module: Verify, the postcondition of the lowering, does. The lowering polls
// control, which may be nil, for each Core node it lowers and each statement it emits.
func lowerTable(p plandata.Planned, control *cancel.Control) (*plandata.Table[code.Flow], error) {
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
	sites := core.ActionSitesOf(p.IR.M)
	tb := newTables(p.IR.M)
	tb.control = control
	m, err := lowerUnit(u, sites, tb)
	if err != nil {
		return nil, err
	}
	plan := p.Plan.Refusing(refused)
	outlined, calls, err := lowerOutlined(plan, p.IR, sites, tb, m)
	if err != nil {
		return nil, err
	}
	return plandata.NewTable(plan.Outlining(outlined, calls), p.IR, code.At[code.Flow]{}.With(m))
}

// lowerOutlined lowers the nodes that the functions of the module m, which holds the functions of the
// plan in order, run as functions of their own (flow.CallOutline), and appends them to m: the calls
// of each function in the order of its code, after the functions of the plan, and the calls that
// those make after them in turn, with the action sites sites and the tables tb of the table's
// module, which number the calls. A node is lowered once for each frame and building of its calls,
// one number. It returns the functions it added, each with the Core rule of the function that calls
// it, keyed with the use plandata.Outlined and named after that function, and the number of the
// call of each.
func lowerOutlined(plan *plandata.Plan, ir *plandata.TableIR, sites *core.ActionSites, tb *tables, m *code.Module) ([]*plandata.FuncSpec, []int, error) {
	if len(ir.Opts.Outlined) == 0 {
		return nil, nil, nil
	}
	specs := slices.Clone(plan.Funcs)
	var added []*plandata.FuncSpec
	var numbers []int
	lowered := map[int]bool{}
	lines := map[int]int{}
	for i := 0; i < len(m.Funcs); i++ {
		caller := specs[i]
		var calls []int
		code.Walk(m.Funcs[i].Body, nil, func(e code.Expr) {
			if c, ok := e.(*code.Call); ok && c.Callee == flow.OpCallOutline {
				calls = append(calls, c.Imm.(prim.Outline).Call)
			}
		})
		n := 0
		for _, c := range calls {
			if lowered[c] {
				continue
			}
			lowered[c] = true
			o := tb.outlines[c]
			k := plandata.Key{Use: plandata.Outlined, Rule: caller.Key.Rule, Line: lines[caller.Key.Rule]}
			lines[caller.Key.Rule]++
			s := &plandata.FuncSpec{Key: k, Func: lowerdata.Func{Form: lowerdata.Outline, Rule: caller.Rule,
				Name: fmt.Sprintf("%s/outline%d", caller.Name, n), Body: o.node, Slots: o.slots, Build: o.build}}
			n++
			f, err := lowerFunc(ir.M, sites, tb, s.Func, ir.Opts)
			if err != nil {
				return nil, nil, err
			}
			m.Funcs = append(m.Funcs, f.Funcs...)
			specs = append(specs, s)
			added = append(added, s)
			numbers = append(numbers, c)
		}
	}
	return added, numbers, nil
}
