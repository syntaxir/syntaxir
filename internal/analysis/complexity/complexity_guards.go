package complexity

import (
	"math"

	"syntaxir.dev/sxir/internal/ir/core"
)

// guard is what the predicates next to a binding of an int variable test of the value that it
// binds. A failed test fails the sequence that holds the binding, and backtracking undoes the
// binding, so a value that the guard rejects reaches no call and no later binding.
type guard struct {
	// tests are the tests after the binding that read only its variable and literals.
	tests []core.ActionExpr
	// bounds holds every value that passes the tests after the binding and the tests before it.
	bounds valueRange
}

// passes reports whether the value y of the variable v can pass the guard: it is within the
// bounds and no test is false or an error on it.
func (g guard) passes(v string, y int) bool {
	if y < g.bounds.lo || y > g.bounds.hi {
		return false
	}
	for _, t := range g.tests {
		r, res := eval(t, domainEnv{v: v, x: y})
		if b, isBool := r.(bool); res == evalFailed || res == evalOK && isBool && !b {
			return false
		}
	}
	return true
}

// guardOf returns the guard of the binding b of an int variable v := e. Between the binding and
// the tests after it, and between the tests before it and the binding, only predicates run (see
// adjacentPredicates), so nothing reads the input or calls a rule there.
//
//   - A test after the binding, up to the next binding of v, that reads only v and literals
//     tests the value that b binds: `{v := e} {lo <= v && v <= hi}`. Its comparisons of v with
//     constants bound the value.
//   - A test before the binding, after the last binding of any variable before it, evaluates e
//     with the same variables and captures as b does, and every action expression of the action
//     language is a function of them: `{lo <= e && e <= hi} {v := e}`. Its comparisons of e
//     with constants bound the value.
//
// A test of a conjunction bounds the value by each of its comparisons that has the form; a
// single comparison bounds one side, which the range of e (see actionRange) can bound on the other.
func (k *classifier) guardOf(b *binding) guard {
	g := guard{bounds: fullRange}
	v, e := b.n.Var, b.n.Action
	isVar := func(t core.ActionExpr) bool {
		r, ok := t.(*core.VarRef)
		return ok && r.Name == v
	}
	text := core.ActionExprString(e)
	isValue := func(t core.ActionExpr) bool { return core.ActionExprString(t) == text }
	after, before := k.adjacentPredicates(b)
	for _, p := range after {
		if bd, ok := p.(*core.BindNode); ok {
			if bd.Var == v {
				break
			}
			continue
		}
		if s := p.(*core.Pred).Action; readsOnly(s, v, nil) {
			g.tests = append(g.tests, s)
			g.bounds = g.bounds.meet(actionBounds(s, isVar))
		}
	}
	for _, p := range before {
		pred, ok := p.(*core.Pred)
		if !ok {
			break
		}
		g.bounds = g.bounds.meet(actionBounds(pred.Action, isValue))
	}
	return g
}

// adjacentPredicates returns the predicates and bindings that run right after the binding b and
// right before it, each list in the order of evaluation away from b: those next to b in the
// sequence that holds it, up to the first element on either side that is not one. The module
// splices the statements of a predicate block into that sequence. A predicate under labels
// counts, since labels do not change matching.
func (k *classifier) adjacentPredicates(b *binding) (after, before []core.Node) {
	if len(b.c.seqs) == 0 {
		return nil, nil
	}
	f := b.c.seqs[len(b.c.seqs)-1]
	items := f.seq.Items
	if predicateOf(items[f.index]) != core.Node(b.n) {
		return nil, nil
	}
	for _, it := range items[f.index+1:] {
		q := predicateOf(it)
		if q == nil {
			break
		}
		after = append(after, q)
	}
	for j := f.index - 1; j >= 0; j-- {
		q := predicateOf(items[j])
		if q == nil {
			break
		}
		before = append(before, q)
	}
	return after, before
}

// predicateOf returns the predicate node, a core.Pred or core.BindNode, that n is through labels
// (@error and @diagnostic, which change diagnostics, not matching, backtracking or bindings), or
// nil.
func predicateOf(n core.Node) core.Node {
	for {
		switch x := n.(type) {
		case *core.Pred, *core.BindNode:
			return x
		case *core.Label:
			n = x.Body
		default:
			return nil
		}
	}
}

// valueRange is an inclusive range of ints. A bound beyond maxExact in magnitude stands for no
// bound on that side; an empty range has hi < lo.
type valueRange struct{ lo, hi int }

var fullRange = valueRange{-maxExact - 1, maxExact + 1}

func (r valueRange) meet(s valueRange) valueRange {
	return valueRange{max(r.lo, s.lo), min(r.hi, s.hi)}
}

