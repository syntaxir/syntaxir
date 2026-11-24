package complexity

import (
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
)

// Run sites in the classifier (see Run Sites in spec/runtime/parsing.md).
//
// A repetition without an upper bound whose walks keep records, as those of a skip-run site do, is
// not a scanning source: the records bound the evaluations of its element at each position by a
// constant. The classifier reads the sites that the compiler numbers (corepass.ComputeSites). The
// marking is off in the classification until the proofs of the bounds exist, so the classes do not
// move; the tests and measurements switch it on.
var runSitesOn = false

// runSiteOf reports whether the repetition n is a run site that the compiler numbers. The sites
// are those of the grammar, not of its view: a node of the view is one only if the view shares it
// with the module of the grammar.
func (k *classifier) runSiteOf(n *core.Repeat) bool {
	return runSitesOn && k.compilerSites().Run[n] != 0
}

// compilerSites returns the sites that the compiler numbers on the module of the grammar, with the
// rule flags of the grammar: the rules of left-recursion cycles, the variables that each rule
// reads, and the rules declared beyond a scan.
func (k *classifier) compilerSites() *corepass.Sites {
	if k.numbered == nil {
		m := k.facts.source.Module
		rules := make([]corepass.SiteRule, len(m.Rules))
		for i, r := range m.Rules {
			rules[i] = corepass.SiteRule{LeftRec: k.an.Cyclic[r.Name], Vars: k.vars[r.Name],
				Declared: k.facts.declared[r.Name]}
		}
		k.numbered = corepass.ComputeSites(m, rules, k.siteSwitches, k.control)
	}
	return k.numbered
}
