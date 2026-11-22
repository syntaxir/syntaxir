package examinedref

import "syntaxir.dev/sxir/pkg/grammar/ast"

// valued reports whether e has a value, an element of the sequence around it, as
// spec/language/parser-expressions.md (Values and the Concrete Syntax Tree) defines: terminals,
// #atomic, repetitions and captures have one; a sequence has one when an element has; a choice when
// an alternative that can match has; an optional or a recovery when its operand has one or has the
// type _|_; a call when the rule makes a node or, for a hidden rule without an action, when its body
// has one (valuedRules). Anchors, the cut, discards, top, bottom, lookaheads and predicates have none.
func (ev *evaluator) valued(e ast.Expr) bool {
	if v, ok := ev.valuedMemo[e]; ok {
		return v
	}
	v := valuedIn(e, ev.rules, ev.valuedRules, ev.bottomRules)
	ev.valuedMemo[e] = v
	return v
}

// valuedIn reports whether e has a value when the hidden rules without an action that have one are
// those of the set valuedRules.
func valuedIn(e ast.Expr, rules map[string]*rule, valuedRules, bottomRules map[string]bool) bool {
	valued := func(e ast.Expr) bool { return valuedIn(e, rules, valuedRules, bottomRules) }
	bottom := func(e ast.Expr) bool { return bottomIn(e, bottomRules) }
	switch e := e.(type) {
	case *ast.Literal, *ast.CharClass, *ast.Any, *ast.Atomic, *ast.Repeat, *ast.Capture:
		return true
	case *ast.Group:
		return valued(e.Expr)
	case *ast.Stream:
		return valued(e.Expr)
	case *ast.Seq:
		for _, it := range e.Items {
			if valued(it) {
				return true
			}
		}
		return false
	case *ast.Choice:
		for _, alt := range e.Alts {
			if !bottom(alt) && valued(alt) {
				return true
			}
		}
		return false
	case *ast.Optional:
		return valued(e.Expr) || bottom(e.Expr)
	case *ast.Recovery:
		return valued(e.Expr) || bottom(e.Expr)
	case *ast.Attributed:
		if missingOf(e) {
			return valued(e.Expr) || bottom(e.Expr)
		}
		return valued(e.Expr)
	case *ast.Ref:
		r := rules[e.Name]
		if r == nil {
			return false
		}
		if r.ownNode || r.ops != nil {
			return true
		}
		return valuedRules[e.Name]
	}
	return false
}

// valuedRules returns the hidden rules without an action whose bodies have a value: the least set of
// such rules whose bodies have a value when the calls of the rules of the set do, so that a rule
// that reaches itself before any element, as _r = _r, has none, and a call of a rule that has one
// through another, as _r = "a" _r / "b", has one wherever it is.
func valuedRules(rs *ruleSet, bottomRules map[string]bool) map[string]bool {
	set := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, name := range rs.order {
			r := rs.byName[name]
			if set[name] || r.ownNode || r.ops != nil {
				continue
			}
			if valuedIn(r.body, rs.byName, set, bottomRules) {
				set[name] = true
				changed = true
			}
		}
	}
	return set
}

// missingOf reports whether the attributes of e include #missing.
func missingOf(e *ast.Attributed) bool {
	for _, a := range e.Attrs {
		if a.Name == "missing" {
			return true
		}
	}
	return false
}

// bottom reports whether e has the type _|_ (spec/language/types.md, Top, Bottom, Join and Meet;
// spec/language/pragmas.md, #recover): _|_ itself; a sequence with an element of that type; a choice
// whose alternatives all have it; a repetition of at least one such element; such an expression
// grouped, captured, made #atomic or with attributes other than recovery; and a call of a rule
// without an action or a declared type whose body has it.
func (ev *evaluator) bottom(e ast.Expr) bool {
	return bottomIn(e, ev.bottomRules)
}

func bottomIn(e ast.Expr, rules map[string]bool) bool {
	switch e := e.(type) {
	case *ast.Bottom:
		return true
	case *ast.Group:
		return bottomIn(e.Expr, rules)
	case *ast.Seq:
		for _, it := range e.Items {
			if bottomIn(it, rules) {
				return true
			}
		}
		return false
	case *ast.Choice:
		for _, alt := range e.Alts {
			if !bottomIn(alt, rules) {
				return false
			}
		}
		return true
	case *ast.Repeat:
		return e.Min > 0 && bottomIn(e.Expr, rules)
	case *ast.Attributed:
		return !missingOf(e) && bottomIn(e.Expr, rules)
	case *ast.Capture:
		return bottomIn(e.Expr, rules)
	case *ast.Atomic:
		return bottomIn(e.Expr, rules)
	case *ast.Ref:
		return rules[e.Name]
	}
	return false
}

// bottomRules returns the rules of type _|_: the greatest set of rules without an action or a
// declared type whose bodies have the type _|_ when the calls of the rules of the set do, so that a
// rule that only calls itself, as r = "a" r, is one.
func bottomRules(rs *ruleSet) map[string]bool {
	set := map[string]bool{}
	for _, name := range rs.order {
		r := rs.byName[name]
		if r.act == nil && !r.typed && r.ops == nil {
			set[name] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, name := range rs.order {
			if set[name] && !bottomIn(rs.byName[name].body, set) {
				delete(set, name)
				changed = true
			}
		}
	}
	return set
}
