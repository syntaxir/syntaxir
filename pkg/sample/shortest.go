package sample

import "syntaxir.dev/sxir/pkg/grammar/ast"

// The lengths in this file read the grammar's structure only: lookaheads, predicates and the order
// of choices are left out, so they are lower bounds of what the parser accepts.

// through measures the shortest match of an expression that exercises something: the target goal,
// or a rule call for which ref gives a length.
type through struct {
	in *info
	// goal is a target, or -1. The target of a rule is exercised by every match of the rule.
	goal int
	// ref returns the length of the shortest match of a call of the rule that exercises what is
	// looked for, or inf. It is not called for the rule of goal.
	ref func(ri *ruleInfo) int
}

// addLen adds lengths, saturating at inf.
func addLen(a, b int) int { return int(min(int64(inf), int64(a)+int64(b))) }

// expr returns the length of the shortest match of e that exercises what is looked for, or inf. It
// does not enter called rules: ref answers for them.
func (p *through) expr(e ast.Expr) int {
	in := p.in
	switch e := e.(type) {
	case *ast.Ref:
		ri := in.rules[e.Name]
		switch {
		case ri == nil || ri.height >= inf:
			return inf
		case ri.index == p.goal:
			return ri.length
		}
		return p.ref(ri)
	case *ast.Seq:
		total := in.length(e)
		if total >= inf {
			return inf
		}
		best := inf
		for _, it := range e.Items {
			if t := p.expr(it); t < inf {
				best = min(best, addLen(total-in.length(it), t))
			}
		}
		return best
	case *ast.Choice:
		best := inf
		for j, a := range e.Alts {
			if in.altBase[e]+j == p.goal {
				best = min(best, in.length(a))
			}
			best = min(best, p.expr(a))
		}
		return best
	case *ast.Repeat:
		t := p.expr(e.Expr)
		if e.Max == 0 || t >= inf {
			return inf
		}
		// One iteration exercises it, the others are as short as possible.
		others := int(min(int64(inf), int64(max(min(e.Min, inf), 1)-1)*int64(in.length(e.Expr))))
		return addLen(t, others)
	case *ast.Optional:
		return p.expr(e.Expr)
	case *ast.And:
		// The text of a positive lookahead is matched again by what follows it: counting it would
		// count it twice.
		if p.expr(e.Expr) < inf {
			return 0
		}
	case *ast.Atomic:
		return p.expr(e.Expr)
	case *ast.Discard:
		return p.expr(e.Expr)
	case *ast.Capture:
		return p.expr(e.Expr)
	case *ast.Attributed:
		return p.expr(e.Expr)
	case *ast.OperatorsRule:
		return p.operators(e)
	}
	return inf
}

// operators returns the length for an Operators Rule body: an operand, a skip followed by an operand,
// or an operator part with the operands it needs, each operand as short as possible. The levels that
// a call permits are left out, which keeps the length a lower bound.
func (p *through) operators(pr *ast.OperatorsRule) int {
	in := p.in
	operand := in.length(pr)
	if operand >= inf {
		return inf
	}
	own := func(t int, e ast.Expr) int {
		if t == p.goal {
			return in.length(e)
		}
		return p.expr(e)
	}
	best := inf
	if pr.Skip != nil {
		if t := p.expr(pr.Skip); t < inf {
			best = addLen(t, operand)
		}
	}
	for _, o := range pr.Operands {
		best = min(best, own(in.operands[o], o.Expr))
	}
	for _, l := range pr.Levels {
		for _, op := range l.Operators {
			t := own(in.ops[op], op.Expr)
			if t >= inf {
				continue
			}
			t = addLen(t, operand)
			if op.Kind == ast.Infix {
				t = addLen(t, operand)
			}
			best = min(best, t)
		}
	}
	return best
}

// outer returns, for each rule, the length of the shortest text around a call of the rule in an input:
// the shortest input whose derivation calls the rule, less the shortest match of the rule. It is inf
// for the rules that no input calls.
func (in *info) outer() []int {
	n := len(in.order)
	dist := make([]int, n)
	for i := range dist {
		dist[i] = inf
	}
	if in.start == nil || in.start.height >= inf {
		return dist
	}
	dist[in.start.index] = 0
	done := make([]bool, n)
	var callee *ruleInfo
	call := &through{in: in, goal: -1, ref: func(ri *ruleInfo) int {
		if ri == callee {
			return ri.length
		}
		return inf
	}}
	// Dijkstra's algorithm over the call graph: a call costs the text of the caller's shortest match
	// through it, less the callee's own match.
	for {
		u := -1
		for i := range n {
			if !done[i] && dist[i] < inf && (u < 0 || dist[i] < dist[u]) {
				u = i
			}
		}
		if u < 0 {
			return dist
		}
		done[u] = true
		caller := in.order[u]
		for _, name := range caller.calls {
			callee = in.rules[name]
			if callee == nil || done[callee.index] || callee.height >= inf {
				continue
			}
			if l := call.expr(caller.def.Expr); l < inf {
				dist[callee.index] = min(dist[callee.index], addLen(dist[u], l-callee.length))
			}
		}
	}
}

// minLens returns, for each target, the length in bytes of the shortest input that exercises it, or
// inf: the shortest text around a call of its rule, and the shortest match of the rule's body that
// exercises it directly, without a nested call of the rule.
func (in *info) minLens() []int {
	outer := in.outer()
	local := &through{in: in, ref: func(*ruleInfo) int { return inf }}
	lens := make([]int, len(in.targets))
	for t, tg := range in.targets {
		ri := in.rules[tg.rule]
		switch {
		case outer[ri.index] >= inf || ri.height >= inf:
			lens[t] = inf
		case t == ri.index:
			lens[t] = addLen(outer[ri.index], ri.length)
		default:
			local.goal = t
			lens[t] = addLen(outer[ri.index], local.expr(ri.def.Expr))
		}
	}
	return lens
}