func (r valueRange) bounded() bool { return r.lo >= -maxExact && r.hi <= maxExact }

// size returns the number of values in r, or math.MaxInt when r is not bounded.
func (r valueRange) size() int {
	switch {
	case r.hi < r.lo:
		return 0
	case !r.bounded():
		return math.MaxInt
	}
	return r.hi - r.lo + 1
}

// rangeOf returns the range from lo to hi, with a bound beyond maxExact in magnitude dropped.
func rangeOf(lo, hi int) valueRange {
	if lo < -maxExact {
		lo = fullRange.lo
	}
	if hi > maxExact {
		hi = fullRange.hi
	}
	return valueRange{lo, hi}
}

// actionBounds returns the range that t bounds the subject to when t is true: t is a comparison
// with <, <=, >, >= or == of the subject with a constant int (see constInt), on either side, or a
// conjunction with && of such tests and others, which do not bound it.
func actionBounds(t core.ActionExpr, subject func(core.ActionExpr) bool) valueRange {
	bin, ok := t.(*core.Binary)
	if !ok {
		return fullRange
	}
	if bin.Op == "&&" {
		return actionBounds(bin.L, subject).meet(actionBounds(bin.R, subject))
	}
	op, c := bin.Op, 0
	if n, isConst := constInt(bin.R); isConst && subject(bin.L) {
		c = n
	} else if n, isConst := constInt(bin.L); isConst && subject(bin.R) {
		c = n
		op = map[string]string{"<": ">", "<=": ">=", ">": "<", ">=": "<=", "==": "=="}[op]
	} else {
		return fullRange
	}
	switch op {
	case "<":
		return rangeOf(fullRange.lo, c-1)
	case "<=":
		return rangeOf(fullRange.lo, c)
	case ">":
		return rangeOf(c+1, fullRange.hi)
	case ">=":
		return rangeOf(c, fullRange.hi)
	case "==":
		return valueRange{c, c}
	}
	return fullRange
}

// constInt returns the value of t when t is a constant int expression that eval knows.
func constInt(t core.ActionExpr) (int, bool) {
	if !constant(t) {
		return 0, false
	}
	r, res := eval(t, domainEnv{})
	n, isInt := r.(int)
	return n, res == evalOK && isInt
}

// actionRange returns a range that holds every int that t can give when the variable v holds a
// value of xs: a literal is its value, v ranges over xs, len gives at least 0, unary -, + and *
// combine the ranges of their operands, and any other action expression has no bound.
func actionRange(t core.ActionExpr, v string, xs valueSet) valueRange {
	switch t := t.(type) {
	case *core.Const:
		if n, ok := constInt(t); ok {
			return valueRange{n, n}
		}
	case *core.VarRef:
		if t.Name == v && len(xs) > 0 {
			lo, hi := math.MaxInt, math.MinInt
			for x := range xs {
				lo, hi = min(lo, x), max(hi, x)
			}
			return valueRange{lo, hi}
		}
	case *core.Builtin:
		if t.Func == "len" && len(t.Args) == 1 {
			return valueRange{0, fullRange.hi}
		}
	case *core.Unary:
		if t.Op == "-" {
			r := actionRange(t.X, v, xs)
			return valueRange{-r.hi, -r.lo}
		}
	case *core.Binary:
		l, r := actionRange(t.L, v, xs), actionRange(t.R, v, xs)
		switch t.Op {
		case "+":
			return addRanges(l, r)
		case "-":
			return addRanges(l, valueRange{-r.hi, -r.lo})
		case "*":
			return mulRanges(l, r)
		}
	}
	return fullRange
}

// addRanges returns the range of the sums of the values of l and r.
func addRanges(l, r valueRange) valueRange {
	out := fullRange
	if l.lo >= -maxExact && r.lo >= -maxExact {
		out.lo = rangeOf(l.lo+r.lo, 0).lo
	}
	if l.hi <= maxExact && r.hi <= maxExact {
		out.hi = rangeOf(0, l.hi+r.hi).hi
	}
	return out
}

// mulRanges returns the range of the products of the values of l and r, when both are bounded.
func mulRanges(l, r valueRange) valueRange {
	if !l.bounded() || !r.bounded() || l.size() == 0 || r.size() == 0 {
		return fullRange
	}
	lo, hi := math.MaxInt, math.MinInt
	for _, a := range []int{l.lo, l.hi} {
		for _, b := range []int{r.lo, r.hi} {
			if a != 0 && abs(b) > maxExact/abs(a) {
				return fullRange
			}
			lo, hi = min(lo, a*b), max(hi, a*b)
		}
	}
	return rangeOf(lo, hi)
}
