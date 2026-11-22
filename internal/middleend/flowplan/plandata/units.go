package plandata

import "syntaxir.dev/sxir/pkg/grammar/ast"

// Rule is what a plan reads of a rule of its table: the facts of the run-time program, the rule's
// definition and the action, as the checked grammar holds them. A backend fills it from its view of
// the grammar.
type Rule struct {
	ID   int
	Name string
	// Body is the expression of the rule's definition, and Action the rule's action, nil if it has
	// none.
	Body   ast.Expr
	Action ast.ActionExpr
	// Plain, Novalue and Leader are those of the run-time program (program.Rule); Lean reports that
	// the body's value is unused, so the body runs value-free.
	Plain, Lean, Novalue, Leader bool
}

// Unit is one table that a backend lowers, as the planning of the program plans it: its label, which
// labels its dumps, the rule table whose rules it runs (CoreTable.Name), its Core IR with its lowering
// options, and what its plan is made from.
type Unit struct {
	Label string
	Table string
	IR    *TableIR
	// Rules holds the rules of the table by ID, value-free twins included.
	Rules []*Rule
	// Prune leaves out of the plan the rules that the plan of the program does not reach, by their
	// slot or their wrapper.
	Prune bool
	// NoOperators leaves out the lines of the Operators Rules: a table whose rule bodies only fix the
	// frame layouts that another table runs under.
	NoOperators bool
	// Typed plans the typed table (flowplan.PlanTyped), whose framed bodies have the frame layouts of
	// the unit labeled Frames, which comes before it.
	Typed  bool
	Frames string
	// Streams are the rules whose stream bodies the table has, in order: the copy of each that emits
	// the elements of its #stream repetition (flowplan.Stream). A Go parser has the one of its start
	// rule; the VM, whose one module serves every start rule, one for each rule with a #stream
	// repetition.
	Streams []*Rule
	// Refuse is a test's decision on a flat function by its key, nil for none (flowplan.Unit).
	Refuse func(Key) bool
	// Numbered says that the expectation table is numbered from the table with every rule that it
	// could run (Program.Numbered).
	Numbered bool
}

// PlannedUnit is a planned table of the program with its label. Printed is false for the
// unreachable part of a numbered table, which is lowered only for the numbering of the expectation
// table.
type PlannedUnit struct {
	Label string
	Planned
	Printed bool
}

// Numbered is a table that the expectation table is numbered from: Full is its plan with every rule
// that it could run, and Parts are the places in Program.Units of the parts that hold the functions of
// Full, disjoint: the table as the backend runs it and, if the plan prunes it, its unreachable part.
type Numbered struct {
	Full  *Plan
	Parts []int
}

// Excluding returns the plan of the functions of p that q does not hold, with the layouts of p. p and q
// are unchanged.
func (p *Plan) Excluding(q *Plan) *Plan {
	out := &Plan{Layouts: p.Layouts, Typed: p.Typed}
	for _, s := range p.Funcs {
		if _, ok := q.Func(s.Key); !ok {
			out.Add(s)
		}
	}
	return out
}
