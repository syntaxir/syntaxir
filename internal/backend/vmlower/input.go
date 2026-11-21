package vmlower

import (
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/runtime/program"
)

// Input is what the bytecode lowering reads of a checked grammar: its run-time program and the
// source of its Core IR. A bytecode module is the run-time program in serialized form with the code
// of its rules and terms: the lowering writes the program's rule table, variables, start rules, run
// keys and struct zeros as they are, and the code it emits from the Core IR. It takes nothing else,
// and it does not import the compiler that checks the grammar.
type Input struct {
	// Run is the run-time program of the grammar, which the checking builds: its rule table (the
	// rules, then their value-free twins, whose IDs are the bytecode rule IDs), variables, start
	// rules, run-site keys, struct zeros and output settings.
	Run *program.Program
	// Elab is the program the Core IR of the grammar is elaborated from (CoreOf). It also
	// answers the environment-free sites (EnvFree), the checker of the types and variables
	// (Checker), and whether the grammar uses #missing (MissingTokens). The type table of the
	// module is written from the checker, which holds the declared types in source order.
	Elab elaborate.Program
}
