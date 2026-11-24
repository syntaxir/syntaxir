package examinedref

import (
	"fmt"

	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The facts of the rules that decide the growths of left recursion, computed from the definitions in
// spec/core-ir/attributes.md (Nullability, Rule Facts), once for a parse without token insertion and
// once for a parse with it (spec/language/parser-expressions.md, Left Recursion). The definitions are
// restated here from the spec, not taken from the lowering or the engine. The rules are those of
// ruleSet: the rules as written and the hidden rules of their inline actions.
//
// Nullable is the least fixed point over the rules. Succeeds (a rule off every left-recursion cycle
// whose body always succeeds) starts with no rule proven and grows: the left calls, the cycles and the
// proofs are recomputed until no rule is added. Leaders then grow by declaration order until every
// cycle of left calls passes through one.

// analysis holds the facts of one grammar while they are computed.
type analysis struct {
	rules    map[string]*rule
	names    []string // rule names in declaration order
	ins      bool     // the facts of a parse with token insertion: NullableIns in place of Nullable
	nullable map[string]bool
	proven   map[string]bool
	left     map[string][]string
	err      error
}

// ruleFacts is the result of the analysis: the leaders, which the evaluator reads, and the cycles of
// left calls, which the perturbation check reads (LeftCycles).
type ruleFacts struct {
	leaders map[string]bool
	cycles  map[string]int
}

// analyzeRules computes the leaders of the rules, with token insertion if ins is set.
func analyzeRules(rs *ruleSet, ins bool) (ruleFacts, error) {
	a := &analysis{
		rules:    rs.byName,
		names:    rs.order,
		ins:      ins,
		nullable: map[string]bool{},
		proven:   map[string]bool{},
		left:     map[string][]string{},
	}
	a.computeNullable()
	a.computeSucceeds()
	leaders := a.computeLeaders()
	if a.err != nil {
		return ruleFacts{}, a.err
	}
	return ruleFacts{leaders: leaders, cycles: cycleNumbers(a.names, a.left)}, nil
}

func (a *analysis) unsupported(e ast.Expr) {
	if a.err == nil {
		a.err = &UnsupportedError{Construct: fmt.Sprintf("%T in a rule's facts", e)}
	}
}

// computeNullable is the least fixed point of Nullable over the rules.
func (a *analysis) computeNullable() {
	for changed := true; changed; {
		changed = false
		for _, name := range a.names {
			if !a.nullable[name] && a.nullableExpr(a.rules[name].body) {
				a.nullable[name] = true
				changed = true
			}
		}
	}
}

// nullableExpr reports whether e can succeed without consuming input (Nullable, or NullableIns in the
// facts with insertion).
func (a *analysis) nullableExpr(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Ref:
		return a.nullable[e.Name]
	case *ast.Literal:
		return e.Value == ""
	case *ast.CharClass, *ast.Any, *ast.Bottom:
		return false
	case *ast.Recovery:
		// When the body fails, a skip that consumes nothing is no recovery.
		return a.nullableExpr(e.Expr)
	case *ast.Attributed:
		if base, missing := splitMissing(e); missing != nil {
			// An insertion consumes no input.
			return a.ins || a.nullableExpr(base)
		}
		return a.nullableExpr(e.Expr)
	case *ast.OperatorsRule:
		for _, o := range e.Operands {
			if a.nullableExpr(o.Expr) {
				return true
			}
		}
		return false
	case *ast.Seq:
		for _, item := range e.Items {
			if !a.nullableExpr(item) {
				return false
			}
		}
		return true
	case *ast.Choice:
		for _, alt := range e.Alts {
			if a.nullableExpr(alt) {
				return true
			}
		}
		return false
	case *ast.Repeat:
		return e.Min == 0 || (e.Min == 1 && a.nullableExpr(e.Expr))
	case *ast.Optional, *ast.And, *ast.Not, *ast.Cut, *ast.Top, *ast.Predicate, *ast.BeginInput, *ast.EndInput, *ast.BeginLine, *ast.EndLine:
		return true
	case *ast.Group, *ast.Atomic, *ast.Discard, *ast.Capture, *ast.Stream:
		return a.nullableExpr(transparentBody(e))
	}
	a.unsupported(e)
	return false
}

// transparentBody returns the body of a node whose facts are those of its body.
func transparentBody(e ast.Expr) ast.Expr {
	switch e := e.(type) {
	case *ast.Group:
		return e.Expr
	case *ast.Atomic:
		return e.Expr
	case *ast.Discard:
		return e.Expr
	case *ast.Capture:
		return e.Expr
	case *ast.Stream:
		return e.Expr
	}
	return nil
}

