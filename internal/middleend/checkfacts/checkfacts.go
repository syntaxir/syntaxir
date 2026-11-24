// Package checkfacts gives the checker of the front end the rule facts and the site numbers that it
// reads, in its own types: the facts are the Core analyses of corepass, which the analysis manager
// of the structural module holds, and the front end, which does not import the middle end, asks for
// them through frontend.FactSource. The package is the one place that maps the Core facts to the
// front end: to the checker's rule facts and site numbers (check.RuleFacts, check.Sites), to the
// rules and repetitions of the grammar through the origin map of the module, and to positions in
// the grammar for their errors. The Core passes read and produce Core IR only; they reach no
// package of the front end.
package checkfacts

import (
	"slices"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/diagnostic"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/frontend/desugar"
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Source hands the rule facts of the structural module of a grammar to the front end
// (frontend.FactSource): the front end builds the module and checks the grammar, and the facts
// that the check reads are the Core analyses of corepass, which the front end does not compute.
type Source struct{}

// RuleFacts returns the facts that the checker reads of the module m of the rules defs, with the
// binding errors, from the facts that am holds of m.
func (Source) RuleFacts(am *pass.AnalysisManager[*core.Module], m *core.Module, defs []*ast.RuleDef) *check.RuleFacts {
	f := corepass.FactsOf(am, m)
	out := &check.RuleFacts{Rules: make([]check.RuleFact, len(defs))}
	for i := range defs {
		r := &f.Rules[i]
		rf := check.RuleFact{Cycle: r.Cycle, Leader: r.Leader, Member: r.Member,
			CycleIns: r.CycleIns, LeaderIns: r.LeaderIns, MemberIns: r.MemberIns,
			Positional: r.Positional, Transient: r.Transient, Rec: r.Rec, Vars: r.Vars}
		if r.Recovering >= 0 {
			rf.Recovering = m.Rules[r.Recovering].Name
		}
		if o, ok := m.Rules[i].Body.(*core.OperatorsRule); ok {
			rf.LeftOperands = make([]bool, len(o.Operands))
			for k, l := range o.Operands {
				rf.LeftOperands[k] = slices.Contains(f.LeftCalls(l.Body, false), i) || slices.Contains(f.LeftCalls(l.Body, true), i)
			}
		}
		out.Rules[i] = rf
	}
	for _, e := range f.BindingErrors(m) {
		pos := m.Origin.Of(e.Node).(*ast.Predicate).Action.(*ast.Bind).Diagnostic()
		out.BindingErrors = append(out.BindingErrors, &diagnostic.Error{Pos: pos, Msg: e.Msg})
	}
	return out
}

// Errors returns the structural errors of the module m of the rules defs (corepass.Facts.Errors), from the
// facts that am holds of m, at the positions of the expressions that they are about, in the order
// of corepass.Facts.Errors. With bindings, a binding inside left recursion is an error too.
func (Source) Errors(am *pass.AnalysisManager[*core.Module], m *core.Module, defs []*ast.RuleDef, bindings bool) diagnostic.ErrorList {
	var out diagnostic.ErrorList
	for _, e := range corepass.FactsOf(am, m).Errors(m, bindings) {
		var pos ast.Pos
		switch x := m.Origin.Of(e.Node).(type) {
		case *ast.Predicate:
			pos = x.Action.(*ast.Bind).Diagnostic()
		case *ast.Attributed:
			pos = x.Attrs[0].Diagnostic()
		default:
			pos = defs[e.Rule.ID].Diagnostic()
		}
		out = append(out, &diagnostic.Error{Pos: pos, Msg: e.Msg})
	}
	return out
}

// Sites numbers the sites of the module m of prog's rules with the rule flags of prog, keyed by
// the repetitions of the grammar that the module's repetitions were built from.
func (Source) Sites(m *core.Module, prog *check.Model, control *cancel.Control) check.Sites {
	rules := make([]corepass.SiteRule, len(prog.RuleList))
	for i, r := range prog.RuleList {
		rules[i] = corepass.SiteRule{LeftRec: r.Leader || r.Member, Vars: r.Vars, Declared: desugar.DeclaredBeyondScan(r.Def)}
	}
	s := corepass.ComputeSites(m, rules, corepass.SiteSwitches{NoRunSites: prog.Opts.NoRunSites, NoScanSites: prog.Opts.NoScanSites}, control)
	key := func(in map[*core.Repeat]int) map[*ast.Repeat]int {
		out := make(map[*ast.Repeat]int, len(in))
		for n, site := range in {
			out[m.Origin.Of(n).(*ast.Repeat)] = site
		}
		return out
	}
	runKeys := s.RunKeys
	if len(runKeys) == 0 {
		runKeys = nil
	}
	return check.Sites{Skip: key(s.Skip), Run: key(s.Run), Scan: key(s.Scan),
		NumSkip: s.NumSkip, NumRun: s.NumRun, NumScan: s.NumScan, EnvFree: s.EnvFreeSites(), RunKeys: runKeys}
}
