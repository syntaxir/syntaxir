package vmlower

import (
	"fmt"
	"io"

	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/runtime/program"
)

// Input is what the bytecode lowering reads of a checked grammar: its run-time program, its
// elaborated program and its planned Node table at the end of the Flow stage. A bytecode module is
// the run-time program in serialized form with the Flow bodies of its rules and the code of its
// action expressions: the lowering writes the program's rule table, variables, start rules, run keys
// and struct zeros as they are, and the bodies it selects from the functions of the table. It takes
// nothing else, and it does not import the compiler that checks the grammar.
type Input struct {
	// Run is the run-time program of the grammar, which the checking builds: its rule table (the
	// rules, then their value-free twins, whose IDs are the bytecode rule IDs), variables, start
	// rules, run-site keys, struct zeros and output settings.
	Run *program.Program
	// Elab is the elaborated program of the grammar. It answers the environment-free sites
	// (EnvFree), the checker of the types and variables (Checker), and whether the grammar uses
	// #missing (MissingTokens). The type table of the module is written from the checker, which
	// holds the declared types in source order.
	Elab elaborate.Program
	// Table is the Node table of the grammar as the VM runs it, after the Flow passes for the VM's
	// target and flow-finish: its Core IR, which the module's rule and Operators Rule tables are
	// built from, its plan with the decisions of its lowering taken, and the function of each
	// function of the plan, whose bodies the module runs.
	Table *plandata.Table[code.Flow]
}

// InputIR is the kind vm-input, which the join of the planned table of a VM target gives and the
// lowering takes. It prints as the module of its table.
var InputIR = pipe.RegisterIR(&pipe.IR[Input]{
	Name: "vm-input",
	Verify: func(in Input) error {
		if in.Run == nil || in.Elab == nil || in.Table == nil || in.Table.Plan == nil {
			return fmt.Errorf("the input of the bytecode lowering lacks its run-time program, its program or its planned table")
		}
		return nil
	},
	Print: func(w io.Writer, in Input) { plandata.FlowTable.Print(w, in.Table) },
})