// computeSucceeds grows the proven rules and the left calls together. Each round recomputes the left
// calls and the cycles from the rules proven so far, and proves the rules that are off every cycle and
// whose body always succeeds. A proof only removes left calls and cycles, so the rounds end.
func (a *analysis) computeSucceeds() {
	for {
		for _, name := range a.names {
			a.left[name] = a.leftCalls(a.rules[name].body)
		}
		onCycle := cyclicRules(a.names, a.left, nil)
		added := false
		for _, name := range a.names {
			if !a.proven[name] && !onCycle[name] && a.alwaysSucc(a.rules[name].body) {
				a.proven[name] = true
				added = true
			}
		}
		if !added || a.err != nil {
			return
		}
	}
}

// computeLeaders makes leaders until every cycle of left calls passes through one: among the rules
// that lie on a cycle avoiding the leaders, the one declared first becomes a leader.
func (a *analysis) computeLeaders() map[string]bool {
	leaders := map[string]bool{}
	for {
		onCycle := cyclicRules(a.names, a.left, leaders)
		candidate := ""
		for _, name := range a.names {
			if onCycle[name] {
				candidate = name
				break
			}
		}
		if candidate == "" {
			return leaders
		}
		leaders[candidate] = true
	}
}

// leftCalls returns the rules that e may call before it consumes input (Left calls, Rule Facts).
func (a *analysis) leftCalls(e ast.Expr) []string {
	switch e := e.(type) {
	case *ast.Ref:
		return []string{e.Name}
	case *ast.Recovery:
		out := a.leftCalls(e.Expr)
		if !a.alwaysSucc(e.Expr) {
			out = append(out, a.leftCalls(e.Skip)...)
		}
		return out
	case *ast.Attributed:
		return a.leftCalls(e.Expr)
	case *ast.OperatorsRule:
		var out []string
		if e.Skip != nil {
			out = append(out, a.leftCalls(e.Skip)...)
		}
		nullOperand := false
		for _, o := range e.Operands {
			nullOperand = nullOperand || a.nullableExpr(o.Expr)
		}
		for _, o := range e.Operands {
			out = append(out, a.leftCalls(o.Expr)...)
			if a.alwaysSucc(o.Expr) {
				break
			}
		}
		// An operand that matched nothing leaves the infix and postfix parts to be tried where the
		// rule started.
		for _, l := range e.Levels {
			for _, op := range l.Operators {
				if op.Kind == ast.Prefix || nullOperand {
					out = append(out, a.leftCalls(op.Expr)...)
				}
			}
		}
		return out
	case *ast.Seq:
		var out []string
		for _, item := range e.Items {
			out = append(out, a.leftCalls(item)...)
			if !a.nullableExpr(item) {
				break
			}
		}
		return out
	case *ast.Choice:
		var out []string
		for _, alt := range e.Alts {
			out = append(out, a.leftCalls(alt)...)
			if a.alwaysSucc(alt) {
				break
			}
		}
		return out
	case *ast.Repeat:
		if e.Max == 0 {
			return nil
		}
		return a.leftCalls(e.Expr)
	case *ast.Optional:
		return a.leftCalls(e.Expr)
	case *ast.And:
		return a.leftCalls(e.Expr)
	case *ast.Not:
		return a.leftCalls(e.Expr)
	case *ast.Group, *ast.Atomic, *ast.Discard, *ast.Capture, *ast.Stream:
		return a.leftCalls(transparentBody(e))
	case *ast.Literal, *ast.CharClass, *ast.Any, *ast.Cut, *ast.Top, *ast.Bottom, *ast.Predicate, *ast.BeginInput, *ast.EndInput, *ast.BeginLine, *ast.EndLine:
		return nil
	}
	a.unsupported(e)
	return nil
}

// alwaysSucc reports whether e always succeeds, given the rules proven to succeed (Rule Facts).
func (a *analysis) alwaysSucc(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Ref:
		return a.proven[e.Name]
	case *ast.Literal:
		return e.Value == ""
	case *ast.Cut, *ast.Top:
		return true
	case *ast.Recovery:
		return a.alwaysSucc(e.Expr)
	case *ast.Attributed:
		if _, missing := splitMissing(e); missing != nil {
			return false
		}
		return a.alwaysSucc(e.Expr)
	case *ast.Seq:
		for _, item := range e.Items {
			if !a.alwaysSucc(item) {
				return false
			}
		}
		return true
	case *ast.Choice:
		earlierCut := false
		for _, alt := range e.Alts {
			if !earlierCut && a.alwaysSucc(alt) {
				return true
			}
			earlierCut = earlierCut || a.looseCut(alt)
		}
		return false
	case *ast.Optional:
		return !a.looseCut(e.Expr) || a.alwaysSucc(e.Expr)
	case *ast.Repeat:
		switch {
		case e.Max == 0:
			return e.Min == 0
		case e.Min == 0:
			return !a.looseCut(e.Expr) || a.alwaysSucc(e.Expr)
		case e.Min == 1:
			return a.alwaysSucc(e.Expr)
		}
		return false
	case *ast.And:
		return a.alwaysSucc(e.Expr)
	case *ast.Not:
		return a.alwaysFail(e.Expr)
	case *ast.Group, *ast.Atomic, *ast.Discard, *ast.Capture, *ast.Stream:
		return a.alwaysSucc(transparentBody(e))
	case *ast.CharClass, *ast.Any, *ast.Bottom, *ast.Predicate, *ast.OperatorsRule, *ast.BeginInput, *ast.EndInput, *ast.BeginLine, *ast.EndLine:
		return false
	}
	a.unsupported(e)
	return false
}

