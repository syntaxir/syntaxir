package corepass

import (
	"fmt"

	"syntaxir.dev/sxir/internal/ir/core"
)

// The facts that depend on the position of the input: whether a node can succeed without
// consuming input when the input is at its end, and whether a node can match at all. They are
// over-approximations (nullability, matching) and under-approximations (success, failure) in the
// same sense as the other facts, and are the least fixed points over the rules.

// nullableEnd computes the NullableAtEnd and NullableBeforeEnd facts of the rules.
func (c *computer) nullableEnd() {
	c.fixRules(func(id int) bool { return c.nullableAt(c.m.Rules[id].Body, true) },
		func(id int) *bool { return &c.rules[id].NullableAtEnd })
	c.fixRules(func(id int) bool { return c.nullableAt(c.m.Rules[id].Body, false) },
		func(id int) *bool { return &c.rules[id].NullableBeforeEnd })
}

// canMatch computes the CanMatch facts of the rules.
func (c *computer) canMatch() {
	c.fixRules(func(id int) bool { return c.mayMatch(c.m.Rules[id].Body) },
		func(id int) *bool { return &c.rules[id].CanMatch })
}

// fixRules sets, to a least fixed point over the rules that are not twins, each fact for which
// holds is true.
func (c *computer) fixRules(holds func(id int) bool, fact func(id int) *bool) {
	for changed := true; changed; {
		changed = false
		for _, id := range c.order {
			c.control.Poll()
			if p := fact(id); !*p && holds(id) {
				*p, changed = true, true
			}
		}
	}
}

// nullableAt reports that n can succeed without consuming input: at the end of the input if end
// is set, and at another position otherwise. At the end, a lookahead's body must match without
// input, and a negative lookahead succeeds unless its body certainly succeeds there.
func (c *computer) nullableAt(n core.Node, end bool) bool {
	c.control.Poll()
	switch n := n.(type) {
	case nil:
		return true
	case *core.Lit:
		return n.Text == ""
	case *core.ClassNode, *core.Any, *core.FailNode, *core.ClassRun:
		return false
	case *core.Anchor:
		return n.Kind != core.EndInput || end
	case *core.Look:
		if n.Neg {
			return !c.succeedsAt(n.Body, end)
		}
		return !end || c.nullableAt(n.Body, true)
	case *core.Opt, *core.Cut, *core.Succeed, *core.Pred, *core.BindNode:
		return true
	case *core.Seq:
		for _, it := range n.Items {
			if !c.nullableAt(it, end) {
				return false
			}
		}
		return true
	case *core.Choice:
		for _, alt := range n.Alts {
			if c.nullableAt(alt.Body, end) {
				return true
			}
		}
		return false
	case *core.Repeat:
		return n.Min == 0 || n.Min == 1 && c.nullableAt(n.Body, end)
	case *core.Call:
		if end {
			return c.rules[c.canon[n.Rule]].NullableAtEnd
		}
		return c.rules[c.canon[n.Rule]].NullableBeforeEnd
	case *core.OperatorsRule:
		for _, l := range n.Operands {
			if c.nullableAt(l.Body, end) {
				return true
			}
		}
		return false
	case *core.Missing:
		return c.nullableAt(n.Lit, end)
	case *core.Scan:
		return n.Min == 0
	case *core.Stream:
		return c.nullableAt(n.Repeat, end)
	case *core.Nest:
		return c.nullableAt(n.Body, end)
	case *core.Text:
		return c.nullableAt(n.Body, end)
	case *core.Drop:
		return c.nullableAt(n.Body, end)
	case *core.Capture:
		return c.nullableAt(n.Body, end)
	case *core.Label:
		return c.nullableAt(n.Body, end)
	case *core.Recover:
		return c.nullableAt(n.Body, end)
	case *core.Terminal:
		return c.nullableAt(n.Body, end)
	}
	panic(fmt.Sprintf("corepass: nullability at the end of %T", n))
}

// succeedsAt reports a proof that n cannot fail at the end of the input if end is set, and at
// another position otherwise. Only the nodes that match the input's end or its characters differ
// from the position-free proof (alwaysSucceeds).
func (c *computer) succeedsAt(n core.Node, end bool) bool {
	c.control.Poll()
	switch n := unwrapCore(n).(type) {
	case *core.Anchor:
		if n.Kind == core.EndInput {
			return end
		}
	case *core.Any:
		return !end
	case *core.Look:
		if n.Neg {
			return c.failsAt(n.Body, end)
		}
	}
	return c.alwaysSucceeds(n)
}

// failsAt reports a proof that n cannot succeed at the end of the input if end is set, and at
// another position otherwise. It is an under-approximation: a node whose failure at a position
// depends on the rules it calls is not proven.
func (c *computer) failsAt(n core.Node, end bool) bool {
	c.control.Poll()
	switch n := unwrapCore(n).(type) {
	case *core.Anchor:
		if n.Kind == core.EndInput {
			return !end
		}
	case *core.Any, *core.ClassNode:
		return end
	case *core.Lit:
		return end && n.Text != ""
	case *core.FailNode:
		return true
	case *core.Look:
		if n.Neg {
			return c.succeedsAt(n.Body, end)
		}
	}
	return false
}

// unwrapCore strips the nodes that do not change whether a node matches or how much input it
// consumes: captures, drops, texts, labels, streams and the missing-token marks of a literal.
func unwrapCore(n core.Node) core.Node {
	for {
		switch x := n.(type) {
		case *core.Capture:
			n = x.Body
		case *core.Drop:
			n = x.Body
		case *core.Text:
			n = x.Body
		case *core.Label:
			n = x.Body
		case *core.Stream:
			n = x.Repeat
		case *core.Missing:
			n = x.Lit
		default:
			return n
		}
	}
}

// mayMatch reports that n can succeed: a recovery and a missing literal always may, a negative
// lookahead may unless its body always succeeds, and a call may if its rule may.
func (c *computer) mayMatch(n core.Node) bool {
	c.control.Poll()
	switch n := n.(type) {
	case *core.FailNode:
		return false
	case *core.Call:
		return c.rules[c.canon[n.Rule]].CanMatch
	case *core.Seq:
		for _, it := range n.Items {
			if !c.mayMatch(it) {
				return false
			}
		}
		return true
	case *core.Choice:
		for _, alt := range n.Alts {
			if c.mayMatch(alt.Body) {
				return true
			}
		}
		return false
	case *core.Repeat:
		return n.Min == 0 || c.mayMatch(n.Body)
	case *core.Look:
		if n.Neg {
			return !c.alwaysSucceeds(n.Body)
		}
		return c.mayMatch(n.Body)
	case *core.OperatorsRule:
		for _, l := range n.Operands {
			if c.mayMatch(l.Body) {
				return true
			}
		}
		return false
	case *core.Capture:
		return c.mayMatch(n.Body)
	case *core.Drop:
		return c.mayMatch(n.Body)
	case *core.Text:
		return c.mayMatch(n.Body)
	case *core.Label:
		return c.mayMatch(n.Body)
	case *core.Stream:
		return c.mayMatch(n.Repeat)
	case *core.Terminal:
		return c.mayMatch(n.Body)
	}
	return true
}
