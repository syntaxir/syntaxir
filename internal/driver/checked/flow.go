package checked

import (
	"context"

	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
)

// FlowTable is the Node table of a program as the bytecode VM runs it from Flow IR: the table's Core
// IR and lowering options, its plan, and the Flow functions that the plan holds.
type FlowTable struct {
	IR *plandata.TableIR
	// Plan is the plan of the table with the decisions of its lowering taken.
	Plan *plandata.Plan
	// Funcs maps the key of each function of the plan to a module of the function alone
	// (code.Module.FuncUnit), after the Flow passes: the body of a rule, the rule's flat function or
	// its framed body as the plan decides, its stream body, and the skip and the lines of an
	// Operators Rule.
	Funcs map[plandata.Key]*code.Module
}

// FlowTable builds the Node table of the program the way the generated Go parser with budget
// controls builds its Node table, so that the VM runs the same functions (driver.FlowTable): the same
// Core IR and lowering options, the same plan with its decisions and the stream body of each rule that
// has a #stream repetition, and the Core and Flow parts of the pipeline of the table at the default
// optimizations, for the VM's target (flowpass.Bytecode).
func (s *Source) FlowTable() (*FlowTable, error) {
	lt, err := driver.FlowTable(context.Background(), s.prog)
	if err != nil {
		return nil, err
	}
	ft := &FlowTable{IR: lt.IR, Plan: lt.Plan, Funcs: map[plandata.Key]*code.Module{}}
	for i, f := range lt.Plan.Funcs {
		ft.Funcs[f.Key] = lt.Module.Module().FuncUnit(i)
	}
	return ft, nil
}
