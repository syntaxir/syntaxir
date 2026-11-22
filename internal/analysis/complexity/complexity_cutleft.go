package complexity

import "syntaxir.dev/sxir/internal/ir/core"

// checkCutLeftCalls rejects, in the rules of a cycle with more than one leader, a #cut that is
// passed after a left call in the alternative, iteration or option that it commits, and a
// lookahead or the skip of an Operators Rule that can make a left call. The growth of one leader reads the other, and such a cut
// or lookahead makes a later step of a nested growth reach an alternative that its first step did
// not, so the growths cannot be told apart by what they include.
func (k *classifier) checkCutLeftCalls() {
	leaders := map[int]int{}
	for name := range k.an.Leaders {
		if k.an.Cyclic[name] {
			leaders[k.an.Cycle[name]]++
		}
	}
	for _, r := range k.rules {
		if !k.reach[r.Name] || !k.an.Cyclic[r.Name] || leaders[k.an.Cycle[r.Name]] < 2 {
			continue
		}
		k.cutLeftScan(r.Name, k.ruleBody(r.Name), cutLeftState{start: true})
	}
}

// cutLeftState is what cutLeftScan knows before a node of the rule body.
type cutLeftState struct {
	// start says that the node can begin where the rule body begins: some path from the start of
	// the body to it consumes no input, so a call of a rule of the cycle there is a left call.
	start bool
	// left says that a left call can have been evaluated earlier in the alternative, iteration or
	// option that a #cut at the node commits.
	left bool
}

// cutLeftScan walks n of the rule from state s. It returns whether n can complete at the start of
// the rule body, and whether n can evaluate a left call. A #cut commits the innermost enclosing
// choice, option or repetition of the rule, through captures, lookaheads, labels and recoveries,
// so only those three begin a new region for the left calls that a cut is passed after.
func (k *classifier) cutLeftScan(rule string, n core.Node, s cutLeftState) (start, found bool) {
	k.control.Poll()
	switch n := n.(type) {
	case nil:
		return s.start, false
	case *core.Call:
		if callee := k.rules[n.Rule].Name; plainCall(n) && k.an.Cyclic[callee] && k.sameCycle(rule, callee) {
			return s.start && k.nullableOf(n), s.start
		}
		return s.start && k.nullableOf(n), false
	case *core.Capture:
		return k.cutLeftScan(rule, n.Body, s)
	case *core.Drop:
		return k.cutLeftScan(rule, n.Body, s)
	case *core.Text:
		return k.cutLeftScan(rule, n.Body, s)
	case *core.Seq:
		for _, it := range n.Items {
			st, f := k.cutLeftScan(rule, it, s)
			s.start = st
			s.left = s.left || f
			found = found || f
		}
		return s.start, found
	case *core.Choice:
		for _, alt := range n.Alts {
			st, f := k.cutLeftScan(rule, alt.Body, cutLeftState{start: s.start})
			start = start || st
			found = found || f
		}
		return start, found
	case *core.Opt:
		_, f := k.cutLeftScan(rule, n.Body, cutLeftState{start: s.start})
		return s.start, f
	case *core.Repeat:
		st, f := k.cutLeftScan(rule, n.Body, cutLeftState{start: s.start})
		return s.start && (n.Min == 0 || st), f
	case *core.Recover:
		return k.cutLeftRecovery(rule, n.Body, n.Skip, s)
	case *core.Label:
		return k.cutLeftAttribute(rule, n, n.Body, s)
	case *core.Missing:
		return k.cutLeftAttribute(rule, n, n.Body, s)
	case *core.Stream:
		return k.cutLeftAttribute(rule, n, n.Repeat, s)
	case *core.OperatorsRule:
		return k.cutLeftOperators(rule, n, s)
	case *core.Look:
		return k.cutLeftLookahead(rule, n, s)
	case *core.Cut:
		if s.left {
			pos := exprPos(k.exprOf(n))
			k.reason(rule, pos, "the #cut at %s is passed after a left call in a cycle with more than one leader, which the classifier does not analyze", at(rule, pos))
		}
		return s.start, false
	}
	return s.start && k.nullableOf(n), false
}

// cutLeftAttribute walks the attribute node n other than #recover, whose operand is body: it can
// complete at the start of the rule body where its operand can, or where it matches without
// consuming input, as #missing does by inserting its literal.
func (k *classifier) cutLeftAttribute(rule string, n, body core.Node, s cutLeftState) (start, found bool) {
	st, f := k.cutLeftScan(rule, body, s)
	return st || (s.start && k.nullableOf(n)), f
}

// cutLeftRecovery walks a recovery: its skip runs after its expression fails, at the start of the
// expression or later, so a left call in the expression is earlier than a cut in the skip.
func (k *classifier) cutLeftRecovery(rule string, body, skip core.Node, s cutLeftState) (start, found bool) {
	st, f := k.cutLeftScan(rule, body, s)
	ss, fs := k.cutLeftScan(rule, skip, cutLeftState{start: s.start, left: s.left || f})
	return st || ss, f || fs
}

// cutLeftLookahead walks a lookahead. The lookahead consumes no input, so what follows it begins
// where it does; a lookahead whose expression can make a left call is reported, and so is each
// such lookahead nested in it.
func (k *classifier) cutLeftLookahead(rule string, n *core.Look, s cutLeftState) (start, found bool) {
	if _, found = k.cutLeftScan(rule, n.Body, s); found {
		e := k.exprOf(n)
		k.reason(rule, exprPos(e), "the lookahead %s at %s can make a left call in a cycle with more than one leader, which the classifier does not analyze", k.show(e), at(rule, exprPos(e)))
	}
	return s.start, found
}

// cutLeftOperators walks an Operators Rule. Its skip runs before the first operand, where the
// rule body begins, and its results are kept apart from those computed outside it as the results
// of a lookahead are, so a skip that can make a left call is reported as such a lookahead is.
func (k *classifier) cutLeftOperators(rule string, n *core.OperatorsRule, s cutLeftState) (start, found bool) {
	if n.Skip != nil {
		if _, found = k.cutLeftScan(rule, n.Skip.Body, cutLeftState{start: s.start}); found {
			skip := k.exprOf(n.Skip.Body)
			k.reason(rule, exprPos(skip), "the skip %s of the Operators Rule at %s can make a left call in a cycle with more than one leader, which the classifier does not analyze", k.show(skip), at(rule, exprPos(k.exprOf(n))))
		}
	}
	return s.start && k.nullableOf(n), found
}