// alwaysFail reports whether e always fails, given the rules proven to succeed (Rule Facts).
func (a *analysis) alwaysFail(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Bottom:
		return true
	case *ast.Attributed:
		if _, missing := splitMissing(e); missing != nil {
			return false
		}
		return a.alwaysFail(e.Expr)
	case *ast.Seq:
		for _, item := range e.Items {
			if a.alwaysFail(item) {
				return true
			}
		}
		return false
	case *ast.Choice:
		for _, alt := range e.Alts {
			if !a.alwaysFail(alt) {
				return false
			}
		}
		return len(e.Alts) > 0
	case *ast.And:
		return a.alwaysFail(e.Expr)
	case *ast.Not:
		return a.alwaysSucc(e.Expr)
	case *ast.Group, *ast.Atomic, *ast.Discard, *ast.Capture, *ast.Stream:
		return a.alwaysFail(transparentBody(e))
	case *ast.Ref, *ast.Literal, *ast.CharClass, *ast.Any, *ast.Optional, *ast.Repeat, *ast.Cut, *ast.Top, *ast.Predicate,
		*ast.Recovery, *ast.OperatorsRule, *ast.BeginInput, *ast.EndInput, *ast.BeginLine, *ast.EndLine:
		return false
	}
	a.unsupported(e)
	return false
}

// looseCut reports whether e has a cut outside nested choices, options, repetitions, lookaheads and
// calls.
func (a *analysis) looseCut(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Cut:
		return true
	case *ast.Seq:
		for _, item := range e.Items {
			if a.looseCut(item) {
				return true
			}
		}
		return false
	case *ast.Group, *ast.Atomic, *ast.Discard, *ast.Capture, *ast.Stream:
		return a.looseCut(transparentBody(e))
	case *ast.Attributed:
		return a.looseCut(e.Expr)
	case *ast.Recovery:
		// The skip of a recovery is in the scope of its body.
		return a.looseCut(e.Expr) || a.looseCut(e.Skip)
	case *ast.Ref, *ast.Literal, *ast.CharClass, *ast.Any, *ast.Choice, *ast.Optional, *ast.Repeat, *ast.And, *ast.Not,
		*ast.Top, *ast.Bottom, *ast.Predicate, *ast.OperatorsRule, *ast.BeginInput, *ast.EndInput, *ast.BeginLine, *ast.EndLine:
		return false
	}
	a.unsupported(e)
	return false
}

// cyclicRules returns the rules that lie on a cycle of the graph of left calls among the rules that
// are not excluded.
func cyclicRules(names []string, edges map[string][]string, excluded map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, comp := range cycles(names, edges, excluded) {
		for _, v := range comp {
			out[v] = true
		}
	}
	return out
}

// cycleNumbers numbers the cycles of the graph of left calls: the rules of one cycle get the same
// number, and a rule on no cycle has none.
func cycleNumbers(names []string, edges map[string][]string) map[string]int {
	out := map[string]int{}
	for i, comp := range cycles(names, edges, nil) {
		for _, v := range comp {
			out[v] = i
		}
	}
	return out
}

// cycles returns the cycles of the graph of left calls among the rules that are not excluded, as its
// strongly connected components that hold one: a component of several rules, or a rule with a left
// call of itself.
func cycles(names []string, edges map[string][]string, excluded map[string]bool) [][]string {
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var comps [][]string
	next := 0
	var visit func(v string)
	visit = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range edges[v] {
			if excluded[w] || !isRule(names, w) {
				continue
			}
			if _, seen := index[w]; !seen {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			comps = append(comps, comp)
		}
	}
	for _, v := range names {
		if excluded[v] {
			continue
		}
		if _, seen := index[v]; !seen {
			visit(v)
		}
	}
	var out [][]string
	for _, comp := range comps {
		if len(comp) > 1 {
			out = append(out, comp)
			continue
		}
		v := comp[0]
		for _, w := range edges[v] {
			if w == v && !excluded[v] {
				out = append(out, comp)
				break
			}
		}
	}
	return out
}

func isRule(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
