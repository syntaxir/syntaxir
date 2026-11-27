package check

import (
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Rule is a rule of a compiled program: what the compiler found out about it. Every backend and
// generator reads it; the engine's parser runs a rule
// (internal/runtime/state/runtime.go) made from it.
type Rule struct {
	// Where names the rule in the message of a failed action, if not name (Rule.WhereName).
	Where string
	// Splice reports that the rule makes no node of its own: a hidden rule without an action, whose
	// elements are those of the sequence that calls it.
	Splice bool
	ID     int
	Name   string
	Def    *ast.RuleDef
	Scope  *Scope
	Action ast.ActionExpr
	// NodeType, if not "", is the named node type the rule's value is converted to
	// (Checker.RuleNodeType).
	NodeType string
	// OpNodeType, if not "", is the named node type every operator node of an Operators Rule is
	// converted to (Checker.OperatorsNodeType).
	OpNodeType string
	Memo       bool
	Missing    bool // this rule or a callee contains #missing
	Leader     bool
	// Member reports that the rule belongs to a left-recursion cycle and is not its leader: its
	// results may read the growing seed of a leader at their own position.
	Member bool
	// Cycle numbers the left-recursion cycle of a leader or member from 1, the same for the rules of
	// one cycle, and is 0 for any other rule: a result reads only the seeds of the growths of its own
	// cycle.
	Cycle int
	// LeaderIndex numbers the leaders of each cycle from 0 (NumberLeaders): a leader's member in the
	// sets of growths that a memoized result was evaluated with; the first program.MaxLeaderBits
	// leaders of a cycle have the bit 1<<LeaderIndex in present sets.
	LeaderIndex int
	// Stream reports that the top level of the rule body has a #stream repetition.
	Stream bool
	// Positional reports that the result may depend on position values (analysis.positional).
	Positional bool
	// Twin is the value-free twin used for calls from places where the value is discarded (#atomic, -, !,
	// value-free bodies). It is created for rules that return a CST (no action) and are not part of a left-recursion cycle (twinOK), when such a call exists. It matches the
	// same input in the same way, but keeps a separate memo (ids from len(RuleList) on).
	Twin   *Rule
	TwinOK bool
	// Vars are the variables that the rule or the rules it calls read, sorted. The rule's result is
	// memoized per combination of their values at the call (memoKey.env).
	Vars []string
	// Transient reports that the rule is not memoized in ordinary parses (it is memoized only by
	// the incremental Document) (transientRules).
	Transient bool
	// Lexical reports that the rule only reads characters (corepass.Rule.Lexical). A Document does
	// not memoize the value-free twin of a transient lexical rule (program.Rule.ValuelessLexical).
	Lexical bool
	// Seen is the rule's index among the rules whose memoization is deferred to the second call at a
	// position (rule.seen, the firstCall composition), or -1.
	Seen int
	// Plain reports that the rule is never memoized except by Document and has no captures, so
	// call runs it with invokePlain (the inlined rules of the closure backend call InvokePlain directly).
	Plain bool
	// Rec reports that the rule is on a cycle of the call graph, or calls itself: only such rules
	// can nest without bound, so only their entries count against the nesting limit.
	Rec bool
	// Novalue reports that this is a value-free twin (its value is always nil).
	Novalue bool
	// PredCaps tells, for value-free rules in a program that builds no tree, which captures
	// record their values: those that predicates can read (other captures build no values). It
	// is nil for ordinary rules.
	PredCaps *PredicateCaptures
	// Lean reports that the body's value is unused (an action, which reads captures only), so the
	// body runs value-free. Captures still build values.
	Lean bool
}

// BodyExpr returns the expression the rule's body matches.
func (r *Rule) BodyExpr() ast.Expr {
	return r.Def.Expr
}

// EntryDepth returns the nesting levels that a call of the rule adds: one for a recursive rule, none
// for any other.
func (r *Rule) EntryDepth() int {
	if r.Rec {
		return 1
	}
	return 0
}

// WhereName returns the text that names the rule in the message of a failed action.
func (r *Rule) WhereName() string {
	if r.Where != "" {
		return r.Where
	}
	return r.Name
}

// PredicateCaptures tells which captures of a rule body record their values in a program that
// builds no tree.
type PredicateCaptures struct {
	// Read holds the captures that a predicate can read (predicateCaptures).
	Read map[*ast.Capture]bool
	// Names holds the capture names that predicates read. A capture of such a name that no
	// predicate can read records nothing but keeps its slot, so that a predicate which reads the
	// name where no capture of it can hold a value finds the slot unset, as in a parse.
	Names map[string]bool
}

// Records reports whether the capture records its value.
func (p *PredicateCaptures) Records(c *ast.Capture) bool { return p.Read[c] }

// Slotted reports whether a capture that records nothing still has the slot of its name.
func (p *PredicateCaptures) Slotted(c *ast.Capture) bool { return p.Names[c.Name] }
