package complexity

import "syntaxir.dev/sxir/internal/ir/core"

// leftMode says what may come before a left call of the unit that starts with it: in leftExamine,
// nothing that examines the input, and in leftConsume, nothing that consumes input.
type leftMode int

const (
	leftExamine leftMode = iota
	leftConsume
)

// leadingPassOn makes startsWithLeftCall look through predicates, cuts, anchors and calls of rules
// off the cycle that can match without examining or consuming input. It is on by default; the
// variable is a test hook, whose off setting is the classifier that stopped at them: it checked
// E = {true} E R "!" / E "+" "x" / "x" as an alternative that is not growing, and missed that the
// alternative {true} S "q" in S = R "z" / {true} S "q" / "y" drops the result of R (condition 5).
var leadingPassOn = true

// startsWithLeftCall reports whether n can evaluate a call of a rule of the cycle of rule before
// it examines the input (leftExamine) or consumes input (leftConsume). Before such a call may come
// predicates, cuts, anchors and calls of rules off the cycle that can match without examining or
// consuming input; for leftConsume also parts that can match without consuming input, as an
// option does, and lookaheads. The decision errs toward the call: a part that might go on
// without examining or consuming input is looked through.
func (k *classifier) startsWithLeftCall(rule string, n core.Node, mode leftMode) bool {
	found, _ := k.leftScan(rule, n, mode, 0)
	return found
}

// leftScan returns whether n can evaluate such a call, and whether n can complete before it
// examines (leftExamine) or consumes (leftConsume) input, so that what follows n is reached too.
// The scan stops at attributes: it reports neither for @error, @diagnostic, #recover, #missing and
// #stream.
func (k *classifier) leftScan(rule string, n core.Node, mode leftMode, depth int) (found, through bool) {
	k.control.Poll()
	if depth > 64 {
		return false, false
	}
	switch n := n.(type) {
	case nil:
		return false, true
	case *core.Call:
		if callee := k.rules[n.Rule].Name; plainCall(n) && k.an.Cyclic[callee] && k.sameCycle(rule, callee) {
			return true, false
		}
		return false, leadingPassOn && k.passesBefore(n, mode)
	case *core.Capture:
		return k.leftScan(rule, n.Body, mode, depth+1)
	case *core.Drop:
		return k.leftScan(rule, n.Body, mode, depth+1)
	case *core.Text:
		return k.leftScan(rule, n.Body, mode, depth+1)
	case *core.Seq:
		through = true
		for _, it := range n.Items {
			f, t := k.leftScan(rule, it, mode, depth+1)
			found = found || f
			if !t {
				return found, false
			}
		}
		return found, through
	case *core.Choice:
		for _, alt := range n.Alts {
			f, t := k.leftScan(rule, alt.Body, mode, depth+1)
			found, through = found || f, through || t
		}
		return found, through
	case *core.Opt:
		f, _ := k.leftScan(rule, n.Body, mode, depth+1)
		return f, mode == leftConsume
	case *core.Repeat:
		f, t := k.leftScan(rule, n.Body, mode, depth+1)
		return f, mode == leftConsume && (n.Min == 0 || n.Max == 0 || t)
	case *core.Look:
		return false, mode == leftConsume
	case *core.Pred, *core.BindNode, *core.Cut, *core.Succeed, *core.Anchor:
		return false, leadingPassOn
	case *core.Lit:
		return false, n.Text == ""
	}
	return false, false
}

// plainCall reports whether the call c applies its rule as the rule's body does: it restricts no
// level of an Operators Rule.
func plainCall(c *core.Call) bool { return c.Level == 0 }

// passesBefore reports whether n, a call of a rule that is not on the cycle, can match without
// examining the input (leftExamine) or consuming input (leftConsume). A call that restricts the
// levels of an Operators Rule, and a right operand of one (isRightOperand), are not looked
// through.
func (k *classifier) passesBefore(n core.Node, mode leftMode) bool {
	c, ok := n.(*core.Call)
	if !ok || !plainCall(c) {
		return false
	}
	name := k.rules[c.Rule].Name
	if mode == leftConsume {
		return k.nullRule[name]
	}
	return k.examinesNothing(name, map[string]bool{}, false)
}

// examinesNothing reports whether the rule can match without examining the input: some path of
// its body evaluates only predicates, empty literals, anchors and calls of such rules. With every
// set, whether every path does.
func (k *classifier) examinesNothing(name string, seen map[string]bool, every bool) bool {
	r := k.byName[name]
	if r == nil || seen[name] {
		return false
	}
	seen[name] = true
	defer delete(seen, name)
	var nothing func(n core.Node) bool
	nothing = func(n core.Node) bool {
		switch n := n.(type) {
		case nil:
			return true
		case *core.Call:
			return plainCall(n) && k.examinesNothing(k.rules[n.Rule].Name, seen, every)
		case *core.Capture:
			return nothing(n.Body)
		case *core.Drop:
			return nothing(n.Body)
		case *core.Text:
			return nothing(n.Body)
		case *core.Seq:
			for _, it := range n.Items {
				if !nothing(it) {
					return false
				}
			}
			return true
		case *core.Choice:
			for _, alt := range n.Alts {
				if nothing(alt.Body) != every {
					return !every
				}
			}
			return every
		case *core.Pred, *core.BindNode, *core.Cut, *core.Succeed, *core.Anchor:
			return true
		case *core.Lit:
			return n.Text == ""
		}
		return false
	}
	return nothing(k.ruleBody(name))
}
