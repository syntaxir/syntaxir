package vmlower

import (
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/code"
	ir "syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/runtime/program"
)

// Input is what the bytecode lowering reads of a checked grammar: its run-time program, its
// elaborated program and its planned Node table. A bytecode module is the run-time program in
// serialized form with the Flow bodies of its rules and the code of its action expressions: the lowering writes
// the program's rule table, variables, start rules, run keys and struct zeros as they are, and the
// bodies it selects from the functions of the table. It takes nothing else, and it does not import
// the compiler that checks the grammar.
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
	// Table is the planned Node table of the grammar, whose functions are the bodies of the module.
	Table Table
}

// Table is the Node table of a grammar as the VM runs it (checked.FlowTable): the Core IR of the
// table, which the module's rule and Operators Rule tables are built from, its plan with the
// decisions of its lowering taken, and the module of each planned function by its key, after the
// Flow passes for the VM.
type Table struct {
	Core  *ir.Module
	Plan  *plandata.Plan
	Funcs map[plandata.Key]*code.Module
}
