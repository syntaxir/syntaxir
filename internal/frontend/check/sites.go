package check

import "syntaxir.dev/sxir/pkg/grammar/ast"

// Sites numbers the skip-run, run and scan sites of a program's repetitions: the repetitions whose
// walks a parser records (see "Skip Runs", "Run Sites" and "Scan Sites" in
// spec/runtime/parsing.md). The numbering is an analysis of the Core IR module of the rules
// (internal/middleend/corepass, ComputeSites); the front end keys its result by the repetitions of
// the grammar that the module's repetitions were built from, which the Core IR of the program, its
// value-free twins included, is built from again.
type Sites struct {
	// Skip, Run and Scan number the skip-run sites from 1, the run sites after them and the scan
	// sites after those, two numbers each: one for the walks that build a value and the next for
	// those that do not.
	Skip, Run, Scan map[*ast.Repeat]int
	// NumSkip, NumRun and NumScan count them.
	NumSkip, NumRun, NumScan int
	// EnvFree[site] reports a skip-run or run site whose records are not keyed by the environment.
	EnvFree []bool
	// RunKeys holds, by site, the key of each run site that is not free of the environment.
	RunKeys map[int]RunKey
}

// RunKey holds the variables that key the records of a run site that is not free of the
// environment. Vars are the variables that its element reads, directly or in the rules it calls,
// and those that the element binds itself, Bind: a walk from a position is a function of the
// position, the input and the values of Vars, and changes no variable but those of Bind, since a
// binding made in a called rule ends when the rule returns. A walk that takes a record sets the
// variables of Bind to the values that the walk of the record ended with. Keying by the bound
// variables as well makes a binding that leaves a value as it was the same as no binding, which a
// walk cannot tell apart. Both lists are sorted.
type RunKey struct {
	Vars, Bind []string
}
