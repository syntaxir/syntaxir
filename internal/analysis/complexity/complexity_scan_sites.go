package complexity

import (
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
)

// scanOnly reports whether a call of the rule name pays at most a constant when it is evaluated
// again at a position where it was evaluated before: the rule reads no variable and calls no rule,
// so each of its evaluations at a position is the same; every repetition without an upper bound in
// its body as written is a site that the compiler numbers (compilerSites) and whose records answer
// a walk again from its start: a scan site, or a run site free of the environment with minimum 0
// whose element has no repetition without an upper bound (recordedRun); its body has no #recover,
// Operators Rule or #stream; and every scanning source that the walk found in it is such a
// repetition (no copying operation). The records then answer each walk that starts again at a
// position with a constant number of steps once the walks from there are recorded ("Scan Sites"
// and "Run Sites" in spec/runtime/parsing.md; Lemmas 6 to 8 of spec/proofs/run-sites.md), so the
// evaluations that repeat the call at one position pay a constant each, besides a constant number
// of evaluations there that cost at most a constant factor more than the first.
//
// The test reads the rules as written, not their view: the view merges choices of single
// characters into classes and unrolls repetitions, which the runtime does not, so a repetition of a
// class in the view need not be a scan site.
//
// It is false for every rule until the sources are settled (computeScanOnly).
func (k *classifier) scanOnly(name string) bool { return k.scanOnlyRules[name] }

// computeScanOnly finds the rules for scanOnly, once the scanning sources of the walk are settled.
func (k *classifier) computeScanOnly() {
	out := map[string]bool{}
	for _, r := range k.rules {
		if k.reach[r.Name] && k.scanOnlyRule(r.Name) {
			out[r.Name] = true
		}
	}
	k.scanOnlyRules = out
}

func (k *classifier) scanOnlyRule(name string) bool {
	if k.siteSwitches.NoScanSites || k.siteSwitches.NoRunSites {
		return false
	}
	body, ok := k.writtenBody(name)
	if !ok || len(k.vars[name]) > 0 {
		return false
	}
	scans := k.compilerSites().Scan
	walkNodes(body, func(x core.Node) {
		k.control.Poll()
		switch x := x.(type) {
		case *core.Call, *core.Nest, *core.Recover, *core.OperatorsRule, *core.Stream:
			ok = false
		case *core.Repeat:
			// Every repetition without an upper bound is a scan site or a recorded run site that
			// the compiler numbers.
			ok = ok && (x.Max >= 0 || scans[x] != 0 || k.recordedRun(x))
		}
	})
	if !ok {
		return false
	}
	for _, s := range k.walked[name] {
		if s.scan == nil || k.srcNodes[s.scan] && !corepass.IsScan(s.scan) && !k.recordedRun(s.scan) {
			return false
		}
	}
	return true
}

// recordedRun reports whether the repetition rep is a run site that the compiler numbers, free of
// the environment, with minimum 0, whose element has no repetition without an upper bound. As for
// runSiteOf, a node of the view is one only if the view shares it with the module of the grammar.
// Its records answer a walk again from one start after a constant number of walks from there
// (Lemma 7 of spec/proofs/run-sites.md): such a walk takes the record of its start before its first
// iteration, so it evaluates no element and copies no recorded elements behind its own. With a
// minimum above 0, a walk that builds values would take the record after its first iterations and
// copy the rest of the recorded list behind them, a cost of the length of the walk each time.
func (k *classifier) recordedRun(rep *core.Repeat) bool {
	sites := k.compilerSites()
	site := sites.Run[rep]
	if site == 0 || rep.Min != 0 || !sites.EnvFree(site) {
		return false
	}
	bounded := true
	walkNodes(rep.Body, func(x core.Node) {
		if r, ok := x.(*core.Repeat); ok && r.Max < 0 {
			bounded = false
		}
	})
	return bounded
}

// writtenBody returns the node of the body of the rule name as the grammar writes it: in the
// module of the grammar, not in its view. The rules of the grammar are numbered as those of its
// module.
func (k *classifier) writtenBody(name string) (core.Node, bool) {
	if _, ok := k.byName[name]; !ok {
		return nil, false
	}
	return k.facts.source.Module.Rules[k.ruleIDs[name]].Body, true
}
