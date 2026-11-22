package complexity

import "syntaxir.dev/sxir/internal/ir/core"

// A unit restarts the input that a scanning source inside it examined only when it fails after
// the source has run. A unit whose sources are followed by nothing that can fail, such as the
// iteration of ([^"]+ / "\"\"")*, fails only before its sources examine any input; what the parser
// evaluates next then examines no more than the unit examined outside its sources, which the other
// conditions bound.

// containsSource reports whether n contains a scanning source, in itself or in a rule it calls.
func (k *classifier) containsSource(n core.Node) bool {
	if v, ok := k.srcMemo[n]; ok {
		return v
	}
	found := false
	walkNodes(n, func(x core.Node) {
		if found {
			return
		}
		if k.srcNodes[x] {
			found = true
			return
		}
		if rule, ok := k.calledRule(x); ok && k.ruleSrc[rule] != nil {
			found = true
		}
	})
	k.srcMemo[n] = found
	return found
}

// computeFailRules computes, for each reachable rule, whether its body can fail after a scanning
// source in it examined input. The least fixpoint is the answer: a rule fails after a source only
// through an evaluation that ends.
func (k *classifier) computeFailRules() {
	k.failRule = map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, r := range k.rules {
			k.control.Poll()
			if !k.reach[r.Name] || k.failRule[r.Name] {
				continue
			}
			if k.failsAfterSource(k.ruleBody(r.Name)) {
				k.failRule[r.Name] = true
				changed = true
			}
		}
	}
}

// failsAfterSource reports whether n can fail after a scanning source in it examined input.
func (k *classifier) failsAfterSource(n core.Node) bool {
	switch x := n.(type) {
	case nil:
		return false
	case *core.Seq:
		for i, it := range x.Items {
			if k.failsAfterSource(it) {
				return true
			}
			if k.containsSource(it) {
				for _, later := range x.Items[i+1:] {
					if !k.succeedsOf(later) {
						return true
					}
				}
			}
		}
		return false
	case *core.Choice:
		for _, alt := range x.Alts {
			if k.failsAfterSource(alt.Body) {
				return true
			}
		}
		return false
	case *core.Repeat:
		if x.Min == 0 || x.Max == 0 {
			return false
		}
		// A repetition ends at its first empty iteration, so one with a minimum above one can
		// fail after an iteration ran a source even when its expression always succeeds.
		return k.failsAfterSource(x.Body) || x.Min > 1 && k.containsSource(x.Body) && !k.succeedsOf(x)
	case *core.Opt:
		return false
	case *core.Look:
		if x.Neg {
			return k.containsSource(x.Body)
		}
		return k.failsAfterSource(x.Body)
	case *core.Capture:
		return k.failsAfterSource(x.Body)
	case *core.Drop:
		return k.failsAfterSource(x.Body)
	case *core.Text:
		return k.failsAfterSource(x.Body)
	case *core.Label, *core.Recover, *core.Missing, *core.Stream:
		// A #recover can fail after its skip examined input, and a #missing after the input it
		// examined before it inserted its literal: the expression of either fails after a
		// source it contains. Other attributes fail as their operand does.
		operand, rec, missing := attributes(x)
		if rec != nil || missing {
			return k.containsSource(x)
		}
		return k.failsAfterSource(operand)
	case *core.Call, *core.Nest:
		if k.srcNodes[x] {
			return true
		}
		if rule, ok := k.calledRule(x); ok {
			return k.failRule[rule]
		}
	case *core.Pred, *core.BindNode:
		return k.srcNodes[x]
	case *core.Lit, *core.ClassNode, *core.Any, *core.Cut, *core.Succeed, *core.FailNode, *core.Anchor:
		return false
	}
	return k.containsSource(n)
}

// unitFailsAfterSource reports whether the unit can fail after a scanning source inside it
// examined input. Lookaheads are abandoned whether they match or not, and the units of an Operators
// Rule and growing units are not refined.
func (k *classifier) unitFailsAfterSource(u *unit) bool {
	switch u.kind {
	case unitAlt:
		if u.growing {
			return true
		}
		return k.failsAfterSource(u.expr)
	case unitIteration:
		return k.failsAfterSource(u.expr.(*core.Repeat).Body)
	case unitOption:
		return k.failsAfterSource(u.expr.(*core.Opt).Body)
	}
	return true
}
