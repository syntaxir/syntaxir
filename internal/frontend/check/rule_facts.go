package check

import (
	"syntaxir.dev/sxir/internal/diagnostic"
)

// RuleFlags are rule properties determined by static analysis. They are saved in compiled
// grammars so loading can skip the analysis.
type RuleFlags struct {
	Memo, Leader, Member, Positional bool
	// Cycle is Rule.Cycle.
	Cycle int
	// Twin reports that the rule has a value-free twin (Rule.Twin).
	Twin bool
}

// RuleFacts are the facts of the rules of a canonical grammar that CheckCanonical reads. They are
// the facts of the untyped Core IR module of the rules (internal/middleend/corepass), which the
// caller computes (internal/frontend): the compiler takes every rule fact from them.
type RuleFacts struct {
	// Rules holds the facts of each rule of the grammar, the rules of UniqueRules in order.
	Rules []RuleFact
	// BindingErrors are the bindings of variables that a left-recursion cycle reads and that may
	// run before a left call into the cycle, with or without token insertion, in the order of the
	// rules. CheckCanonical reports them unless Options.LeftRecursionBindings.
	BindingErrors diagnostic.ErrorList
}

// RuleFact holds the facts of one rule.
type RuleFact struct {
	// Cycle numbers the rule's left-recursion cycle from 1, or is 0 for a rule on none; Leader
	// reports a leader of a cycle and Member a rule on a cycle that is not one.
	Cycle          int
	Leader, Member bool
	// CycleIns, LeaderIns and MemberIns are Cycle, Leader and Member with token insertion enabled.
	CycleIns             int
	LeaderIns, MemberIns bool
	// Positional reports that the rule or a rule it calls reads positions.
	Positional bool
	// Transient reports that ordinary parses do not memoize the rule.
	Transient bool
	// Lexical reports that the rule only reads characters (corepass.Rule.Lexical).
	Lexical bool
	// Rec reports that the rule is on a cycle of calls.
	Rec bool
	// Vars are the variables the rule or a rule it calls reads, sorted.
	Vars []string
	// Recovering names a rule whose #recover can put an error value in the rule's value: the rule
	// itself or one it calls; it is "" for none.
	Recovering string
	// LeftOperands reports, for each operand of an Operators Rule, that the operand may call the
	// rule before it consumes input, with or without token insertion.
	LeftOperands []bool
}
