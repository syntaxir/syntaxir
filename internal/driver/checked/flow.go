package checked

import (
	"fmt"

	"zeroparse.dev/zp/internal/compile"
	"zeroparse.dev/zp/internal/driver"
	"zeroparse.dev/zp/internal/ir/code"
	"zeroparse.dev/zp/internal/ir/core"
	"zeroparse.dev/zp/internal/middleend/flowpass"
	"zeroparse.dev/zp/internal/middleend/flowplan"
	"zeroparse.dev/zp/internal/middleend/flowplan/plandata"
)

// FlowTable is the Node table of a program as the bytecode VM runs it from Flow IR: the table's Core
// IR and lowering options, its plan, and the Flow function of each rule body that the plan holds.
type FlowTable struct {
	IR *plandata.TableIR
	// Plan is the plan of the table with the decisions of its lowering taken.
	Plan *plandata.Plan
	// Bodies maps the ID of each rule whose body the plan holds to a module of the function the table
	// runs for it alone (code.Module.FuncUnit), after the Flow passes: the rule's flat function or its
	// framed body, as the plan decides.
	Bodies map[int]*code.Module
}

// FlowTable builds the Node table of the program the way the generated Go parser with budget
// controls builds its Node table, so that the VM runs the same functions: the same Core IR and
// lowering options (flowplan.BuildTable), the same plan, with its decisions (flowplan.Plan,
// flowplan.Unit), and the Core step and the Flow steps of the driver on the table at the default
// optimizations, for the VM's target (flowpass.Bytecode).
func (s *Source) FlowTable() (*FlowTable, error) {
	p := s.prog
	d := &driver.Driver{}
	t, err := flowplan.BuildTable(p, plandata.TableConfig{Values: true, Budget: true}, func(build func() (*core.Module, error)) (*core.Module, error) {
		return d.Core("rules", build)
	})
	if err != nil {
		return nil, fmt.Errorf("internal error: Core IR: %w", err)
	}
	rules := append(append([]*compile.Rule(nil), p.RuleList...), p.Twins...)
	plan := flowplan.Plan(t, planRules(rules), nil)
	lt, err := d.LowerFlowTable("rules", flowplan.Unit(plan, t, nil), false, flowpass.Bytecode)
	if err != nil {
		return nil, fmt.Errorf("table rules: %w", err)
	}
	ft := &FlowTable{IR: t, Plan: lt.Plan, Bodies: map[int]*code.Module{}}
	for _, r := range rules {
		if i, ok := lt.Index(plandata.Key{Use: plandata.RuleBody, Rule: r.ID}); ok {
			ft.Bodies[r.ID] = lt.Module.Module().FuncUnit(i)
		}
	}
	return ft, nil
}

// planRules returns what the plan of a table reads of the compiled rules (flowplan.Rule).
func planRules(rules []*compile.Rule) []*flowplan.Rule {
	out := make([]*flowplan.Rule, len(rules))
	for i, r := range rules {
		out[i] = &flowplan.Rule{ID: r.ID, Name: r.Name, Body: r.Def.Expr, Action: r.Action, Plain: r.Plain, Lean: r.Lean,
			Novalue: r.Novalue, Leader: r.Leader}
	}
	return out
}
