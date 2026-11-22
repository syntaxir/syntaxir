package examinedref

import (
	"fmt"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// unbounded is the bound of an expression whose last step was an operand or a postfix operator:
// every operator may follow it.
const unbounded = int(^uint(0) >> 1)

// levelOf returns the binding level of a call of rule with the level name: the minimum level, below
// the named level, so that the call applies the operators of that level and of the tighter ones.
// Levels are numbered from 1 in declaration order (spec/core-ir/nodes.md, Call).
func (ev *evaluator) levelOf(rule, name string) (int, error) {
	r := ev.rules[rule]
	if r == nil || r.ops == nil {
		return 0, fmt.Errorf("examinedref: level(%s, %s) of a rule without an Operators Rule", rule, name)
	}
	for i, l := range r.ops.Levels {
		if l.Name == name {
			return i, nil
		}
	}
	return 0, fmt.Errorf("examinedref: rule %s has no level %s", rule, name)
}

// opPart is an operator part of an Operators Rule with its binding level.
type opPart struct {
	op    *ast.OperatorsRuleOperator
	level int
}

// opsRun is the evaluation of one call of an Operators Rule. Parsing the operand of a prefix operator
// and the right operand of an infix operator is part of the call, not a call of its own; h collects
// the examined end of everything the call evaluates.
type opsRun struct {
	ev          *evaluator
	r           *rule
	prefix, led []opPart
	h           int
}

// opsResult is the result of an expression of the run: on a match, the state after it, its value
// and the position its node starts at; and the bound of the last step, which is the level of the
// prefix operator of the start, or unbounded.
type opsResult struct {
	ok    bool
	st    state
	val   value
	start int
}

// attempt is the longest match of operator parts at a position: the part, the state after it, its
// value, its captures and whether it passed a cut.
type attempt struct {
	part  *opPart
	start int
	after state
	val   value
	caps  *capture
	cut   bool
}

// operators evaluates the Operators Rule of r at st with the minimum binding level min
// (spec/language/operators-rule.md, Parsing Rules and Level-Restricted Calls).
func (ev *evaluator) operators(r *rule, min int, st state) outcome {
	run := &opsRun{ev: ev, r: r, h: st.pos}
	for i, l := range r.ops.Levels {
		for _, op := range l.Operators {
			p := opPart{op: op, level: i + 1}
			if op.Kind == ast.Prefix {
				run.prefix = append(run.prefix, p)
			} else {
				run.led = append(run.led, p)
			}
		}
	}
	res := run.expr(min, st, false)
	if !res.ok {
		return failed(st, run.h, false)
	}
	return outcome{matched: true, end: res.st.pos, h: run.h, vars: res.st.vars, caps: st.caps, path: res.st.path, vals: []value{res.val}}
}

func (x *opsRun) see(h int) { x.h = maxInt(x.h, h) }

// skip matches the skip of the rule, if it has one, as a lookahead does for token insertion: nothing
// is inserted in it, and a failure leaves the state as it was.
func (x *opsRun) skip(st state) state {
	s := x.r.ops.Skip
	if s == nil {
		return st
	}
	in := st
	in.look, in.caps = true, nil
	o := x.ev.eval(s, in)
	x.see(o.h)
	if !o.matched {
		return st
	}
	st.pos, st.vars = o.end, o.vars
	return st
}

// longest tries every part of parts at st and returns the longest match, the first declared among
// equals. A prefix or postfix part that matches the empty string is not a candidate. cutFailed
// reports that a part failed, or was rejected, after passing a cut.
func (x *opsRun) longest(parts []opPart, st state) (best *attempt, cutFailed bool) {
	for i := range parts {
		p := &parts[i]
		line := st
		line.caps = nil
		o := x.ev.eval(p.op.Expr, line)
		x.see(o.h)
		matched := o.matched && !(o.end == st.pos && p.op.Kind != ast.Infix)
		if matched && (best == nil || o.end > best.after.pos) {
			after := advance(line, o)
			best = &attempt{part: p, start: st.pos, after: after, val: x.ev.single(o, line), caps: o.caps, cut: o.cut}
			if !x.ev.valued(p.op.Expr) {
				best.val = &node{kind: tupleNode, start: st.pos, end: o.end, caps: o.caps}
			}
		}
		if !matched && o.cut {
			cutFailed = true
		}
	}
	return best, cutFailed
}

// apply moves st past the operator part of a.
func apply(st state, a *attempt) state {
	st.pos, st.vars, st.path = a.after.pos, a.after.vars, a.after.path
	return st
}

// nud parses the start of an expression: the application of the longest prefix operator to its
// operand, or else the first operand line that matches. It returns the bound of the start.
func (x *opsRun) nud(st state) (opsResult, int) {
	st = x.skip(st)
	a, cutFailed := x.longest(x.prefix, st)
	if a != nil {
		rhs := x.expr(a.part.level, apply(st, a), false)
		if rhs.ok {
			return opsResult{ok: true, st: rhs.st, val: x.build(a, nil, rhs, a.start), start: a.start}, a.part.level
		}
		if a.cut {
			return opsResult{}, 0
		}
	} else if cutFailed {
		return opsResult{}, 0
	}
	for _, operand := range x.r.ops.Operands {
		line := st
		line.caps = nil
		o := x.ev.eval(operand.Expr, line)
		x.see(o.h)
		if o.matched {
			var v value
			switch {
			case operand.Action != nil && !operand.DefaultAction:
				v = x.ev.actionValue(operand.Action, o.vars, o.caps)
			case x.ev.valued(operand.Expr):
				v = x.ev.single(o, line)
			default:
				v = &node{kind: tupleNode, start: st.pos, end: o.end, caps: o.caps}
			}
			after := advance(line, o)
			after.caps, after.look, after.fold = st.caps, st.look, st.fold
			return opsResult{ok: true, st: after, val: v, start: st.pos}, unbounded
		}
		if o.cut {
			return opsResult{}, 0
		}
	}
	return opsResult{}, 0
}

// expr parses an expression that applies the operators binding tighter than min. afterEmpty reports
// the right operand of an operator part that consumed no input, with no skip text: such an operand
// does not apply an empty part until it has consumed input.
func (x *opsRun) expr(min int, st state, afterEmpty bool) opsResult {
	guard := -1
	if afterEmpty {
		guard = st.pos
	}
	lhs, bound := x.nud(st)
	if !lhs.ok || len(x.led) == 0 {
		return lhs
	}
	lastNone := -1
	cur := lhs.st
	for {
		start := cur.pos
		a, cutFailed := x.longest(x.led, x.skip(cur))
		if a == nil {
			if cutFailed {
				return opsResult{}
			}
			break
		}
		level := a.part.level
		if level <= min || level > bound || a.part.op.Assoc == ast.AssocNone && level == lastNone {
			break
		}
		if start == guard && a.after.pos == start {
			break
		}
		after := apply(cur, a)
		if a.part.op.Kind == ast.Postfix {
			lhs.val = x.build(a, &lhs, opsResult{}, lhs.start)
			cur, lastNone, bound = after, -1, unbounded
			if cur.pos == start {
				break
			}
			continue
		}
		rmin := level
		if a.part.op.Assoc == ast.AssocRight {
			rmin--
		}
		rhs := x.expr(rmin, after, after.pos == start)
		if !rhs.ok {
			if a.cut {
				return opsResult{}
			}
			break
		}
		lhs.val = x.build(a, &lhs, rhs, lhs.start)
		cur, lastNone, bound = rhs.st, -1, rmin
		if a.part.op.Assoc == ast.AssocNone {
			lastNone = level
		}
		if cur.pos == start {
			break
		}
	}
	lhs.st = cur
	return lhs
}

// build is the value of an operator application: the tuple node of the left operand, the operator
// part and the right operand that it has, captured as lhs, op and rhs, or the value of the operator's
// action, which reads them as $lhs, $op and $rhs besides the captures of the operator part. With a
// declared named node type and no whole-rule action, the value is converted to that type.
func (x *opsRun) build(a *attempt, lhs *opsResult, rhs opsResult, start int) value {
	op := a.part.op
	end, vars := a.after.pos, a.after.vars
	if rhs.ok {
		end, vars = rhs.st.pos, rhs.st.vars
	}
	n := &node{kind: tupleNode, start: start, end: end}
	if lhs != nil {
		n.elems = append(n.elems, lhs.val)
		n.caps = &capture{name: "lhs", val: lhs.val, next: n.caps}
	}
	n.elems = append(n.elems, a.val)
	n.caps = &capture{name: "op", val: a.val, next: n.caps}
	if rhs.ok {
		n.elems = append(n.elems, rhs.val)
		n.caps = &capture{name: "rhs", val: rhs.val, next: n.caps}
	}
	var v value = n
	if op.Action != nil && !op.DefaultAction {
		caps := a.caps
		for c := n.caps; c != nil; c = c.next {
			caps = &capture{name: c.name, val: c.val, next: caps}
		}
		v = x.ev.actionValue(op.Action, vars, caps)
	}
	if m, ok := v.(*node); ok && x.r.nodeType != "" && x.r.act == nil {
		c := *m
		c.name = x.r.nodeType
		v = &c
	}
	return v
}
