package corepass

import (
	"fmt"

	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/ir/core"
)

// The queries below answer for any node of the module the facts were computed on, with the rule
// facts of Facts. A node's nullability, first terminal and effects are its attributes
// (core.Node.Attrs); these are the facts that depend on the rules called.

// WithControl returns the facts f with node queries that poll control, which may be nil, in place of
// the control they were computed under: a tool that reads the facts later polls its own. The copy
// shares the rule facts of f.
func (f *Facts) WithControl(control *cancel.Control) *Facts {
	c := *f.c
	c.control = control
	return &Facts{Rules: f.Rules, c: &c}
}

// Succeeds reports a proof that n cannot fail, at any position. Errors that end the parse are
// not failures.
func (f *Facts) Succeeds(n core.Node) bool { return f.c.alwaysSucceeds(n) }

// NullableAtEnd reports that n can succeed without consuming input when the input is at its end
// (see Rule.NullableAtEnd).
func (f *Facts) NullableAtEnd(n core.Node) bool { return f.c.nullableAt(n, true) }

// NullableBeforeEnd reports that n can succeed without consuming input at a position before the end
// of the input (see Rule.NullableBeforeEnd).
func (f *Facts) NullableBeforeEnd(n core.Node) bool { return f.c.nullableAt(n, false) }

// MayMatch reports that n can succeed (see Rule.CanMatch).
func (f *Facts) MayMatch(n core.Node) bool { return f.c.mayMatch(n) }

// Fails reports a proof that n cannot succeed.
func (f *Facts) Fails(n core.Node) bool { return f.c.alwaysFails(n) }

// LeftCalls returns the rules n may call before it consumes input, in the order of the body, a
// rule called at several such places listed at each; with token insertion enabled if ins.
func (f *Facts) LeftCalls(n core.Node, ins bool) []int { return f.c.leftCalls(n, nil, ins) }

// LooseCut reports a cut in n that can commit the choice, option or repetition around n: one
// outside nested choices, options, repetitions, lookaheads and calls. A recovery's skip runs in
// the scope of its body, so a cut in it counts.
func LooseCut(n core.Node) bool { return looseCut(n) }

// Nullable reports that n can succeed without consuming input when no token is inserted, by the
// rules of core.Analyze, with the nullability of the rules it calls. It answers for a node that
// Analyze did not see, such as one a rewrite builds, whose attributes are zero.
func (f *Facts) Nullable(n core.Node) bool { return f.c.nullable(n, false) }

// NullableIns reports that n can succeed without consuming input when token insertion is enabled
// (core.Attrs.NullableIns): as Nullable, except that a #missing literal can be inserted. It answers
// for any node, as Nullable does.
func (f *Facts) NullableIns(n core.Node) bool { return f.c.nullable(n, true) }

// nullable reports that n can succeed without consuming input, with token insertion enabled if
// ins, by the rules of core.Analyze and the nullability of the rules it calls. A node that Analyze
// numbered and saw (a nonzero ID) has its nullability as attributes; any other is computed.
func (c *computer) nullable(n core.Node, ins bool) bool {
	if n == nil {
		return true
	}
	if at := n.Attrs(); at.ID != 0 {
		return at.Nullable || ins && at.NullableIns
	}
	switch n := n.(type) {
	case *core.Lit:
		return n.Text == ""
	case *core.Scan:
		return n.Min == 0
	case *core.ClassNode, *core.Any, *core.FailNode, *core.ClassRun:
		return false
	case *core.Anchor, *core.Opt, *core.Look, *core.Cut, *core.Succeed, *core.Pred, *core.BindNode:
		return true
	case *core.Seq:
		for _, it := range n.Items {
			if !c.nullable(it, ins) {
				return false
			}
		}
		return true
	case *core.Choice:
		for _, alt := range n.Alts {
			if c.nullable(alt.Body, ins) {
				return true
			}
		}
		return false
	case *core.Repeat:
		return n.Min == 0 || n.Min == 1 && c.nullable(n.Body, ins)
	case *core.Call:
		r := &c.rules[c.canon[n.Rule]]
		return r.Nullable || ins && r.NullableIns
	case *core.OperatorsRule:
		for _, l := range n.Operands {
			if c.nullable(l.Body, ins) {
				return true
			}
		}
		return false
	case *core.Missing:
		// An insertion consumes no input.
		return ins || c.nullable(n.Lit, false)
	case *core.Stream:
		return c.nullable(n.Repeat, ins)
	case *core.Nest:
		return c.nullable(n.Body, ins)
	case *core.Text:
		return c.nullable(n.Body, ins)
	case *core.Drop:
		return c.nullable(n.Body, ins)
	case *core.Capture:
		return c.nullable(n.Body, ins)
	case *core.Label:
		return c.nullable(n.Body, ins)
	case *core.Recover:
		return c.nullable(n.Body, ins)
	case *core.Terminal:
		return c.nullable(n.Body, ins)
	}
	panic(fmt.Sprintf("corepass: nullability of %T", n))
}
