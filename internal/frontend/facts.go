package frontend

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// A FactSource computes the rule facts of the structural module of a grammar for the front end,
// which reads them and computes none itself: the facts are the Core analyses of the middle end,
// held by the analysis manager of the module, in the checker's types (checkfacts.Source).
type FactSource interface {
	// RuleFacts returns the rule facts that the checker reads of the module m of the rules defs,
	// with the binding errors, from the facts that am holds of m.
	RuleFacts(am *pass.AnalysisManager[*core.Module], m *core.Module, defs []*ast.RuleDef) *check.RuleFacts
	// Errors returns the structural errors of the module m of the rules defs, in the order of the
	// facts; with bindings, a binding inside left recursion is an error too.
	Errors(am *pass.AnalysisManager[*core.Module], m *core.Module, defs []*ast.RuleDef, bindings bool) diagnostic.ErrorList
	// Sites numbers the sites of the module m of prog's rules with the rule flags of prog.
	Sites(m *core.Module, prog *check.Model, control *cancel.Control) check.Sites
}

// Check checks g into its semantic model (check.CheckCanonical) with the rule facts and the site
// numbers of the untyped Core IR module of its rules, which facts computes: the checker takes
// every rule fact from the Core facts. names receives the hidden rules that the lowering of inline
// actions generates. If flags is not nil, the rules take the flags of a compiled grammar
// (check.Model.Flags), and the sites are numbered with them. A non-nil control polls for
// cancellation. The Result has no Core IR module (Result.WithModule).
func Check(g *ast.Grammar, opts check.Options, flags []check.RuleFlags, names desugar.InlineNames, facts FactSource, control *cancel.Control) (*Result, error) {
	r, _, err := CheckStructural(g, opts, flags, names, facts, control)
	return r, err
}

// Structural is the structural Core IR module of the canonical rules of a grammar, built without
// the checker, from which Check takes the rule facts, with the manager of its analyses, which
// holds those facts. A tool reads the facts through the manager (corepass.FactsOf). Once the types
// of the program check, the module carries their checked types (recordTypes); a module of a
// program whose types do not check carries none.
type Structural struct {
	// Canonical is the grammar as the compiler canonicalized it; Rules are its unique rules, in
	// the order of the rules of Module.
	Canonical *Canonical
	Rules     []*ast.RuleDef
	// Module is nil if the grammar has an error that keeps it from being built. Its origin map
	// (core.Module.Origin) gives the expression each node was built from.
	Module   *core.Module
	Analyses *pass.AnalysisManager[*core.Module]
	// failure is why Module is nil: an error or the value of a panic.
	failure any
	// redeclared tells that the grammar declares a rule twice, which the compiler reports: Rules
	// and Module hold the first declaration of each rule.
	redeclared bool
}

// CheckStructural is Check that also returns the structural module the facts were taken from, so
// that a caller reads them without building the module and computing them again. The module is
// returned with an error too, unless canonicalization failed.
func CheckStructural(g *ast.Grammar, opts check.Options, flags []check.RuleFlags, names desugar.InlineNames, facts FactSource, control *cancel.Control) (*Result, *Structural, error) {
	s, err := buildStructural(g, opts, names, control)
	if err != nil {
		return nil, nil, err
	}
	var ruleFacts *check.RuleFacts
	if s.Module != nil {
		ruleFacts = facts.RuleFacts(s.Analyses, s.Module, s.Rules)
	} else {
		// The module cannot be built from a grammar with an error that the compiler reports, such
		// as a malformed attribute. The rules then take no facts, so that the compiler still
		// reports every error it finds.
		ruleFacts = &check.RuleFacts{Rules: make([]check.RuleFact, len(s.Rules))}
	}
	c := s.Canonical
	prog, err := check.CheckCanonical(c.Grammar, c.Source, c.Starts, c.DefaultStart, opts, flags, names, ruleFacts, control)
	if err == nil && len(c.StartErrors) > 0 {
		// The start rules and the other declaration attributes are reported once the grammar is
		// otherwise valid.
		err = c.StartErrors
	}
	if err != nil {
		return nil, s, mergeErrors(err, s.errors(opts, names, facts))
	}
	if s.failure != nil {
		// A grammar that the compiler accepts has a module: the failure is a bug, which keeps
		// its own error or panic.
		if f, ok := s.failure.(error); ok {
			return nil, s, f
		}
		panic(s.failure)
	}
	// The fact errors are reported for a grammar the compiler accepts too: the compiler no longer
	// checks what the facts check (for example a splice rule that contains itself).
	if fe := s.errors(opts, names, facts); len(fe) > 0 {
		return nil, s, fe
	}
	if rec := prog.Checker().ValueRecord(); rec != nil {
		recordTypes(s.Module, rec, core.SetType)
	}
	r := NewResult(prog, facts.Sites(s.Module, prog, control))
	r.Input = g
	return r, s, nil
}

