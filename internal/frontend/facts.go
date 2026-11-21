package frontend

import (
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/frontend/elaborate"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// A FactSource computes the rule facts of the structural module of a grammar for the front end,
// which reads them and computes none itself: the facts are the Core analyses of the middle end
// (checkfacts.Source), held by the analysis manager of the module.
type FactSource interface {
	// RuleFacts returns the rule facts that the compiler reads of the module m of the rules defs,
	// with the binding errors, from the facts that am holds of m.
	RuleFacts(am *pass.AnalysisManager[*core.Module], m *core.Module, defs []*ast.RuleDef) *compile.RuleFacts
	// Errors returns the structural errors of the module m of the rules defs, in the order of the
	// facts; with bindings, a binding inside left recursion is an error too.
	Errors(am *pass.AnalysisManager[*core.Module], m *core.Module, defs []*ast.RuleDef, bindings bool) diagnostic.ErrorList
	// Sites numbers the sites of the module m of prog's rules with the rule flags of prog.
	Sites(m *core.Module, prog *compile.Program, control *cancel.Control) compile.Sites
}

// Check checks g into its static program (compile.CheckCanonical) with the rule facts and the site
// numbers of the untyped Core IR module of its rules, which facts computes: the compiler takes
// every rule fact from the Core facts. names receives the hidden rules that the lowering of inline
// actions generates. If flags is not nil, the rules take the flags of a compiled grammar
// (compile.Program.Flags), and the sites are numbered with them. A non-nil control polls for
// cancellation.
func Check(g *ast.Grammar, opts compile.Options, flags []compile.RuleFlags, names desugar.InlineNames, facts FactSource, control *cancel.Control) (*compile.Program, error) {
	prog, _, err := CheckStructural(g, opts, flags, names, facts, control)
	return prog, err
}

// Structural is the untyped Core IR module of the canonical rules of a grammar, from which Check
// takes the rule facts, with the manager of its analyses, which holds those facts. A tool reads
// the facts through the manager (corepass.FactsOf).
type Structural struct {
	// Canonical is the grammar as the compiler canonicalized it; Rules are its unique rules, in
	// the order of the rules of Module.
	Canonical *compile.Canonical
	Rules     []*ast.RuleDef
	// Module is nil if the grammar has an error that keeps it from being built. Its origin map
	// (core.Module.Origin) gives the expression each node was built from.
	Module   *core.Module
	Analyses *pass.AnalysisManager[*core.Module]
	// failure is why Module is nil: an error or the value of a panic.
	failure any
}

// CheckStructural is Check that also returns the structural module the facts were taken from, so
// that a caller reads them without building the module and computing them again. The module is
// returned with an error too, unless canonicalization failed.
func CheckStructural(g *ast.Grammar, opts compile.Options, flags []compile.RuleFlags, names desugar.InlineNames, facts FactSource, control *cancel.Control) (*compile.Program, *Structural, error) {
	s, err := buildStructural(g, opts, names, control)
	if err != nil {
		return nil, nil, err
	}
	var ruleFacts *compile.RuleFacts
	if s.Module != nil {
		ruleFacts = facts.RuleFacts(s.Analyses, s.Module, s.Rules)
	} else {
		// The module cannot be built from a grammar with an error that the compiler reports, such
		// as a malformed attribute. The rules then take no facts, so that the compiler still
		// reports every error it finds.
		ruleFacts = &compile.RuleFacts{Rules: make([]compile.RuleFact, len(s.Rules))}
	}
	prog, err := compile.CheckCanonical(s.Canonical, opts, flags, names, ruleFacts, control)
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
	prog.SetSites(facts.Sites(s.Module, prog, control))
	return prog, s, nil
}

// Typed is what the complexity classifier reads of a checked grammar: the grammar as the compiler
// canonicalized it, its default start rule, its unique rules with the untyped Core IR module they
// were elaborated into, with its origin map (core.Module.Origin), and the manager of the analyses of
// that module, which holds its facts, and the value types of its terms.
type Typed struct {
	Grammar      *ast.Grammar
	DefaultStart string
	Rules        []*ast.RuleDef
	Module       *core.Module
	Analyses     *pass.AnalysisManager[*core.Module]
	Types        *check.ValueRecord
}

// CheckTyped checks g with the default options, without building matchers, and returns what the
// classifier reads of it. names receives the hidden rules that the lowering of inline actions
// generates; facts computes the facts that the check reads. It returns an error if g does not
// compile.
func CheckTyped(g *ast.Grammar, names desugar.InlineNames, facts FactSource, control *cancel.Control) (*Typed, error) {
	prog, st, err := CheckStructural(g, compile.Options{}, nil, names, facts, control)
	if err != nil {
		return nil, err
	}
	return &Typed{Grammar: prog.Grammar, DefaultStart: prog.DefaultStartRule, Rules: st.Rules,
		Module: st.Module, Analyses: st.Analyses, Types: prog.ValueRecord()}, nil
}

// buildStructural canonicalizes g and builds the untyped Core IR module of its unique rules. A
// grammar the compiler rejects may fail to build: the failure, an error or the value of a panic,
// is kept for CheckStructural to report only if the compiler accepts the grammar. A cancellation
// is not a failure and unwinds.
func buildStructural(g *ast.Grammar, opts compile.Options, names desugar.InlineNames, control *cancel.Control) (*Structural, error) {
	canon, err := compile.Canonicalize(g, opts, names, control)
	if err != nil {
		return nil, err
	}
	defs, _ := desugar.UniqueRules(canon.Grammar, control)
	s := &Structural{Canonical: canon, Rules: defs, Analyses: newAnalyses(control)}
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
