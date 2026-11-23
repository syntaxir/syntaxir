package common

import (
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Program is a checked grammar as the generators read it: a view over the owners of its facts, not a
// record of its own. internal/driver/checked builds it (checked.Source.Generator), as it builds the
// source of the output schema export (checked.Source.Schema).
//
// Each field names its owner:
//   - Model, the grammar, its types, variables and start rules: the semantic model (check.Model).
//   - Checker, the types of the grammar's expressions and action expressions: the checker (check.Checker).
//   - Typed, the types the checker found for the grammar: the checker's result (check.TypeInfo).
//   - Run, the tables a parser runs (rules, expectations, sites, kinds): the run-time program
//     (program.Program).
//   - RuleList, Twins and ByName: the views of the rules of Run, in the same order (Rule).
//
// The view holds no input of the elaboration to Core IR: the printers read the Core IR of the tables
// they print from the tables, which the pipeline built.
type Program struct {
	*check.Model
	// Check is the checker of the grammar's types, which Checker returns.
	Check *check.Checker
	// Typed holds the types that the checker found, or nil if it did not run.
	Typed *check.TypeInfo
	// Run is the run-time program of the grammar.
	Run *program.Program
	// RuleList holds the views of Run.RuleList and Twins those of Run.Twins, index for index;
	// ByName holds RuleList by name.
	RuleList []*Rule
	Twins    []*Rule
	ByName   map[string]*Rule
}

// Checker returns the checker of the grammar's types.
func (p *Program) Checker() *check.Checker { return p.Check }

// StartRule returns the view of the start rule that name selects (program.Program.StartRule).
func (p *Program) StartRule(name string) (*Rule, error) {
	r, err := p.Run.StartRule(name)
	if err != nil {
		return nil, err
	}
	return p.RuleList[r.ID], nil
}

// Rule is a rule as the generators read it. It embeds the rule of the run-time program (ID, name,
// memoization, left recursion, variables and the other facts a parser runs it with) and points to
// the rule's definition in the grammar; the facts that no other representation holds yet are
// copied from the checked program by the builder of the view.
type Rule struct {
	*program.Rule
	// Def is the rule's definition in the grammar of the semantic model.
	Def *ast.RuleDef
	// Action is the rule's action, nil if it has none: that of Def, or of the hidden rule of an
	// inline action.
	Action ast.ActionExpr
	// Captures is the scope of the rule's captures, in which the checker resolved its action expressions.
	Captures *check.Scope
	// Lean reports that the body's value is unused (an action, which reads captures only), so the
	// body runs value-free; captures still build values. Copied: only the checked program holds it.
	Lean bool
	// Twin is the view of the value-free twin of the rule, if it has one. Copied: only the checked
	// program links a rule to its twin.
	Twin *Rule
}

// BodyExpr returns the expression the rule's body matches.
func (r *Rule) BodyExpr() ast.Expr { return r.Def.Expr }

// PlanRule returns what the plan of a table reads of the rule (plandata.Rule).
func (r *Rule) PlanRule() *plandata.Rule {
	return &plandata.Rule{ID: r.ID, Name: r.Name, Body: r.Def.Expr, Action: r.Action, Plain: r.Plain, Lean: r.Lean, Novalue: r.Novalue, Leader: r.Leader}
}