// recordTypes records on the structural module m of a program whose types check the checked types
// that rec gives its parts: the value type of every rule and the type of every action expression
// but a lambda, which has none (spec/core-ir/modules.md, Structural modules). set records the type
// of an action expression: core.SetType, but in the tests of the record.
func recordTypes(m *core.Module, rec *check.ValueRecord, set func(core.ActionExpr, core.Type)) {
	for _, r := range m.Rules {
		if t, ok := rec.Rules[r.Name]; ok {
			r.Type = t
		}
		core.EachActionExpr(r, func(t core.ActionExpr) {
			core.WalkActionExpr(t, func(x core.ActionExpr) {
				if typ, ok := rec.ActionExprs[m.Origin.OfActionExpr(x)]; ok {
					set(x, typ)
				}
			})
		})
	}
}

// CheckDefault is CheckStructural with the default options and no rule flags, for an analysis of
// the structure of g, such as the complexity classifier, which reads the structural module of g
// also when the compiler rejects g. The module carries the checked types of g only if the compiler
// accepts g. The module is nil only with an error, and it is nil when g declares a rule twice: the
// module holds the first declaration only, so it is not g.
func CheckDefault(g *ast.Grammar, names desugar.InlineNames, facts FactSource, control *cancel.Control) (*Structural, error) {
	_, st, err := CheckStructural(g, check.Options{}, nil, names, facts, control)
	if err != nil && st != nil && (st.Module == nil || st.redeclared) {
		st = nil
	}
	return st, err
}

// structuralOf builds the structural module of the rules of prog, a program that the compiler
// accepted, from its canonical grammar, which it does not canonicalize again.
func structuralOf(prog *check.Model, control *cancel.Control) (*Structural, error) {
	canon := &Canonical{Grammar: prog.Grammar, Source: prog.Source, Starts: prog.StartRules,
		DefaultStart: prog.DefaultStartRule}
	defs, _ := desugar.UniqueRules(canon.Grammar, control)
	m, err := elaborate.Structural(elaborate.StructuralInput(canon.Grammar, defs, canon.Starts), true)
	if err != nil {
		return nil, err
	}
	if rec := prog.Checker().ValueRecord(); rec != nil {
		recordTypes(m, rec, core.SetType)
	}
	return &Structural{Canonical: canon, Rules: defs, Module: m, Analyses: newAnalyses(control)}, nil
}

// buildStructural canonicalizes g and builds the untyped Core IR module of its unique rules. A
// grammar the compiler rejects may fail to build: the failure, an error or the value of a panic,
// is kept for CheckStructural to report only if the compiler accepts the grammar. A cancellation
// is not a failure and unwinds.
func buildStructural(g *ast.Grammar, opts check.Options, names desugar.InlineNames, control *cancel.Control) (*Structural, error) {
	canon, err := Canonicalize(g, opts, names, control)
	if err != nil {
		return nil, err
	}
	defs, dups := desugar.UniqueRules(canon.Grammar, control)
	s := &Structural{Canonical: canon, Rules: defs, Analyses: newAnalyses(control), redeclared: len(dups) > 0}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(cancel.Cancellation); ok {
					panic(r)
				}
				s.Module, s.failure = nil, r
			}
		}()
		m, err := elaborate.Structural(elaborate.StructuralInput(canon.Grammar, defs, canon.Starts), true)
		if err != nil {
			s.failure = err
			return
		}
		s.Module = m
	}()
	return s, nil
}

// newAnalyses returns a manager of the analyses of a module for work under control, which may be
// nil.
func newAnalyses(control *cancel.Control) *pass.AnalysisManager[*core.Module] {
	if control == nil {
		return pass.NewAnalysisManager[*core.Module](nil)
	}
	return pass.NewAnalysisManager[*core.Module](control.Ctx)
}
