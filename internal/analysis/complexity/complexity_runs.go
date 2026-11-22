package complexity

import (
	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Run sites in the classifier (see Run Sites in spec/runtime/parsing.md).
//
// A repetition without an upper bound whose walks keep records, as those of a skip-run site do, is
// not a scanning source: the records bound the evaluations of its element at each position by a
// constant. The engine marks the same repetitions (internal/middleend/corepass/sites.go). The marking is off in the
// classification until the proofs of the bounds exist, so the classes do not move; the tests and
// measurements switch it on.
var runSitesOn = false

// runSite reports whether the repetition e is a run site (runSiteOf of its node).
func (k *classifier) runSite(e *ast.Repeat) bool { return k.runSiteOf(k.repeatOf(e)) }

// runSiteOf reports whether the repetition n is a run site: the records of its walks can be keyed
// by position, and its element costs a constant outside the calls it makes.
func (k *classifier) runSiteOf(n *core.Repeat) bool {
	if !runSitesOn || n.Max >= 0 {
		return false
	}
	if v, ok := k.runMemo[n]; ok {
		return v
	}
	if k.runMemo == nil {
		k.runMemo = map[*core.Repeat]bool{}
	}
	if k.base != nil {
		// The runtime marks the run sites of the grammar, not of the view: a repetition of the view
		// is one only if it is a repetition of the grammar and a run site of it.
		if k.baseRuns == nil {
			k.baseRuns = newClassifier(k.g, k.base, k.facts, k.start, k.control)
		}
		e, ok := k.exprOf(n).(*ast.Repeat)
		_, inBase := k.baseRuns.nodes[e]
		k.runMemo[n] = ok && inBase && k.baseRuns.runSite(e)
		return k.runMemo[n]
	}
	if k.looseCutOf(n.Body) {
		return false
	}
	k.badRules()
	if k.runRestart == nil {
		k.runRestart = k.restartableRepeats()
	}
	if !k.runRestart[n] {
		return false
	}
	ok := true
	walkNodes(n.Body, func(x core.Node) {
		if !ok {
			return
		}
		switch x := x.(type) {
		case *core.Repeat:
			ok = x.Max >= 0 || k.runSiteOf(x)
		case *core.OperatorsRule, *core.Nest:
			ok = false
		case *core.Call:
			ok = x.Level == 0 && !k.runBad[k.rules[x.Rule].Name]
		default:
			ok = !blocksRun(x)
		}
	})
	k.runMemo[n] = ok
	return ok
}

// badRules computes the rules that a record cannot stand for: rules that are on a left-recursion
// cycle or contain #stream, #missing, #error or #diagnostic, directly or through calls.
func (k *classifier) badRules() {
	if k.runBad != nil {
		return
	}
	k.runBad = map[string]bool{}
	for _, r := range k.rules {
		if k.an.Cyclic[r.Name] {
			k.runBad[r.Name] = true
		}
		walkNodes(k.ruleBody(r.Name), func(x core.Node) {
			if blocksRun(x) {
				k.runBad[r.Name] = true
			}
		})
	}
	for changed := true; changed; {
		changed = false
		for _, r := range k.rules {
			if k.runBad[r.Name] {
				continue
			}
			for _, c := range k.calls[r.Name] {
				if k.runBad[c] {
					k.runBad[r.Name] = true
					changed = true
					break
				}
			}
		}
	}
}
