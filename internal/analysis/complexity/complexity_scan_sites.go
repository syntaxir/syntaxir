package complexity

import (
	"fmt"

	"syntaxir.dev/sxir/internal/ir/core"
	"syntaxir.dev/sxir/internal/middleend/corepass"
)

// scanOnly reports whether a call of the rule name pays at most a constant when it is evaluated
// again at a position where it was evaluated before: the rule reads no variable and calls no rule,
// so each of its evaluations at a position is the same; every repetition without an upper bound in
// its body as written is a scan site that the compiler numbers (compilerSites); its body has no
// #recover, Operators Rule or #stream; and every scanning source that the walk found in it is such
// a repetition (no copying operation). The records of scan sites then answer each walk that starts
// again at a position after it matched ScanHotSpan characters, with a constant number of steps
// ("Scan Sites" in spec/runtime/parsing.md), so the evaluations that repeat the call at one
// position pay a constant each, besides at most three full walks of each walk there in each mode.
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
	if corepass.NoScanSites || corepass.NoRunSites {
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
			// Every repetition without an upper bound is a scan site that the compiler numbers.
			ok = ok && (x.Max >= 0 || scans[x] != 0)
		}
	})
	if !ok {
		return false
	}
	for _, s := range k.walked[name] {
		if s.scan == nil || k.srcNodes[s.scan] && !corepass.IsScan(s.scan) {
			return false
		}
	}
	return true
}

// writtenBody returns the node of the body of the rule name as the grammar writes it: in the
// module of the grammar, not in its view. The rules of the grammar are numbered as those of its
// module.
func (k *classifier) writtenBody(name string) (core.Node, bool) {
	rules := k.base
	if rules == nil {
		rules = k.rules
	}
	for i, rd := range rules {
		if rd.Name == name {
			r := k.facts.source.Module.Rules[i]
			if r.Name != name {
				panic(fmt.Sprintf("complexity: rule %d of the module is %s, not %s", i, r.Name, name))
			}
			return r.Body, true
		}
	}
	return nil, false
}
