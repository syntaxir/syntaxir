package corepass

import (
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
// the scope of its body, so a cut in it counts. It polls no control; Facts.LooseCut does.
func LooseCut(n core.Node) bool { return looseCut(n) }

// LooseCut is the package's LooseCut, polling the control of f at each node it visits.
func (f *Facts) LooseCut(n core.Node) bool { return f.c.looseCut(n) }

// Nullable reports that n can succeed without consuming input when no token is inserted, by the
// rules of core.Analyze, with the nullability of the rules it calls. It answers for a node that
// Analyze did not see, such as one a rewrite builds, whose attributes are zero.
func (f *Facts) Nullable(n core.Node) bool { return f.c.nullable(n, false) }

// NullableIns reports that n can succeed without consuming input when token insertion is enabled
// (core.Attrs.NullableIns): as Nullable, except that a #missing literal can be inserted. It answers
// for any node, as Nullable does.
func (f *Facts) NullableIns(n core.Node) bool { return f.c.nullable(n, true) }

// nullable reports that n can succeed without consuming input, with token insertion enabled if
// ins (nullabilities).
func (c *computer) nullable(n core.Node, ins bool) bool {
	null, nullIns := c.nullabilities(n)
	return null || ins && nullIns
}

// nullabilities returns the nullabilities of n, without token insertion and with it, by the rule of
// core.Nullability and the nullabilities of the rules it calls. A node that Analyze numbered and saw
// (a nonzero ID) has them as attributes; any other is computed.
func (c *computer) nullabilities(n core.Node) (bool, bool) {
	if n == nil {
		return true, true
	}
	if at := n.Attrs(); at.ID != 0 {
		return at.Nullable, at.NullableIns
	}
	return core.Nullability(n, c.nullabilities, func(id int) (bool, bool) {
		r := &c.rules[c.canon[id]]
		return r.Nullable, r.NullableIns
	})
}
